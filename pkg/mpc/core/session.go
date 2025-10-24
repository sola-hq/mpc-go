package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/bnb-chain/tss-lib/v2/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/node"
	"github.com/fystack/mpcium/pkg/storage"
	"github.com/fystack/mpcium/pkg/types"
	"github.com/nats-io/nats.go"
)

type SessionType string

const (
	TypeGenerateWalletResultFmt  = "mpc.mpc_keygen_result.%s"
	TypeResharingWalletResultFmt = "mpc.mpc_resharing_result.%s"
	TypeSigningResultFmt         = "mpc.mpc_signing_result.%s"

	SessionTypeECDSA SessionType = "session_ecdsa"
	SessionTypeEDDSA SessionType = "session_eddsa"

	BackwardCompatibleVersion int = 0
	DefaultVersion            int = 1
)

var (
	ErrNotEnoughParticipants = errors.New("not enough participants to sign")
	ErrNotInParticipantList  = errors.New("node is not in the participant list")
)

type TopicComposer struct {
	ComposeBroadcastTopic func() string
	ComposeDirectTopic    func(fromID string, toID string) string
}

type KeyComposerFn func(id string) string

type Session interface {
	ListenToIncomingMessageAsync()
	ListenToPeersAsync(peerIDs []string)
	GetErrChan() <-chan error
	Close() error
	GetPubKeyResult() []byte
	GetVersion() int
	GetPartyID() *tss.PartyID
	GetPartyIDs() []*tss.PartyID
	GetPartyCount() int
}

type KeyGenSession interface {
	Session
	Init()
	GenerateKey(done func())
}

type SigningSession interface {
	Session
	Init(tx *big.Int) error
	Sign(onSuccess func(data []byte))
}

type ResharingSession interface {
	Session
	Init() error
	Reshare(done func())
	GetLegacyCommitteePeers() []string
}

type PartySession struct {
	WalletID string
	PubSub   messaging.PubSub
	Direct   messaging.DirectMessaging

	Threshold          int
	ParticipantPeerIDs []string
	SelfPartyID        *tss.PartyID
	// IDs of all parties in the session including self
	PartyIDs []*tss.PartyID
	OutCh    chan tss.Message
	ErrCh    chan error
	Party    tss.Party
	Version  int

	// PreParams is nil for EDDSA session
	PreParams    *keygen.LocalPreParams
	KVStore      storage.Store
	KeyinfoStore node.KeyStore
	BroadcastSub messaging.Subscription
	DirectSubs   []messaging.Subscription

	ResultQueue   messaging.MessageQueue
	IdentityStore node.IdentityStore

	TopicComposer *TopicComposer
	ComposeKey    KeyComposerFn
	GetRoundFunc  GetRoundFunc
	Mu            sync.Mutex
	// After the session is done, the key will be stored PubkeyBytes
	PubkeyBytes   []byte
	SessionType   SessionType
	IdempotentKey string
}

func (s *PartySession) GetPartyID() *tss.PartyID {
	return s.SelfPartyID
}

func (s *PartySession) GetPartyIDs() []*tss.PartyID {
	return s.PartyIDs
}

func (s *PartySession) GetPartyCount() int {
	return len(s.PartyIDs)
}

