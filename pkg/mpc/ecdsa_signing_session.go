package mpc

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/bnb-chain/tss-lib/v2/common"
	"github.com/bnb-chain/tss-lib/v2/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v2/ecdsa/signing"
	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/fystack/mpcium/pkg/common/errors"
	"github.com/fystack/mpcium/pkg/event"
	"github.com/fystack/mpcium/pkg/identity"
	"github.com/fystack/mpcium/pkg/keyinfo"
	"github.com/fystack/mpcium/pkg/kvstore"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/samber/lo"
)

type SigningSession interface {
	Session

	Init(tx *big.Int) error
	Sign(onSuccess func(data []byte))
}

// Ecdsa signing session
type ecdsaSigningSession struct {
	session
	endCh               chan *common.SignatureData
	data                *keygen.LocalPartySaveData
	tx                  *big.Int
	txID                string
	networkInternalCode string
}

func newECDSASigningSession(
	walletID string,
	txID string,
	networkInternalCode string,
	pubSub messaging.PubSub,
	direct messaging.DirectMessaging,
	participantPeerIDs []string,
	selfID *tss.PartyID,
	partyIDs []*tss.PartyID,
	threshold int,
	preParams *keygen.LocalPreParams,
	kvstore kvstore.KVStore,
	keyinfoStore keyinfo.Store,
	resultQueue messaging.MessageQueue,
	identityStore identity.Store,
	idempotentKey string,
) *ecdsaSigningSession {
	return &ecdsaSigningSession{
		session: session{
			walletID:           walletID,
			pubSub:             pubSub,
			direct:             direct,
			threshold:          threshold,
			participantPeerIDs: participantPeerIDs,
			selfPartyID:        selfID,
			partyIDs:           partyIDs,
			outCh:              make(chan tss.Message),
			ErrCh:              make(chan error),
			preParams:          preParams,
			kvstore:            kvstore,
			keyinfoStore:       keyinfoStore,
			topicComposer: &TopicComposer{
				ComposeBroadcastTopic: func() string {
					return fmt.Sprintf("sign:ecdsa:broadcast:%s:%s", walletID, txID)
				},
				ComposeDirectTopic: func(fromID string, toID string) string {
					return fmt.Sprintf("sign:ecdsa:direct:%s:%s:%s", fromID, toID, txID)
				},
			},
			composeKey: func(waleltID string) string {
				return fmt.Sprintf("ecdsa:%s", waleltID)
			},
			getRoundFunc:  GetEcdsaMsgRound,
			resultQueue:   resultQueue,
			identityStore: identityStore,
			idempotentKey: idempotentKey,
		},
		endCh:               make(chan *common.SignatureData),
		txID:                txID,
		networkInternalCode: networkInternalCode,
	}
}

func (s *ecdsaSigningSession) Init(tx *big.Int) error {
	logger.Infof("Initializing signing session with partyID: %s, peerIDs %s", s.selfPartyID, s.partyIDs)
	ctx := tss.NewPeerContext(s.partyIDs)
	params := tss.NewParameters(tss.S256(), ctx, s.selfPartyID, len(s.partyIDs), s.threshold)

	keyInfo, err := s.keyinfoStore.Get(s.composeKey(s.walletID))
	if err != nil {
		return errors.Wrap(err, "Failed to get key info data")
	}

	if len(s.participantPeerIDs) < keyInfo.Threshold+1 {
		logger.Warn("Not enough participants to sign", "participants", s.participantPeerIDs, "expected", keyInfo.Threshold+1)
		return ErrNotEnoughParticipants
	}

	// check if t+1 participants are present
	result := lo.Intersect(s.participantPeerIDs, keyInfo.ParticipantPeerIDs)
	if len(result) < keyInfo.Threshold+1 {
		return fmt.Errorf(
			"incompatible peerIDs to participate in signing. Current participants: %v, expected participants: %v",
			s.participantPeerIDs,
			keyInfo.ParticipantPeerIDs,
		)
	}

	logger.Info("Have enough participants to sign", "participants", s.participantPeerIDs)

	keyData, err := s.kvstore.Get(s.composeKey(walletIDWithVersion(s.walletID, keyInfo.Version)))
	if err != nil {
		return errors.Wrap(err, "Failed to get wallet data from KVStore")
	}
	// Check if all the participants of the key are present
	var data keygen.LocalPartySaveData
	err = json.Unmarshal(keyData, &data)
	if err != nil {
		return errors.Wrap(err, "Failed to unmarshal wallet data")
	}

	s.party = signing.NewLocalParty(tx, params, data, s.outCh, s.endCh)
	s.data = &data
	s.version = keyInfo.Version
	s.tx = tx
	logger.Info("Initialized sigining session successfully!")
	return nil
}

func (s *ecdsaSigningSession) Sign(onSuccess func(data []byte)) {
	logger.Info("Starting signing", "walletID", s.walletID)
	go func() {
		if err := s.party.Start(); err != nil {
			s.ErrCh <- err
		}
	}()

	for {

		select {
		case msg := <-s.outCh:
			s.handleTssMessage(msg)
		case sig := <-s.endCh:
			publicKey := *s.data.ECDSAPub
			pk := ecdsa.PublicKey{
				Curve: publicKey.Curve(),
				X:     publicKey.X(),
				Y:     publicKey.Y(),
			}

			ok := ecdsa.Verify(&pk, s.tx.Bytes(), new(big.Int).SetBytes(sig.R), new(big.Int).SetBytes(sig.S))
			if !ok {
				s.ErrCh <- errors.New("Failed to verify signature")
				return
			}

			r := event.SigningResultEvent{
				ResultType:          event.ResultTypeSuccess,
				NetworkInternalCode: s.networkInternalCode,
				WalletID:            s.walletID,
				TxID:                s.txID,
				R:                   sig.R,
				S:                   sig.S,
				SignatureRecovery:   sig.SignatureRecovery,
			}

			bytes, err := json.Marshal(r)
			if err != nil {
				s.ErrCh <- errors.Wrap(err, "Failed to marshal raw signature")
				return
			}

			err = s.resultQueue.Enqueue(event.SigningResultCompleteTopic, bytes, &messaging.EnqueueOptions{
				IdempotentKey: s.idempotentKey,
			})
			if err != nil {
				s.ErrCh <- errors.Wrap(err, "Failed to publish sign success message")

				return
			}

			logger.Info("[SIGN] Sign successfully", "walletID", s.walletID)
			err = s.Close()
			if err != nil {
				logger.Error("Failed to close session", err)
			}

			onSuccess(bytes)
			return
		}

	}
}