// update: use AEAD encryption for each message so NATs server learns nothing
func (s *PartySession) HandleTssMessage(btssMsg tss.Message) {
	data, routing, err := btssMsg.WireBytes()
	if err != nil {
		s.ErrCh <- err
		return
	}

	tssMsg := types.NewTssMessage(s.WalletID, data, routing.IsBroadcast, routing.From, routing.To)

	toIDs := make([]string, len(routing.To))
	for i, id := range routing.To {
		toIDs[i] = id.String()
	}
	logger.Debug(
		fmt.Sprintf("%s Sending message", s.SessionType),
		"from",
		s.SelfPartyID.String(),
		"to",
		toIDs,
		"isBroadcast",
		routing.IsBroadcast,
	)

	// Broadcast message
	if routing.IsBroadcast && len(routing.To) == 0 {
		signature, err := s.IdentityStore.SignMessage(&tssMsg) // attach signature
		if err != nil {
			s.ErrCh <- fmt.Errorf("failed to sign message: %w", err)
			return
		}
		tssMsg.Signature = signature
		msg, err := types.MarshalTssMessage(&tssMsg)
		if err != nil {
			s.ErrCh <- fmt.Errorf("failed to marshal tss message: %w", err)
			return
		}

		err = s.PubSub.Publish(s.TopicComposer.ComposeBroadcastTopic(), msg)
		if err != nil {
			s.ErrCh <- err
			return
		}
	} else {
		// p2p message
		msg, err := types.MarshalTssMessage(&tssMsg) // without signature
		if err != nil {
			s.ErrCh <- fmt.Errorf("failed to marshal tss message: %w", err)
			return
		}

		selfID := PartyIDToNodeID(s.SelfPartyID)
		for _, to := range routing.To {
			toNodeID := PartyIDToNodeID(to)
			topic := s.TopicComposer.ComposeDirectTopic(selfID, toNodeID)
			if selfID == toNodeID {
				err := s.Direct.SendToSelf(topic, msg)
				if err != nil {
					logger.Error("Failed in SendToSelf direct message", err, "topic", topic)
					s.ErrCh <- fmt.Errorf("failed to send direct message to %s", topic)
				}
			} else {
				cipher, err := s.IdentityStore.EncryptMessage(msg, toNodeID)
				if err != nil {
					s.ErrCh <- fmt.Errorf("encrypt tss message error %w", err)
					logger.Error("Encrypt tss message error", err, "topic", topic)
				}
				err = s.Direct.SendToOther(topic, cipher)
				if err != nil {
					logger.Error("Failed in SendToOther direct message", err, "topic", topic)
					s.ErrCh <- fmt.Errorf("failed to send direct message to %w", err)
				}
			}
		}
	}
}

func (s *PartySession) receiveP2PTssMessage(topic string, cipher []byte) {
	senderID := extractSenderIDFromDirectTopic(topic)
	if senderID == "" {
		s.ErrCh <- fmt.Errorf("failed to extract senderID from direct topic: the direct topic format is wrong")
		return
	}

	var plaintext []byte
	var err error

	if senderID == PartyIDToNodeID(s.SelfPartyID) {
		plaintext = cipher // to self, no decryption needed
	} else {
		plaintext, err = s.IdentityStore.DecryptMessage(cipher, senderID)
		if err != nil {
			s.ErrCh <- fmt.Errorf("failed to decrypt message: %w, tampered message", err)
			return
		}
	}
	msg, err := types.UnmarshalTssMessage(plaintext)
	if err != nil {
		s.ErrCh <- fmt.Errorf("failed to unmarshal message: %w", err)
		return
	}

	s.receiveTssMessage(msg)
}

func (s *PartySession) receiveBroadcastTssMessage(rawMsg []byte) {

	msg, err := types.UnmarshalTssMessage(rawMsg)
	if err != nil {
		s.ErrCh <- fmt.Errorf("failed to unmarshal message: %w", err)
		return
	}

	err = s.IdentityStore.VerifyMessage(msg)
	if err != nil {
		s.ErrCh <- fmt.Errorf("failed to verify message: %w, tampered message", err)
		return
	}

	s.receiveTssMessage(msg)
}

// update: the logic of receiving message should be modified
func (s *PartySession) receiveTssMessage(msg *types.TssMessage) {
	toIDs := make([]string, len(msg.To))
	for i, id := range msg.To {
		toIDs[i] = id.String()
	}

	round, err := s.GetRoundFunc(msg.MsgBytes, s.SelfPartyID, msg.IsBroadcast)
	if err != nil {
		s.ErrCh <- fmt.Errorf("broken tss share: %w", err)
		return
	}
	logger.Debug(
		"Received message",
		"round",
		round.RoundMsg,
		"isBroadcast",
		msg.IsBroadcast,
		"to",
		toIDs,
		"from",
		msg.From.String(),
		"self",
		s.SelfPartyID.String(),
	)
	isBroadcast := msg.IsBroadcast && len(msg.To) == 0
	var isToSelf bool
	for _, to := range msg.To {
		if ComparePartyIDs(to, s.SelfPartyID) {
			isToSelf = true
			break
		}
	}

	if isBroadcast || isToSelf {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		ok, err := s.Party.UpdateFromBytes(msg.MsgBytes, msg.From, msg.IsBroadcast)
		if !ok || err != nil {
			logger.Error("Failed to update party", err, "walletID", s.WalletID)
			return
		}
	}
}

func (s *PartySession) subscribeDirectTopicAsync(topic string) error {
	t := topic // avoid capturing the changing loop variable
	sub, err := s.Direct.Listen(t, func(cipher []byte) {
		// async to avoid timeouts in handlers
		go s.receiveP2PTssMessage(t, cipher)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to direct topic %s: %w", t, err)
	}
	s.DirectSubs = append(s.DirectSubs, sub)
	return nil
}

func (s *PartySession) subscribeFromPeersAsync(fromIDs []string) {
	toID := PartyIDToNodeID(s.SelfPartyID)
	for _, fromID := range fromIDs {
		topic := s.TopicComposer.ComposeDirectTopic(fromID, toID)
		if err := s.subscribeDirectTopicAsync(topic); err != nil {
			s.ErrCh <- err
		}
	}
}

func (s *PartySession) subscribeBroadcastAsync() {
	go func() {
		topic := s.TopicComposer.ComposeBroadcastTopic()
		sub, err := s.PubSub.Subscribe(topic, func(natMsg *nats.Msg) {
			s.receiveBroadcastTssMessage(natMsg.Data)
		})
		if err != nil {
			s.ErrCh <- fmt.Errorf("failed to subscribe to broadcast topic %s: %w", topic, err)
			return
		}
		s.BroadcastSub = sub
	}()
}

func (s *PartySession) ListenToIncomingMessageAsync() {
	// 1) broadcast
	s.subscribeBroadcastAsync()

	// 2) direct from peers in this session's partyIDs (includes self)
	s.subscribeFromPeersAsync(PartyIDsToNodeIDs(s.PartyIDs))
}

func (s *PartySession) ListenToPeersAsync(peerIDs []string) {
	s.subscribeFromPeersAsync(peerIDs)
}

func (s *PartySession) Close() error {
	err := s.BroadcastSub.Unsubscribe()
	if err != nil {
		return err
	}

	for _, sub := range s.DirectSubs {
		err = sub.Unsubscribe()
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *PartySession) GetPubKeyResult() []byte {
	return s.PubkeyBytes
}

func (s *PartySession) GetErrChan() <-chan error {
	return s.ErrCh
}

func (s *PartySession) GetVersion() int {
	return s.Version
}

// LoadOldShareDataGeneric loads the old share data from storage with backward compatibility (versioned and unversioned keys)
func (s *PartySession) LoadOldShareDataGeneric(walletID string, version int, dest interface{}) error {
	var (
		key     string
		keyData []byte
		err     error
	)

	// Try versioned key first if version > 0
	if version > 0 {
		key = s.ComposeKey(WalletIDWithVersion(walletID, version))
		keyData, err = s.KVStore.Get(key)
		if err != nil {
			return err
		}
	}

	// If version == 0 or previous key not found, fall back to unversioned key
	if version == 0 {
		key = s.ComposeKey(walletID)
		keyData, err = s.KVStore.Get(key)
		if err != nil {
			return err
		}
	}

	if err != nil {
		return fmt.Errorf("failed to get wallet data from KVStore (key=%s): %w", key, err)
	}

	if err := json.Unmarshal(keyData, dest); err != nil {
		return fmt.Errorf("failed to unmarshal wallet data: %w", err)
	}
	return nil
}

// WalletIDWithVersion is used to compose the key for the KVStore
func WalletIDWithVersion(walletID string, version int) string {
	if version > 0 {
		return fmt.Sprintf("%s_v%d", walletID, version)
	}
	return walletID
}

func extractSenderIDFromDirectTopic(topic string) string {
	// E.g: keygen:direct:ecdsa:<fromID>:<toID>:<walletID>
	parts := strings.SplitN(topic, ":", 5)
	if len(parts) >= 4 {
		return parts[3]
	}

	return ""
}
