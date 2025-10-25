package ecdsa

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"

	"github.com/bnb-chain/tss-lib/v2/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v2/ecdsa/resharing"
	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/fystack/mpcium/pkg/encoding"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/mpc/core"
	"github.com/fystack/mpcium/pkg/node"
	"github.com/fystack/mpcium/pkg/storage"
)

type resharingSession struct {
	*core.PartySession
	isNewParty      bool
	oldPeerIDs      []string
	newPeerIDs      []string
	resharingParams *tss.ReSharingParameters
	endCh           chan *keygen.LocalPartySaveData
}

func NewECDSAResharingSession(
	walletID string,
	pubSub messaging.PubSub,
	direct messaging.DirectMessaging,
	participantPeerIDs []string,
	selfID *tss.PartyID,
	oldPartyIDs []*tss.PartyID,
	newPartyIDs []*tss.PartyID,
	threshold int,
	newThreshold int,
	preParams *keygen.LocalPreParams,
	kvStore storage.Storage,
	keyinfoStore node.KeyStore,
	resultQueue messaging.MessageQueue,
	identityStore node.IdentityStore,
	newPeerIDs []string,
	isNewParty bool,
	version int,
) core.ResharingSession {

	realPartyIDs := oldPartyIDs
	if isNewParty {
		realPartyIDs = newPartyIDs
	}

	session := &core.PartySession{
		WalletID:           walletID,
		PubSub:             pubSub,
		Direct:             direct,
		Threshold:          threshold,
		ParticipantPeerIDs: participantPeerIDs,
		SelfPartyID:        selfID,
		PartyIDs:           realPartyIDs,
		OutCh:              make(chan tss.Message),
		ErrCh:              make(chan error),
		PreParams:          preParams,
		KVStore:            kvStore,
		KeyinfoStore:       keyinfoStore,
		Version:            version,
		TopicComposer: &core.TopicComposer{
			ComposeBroadcastTopic: func() string {
				return fmt.Sprintf("resharing:broadcast:ecdsa:%s", walletID)
			},
			ComposeDirectTopic: func(fromID string, toID string) string {
				return fmt.Sprintf("resharing:direct:ecdsa:%s:%s:%s", fromID, toID, walletID)
			},
		},
		ComposeKey: func(walletID string) string {
			return fmt.Sprintf("ecdsa:%s", walletID)
		},
		GetRoundFunc:  GetMsgRound,
		ResultQueue:   resultQueue,
		SessionType:   core.SessionTypeECDSA,
		IdentityStore: identityStore,
	}
	resharingParams := tss.NewReSharingParameters(
		tss.S256(),
		tss.NewPeerContext(oldPartyIDs),
		tss.NewPeerContext(newPartyIDs),
		selfID,
		len(oldPartyIDs),
		threshold,
		len(newPartyIDs),
		newThreshold,
	)

	var oldPeerIDs []string
	for _, partyId := range oldPartyIDs {
		oldPeerIDs = append(oldPeerIDs, core.PartyIDToNodeID(partyId))
	}

	return &resharingSession{
		PartySession:    session,
		resharingParams: resharingParams,
		isNewParty:      isNewParty,
		oldPeerIDs:      oldPeerIDs,
		newPeerIDs:      newPeerIDs,
		endCh:           make(chan *keygen.LocalPartySaveData),
	}
}

// GetLegacyCommitteePeers returns peer IDs that were part of the old committee
// but are NOT part of the new committee after resharing.
// These peers are still relevant during resharing because
// they must send final share data to the new committee.
func (s *resharingSession) GetLegacyCommitteePeers() []string {
	difference := func(A, B []string) []string {
		seen := make(map[string]bool)
		for _, b := range B {
			seen[b] = true
		}
		var result []string
		for _, a := range A {
			if !seen[a] {
				result = append(result, a)
			}
		}
		return result
	}

	return difference(s.oldPeerIDs, s.newPeerIDs)
}

func (s *resharingSession) Init() error {
	logger.Infof("Initializing ecdsa resharing session with partyID: %s, newPartyIDs %s", s.GetPartyID(), s.GetPartyIDs())
	var share keygen.LocalPartySaveData

	if s.isNewParty {
		// New party → generate empty share
		share = keygen.NewLocalPartySaveData(s.GetPartyCount())
		share.LocalPreParams = *s.PreParams
	} else {
		err := s.LoadOldShareDataGeneric(s.WalletID, s.GetVersion(), &share)
		if err != nil {
			return fmt.Errorf("failed to load old share data ecdsa: %w", err)
		}
	}

	s.Party = resharing.NewLocalParty(s.resharingParams, share, s.OutCh, s.endCh)

	logger.Infof("[INITIALIZED] Initialized resharing session successfully partyID: %s, peerIDs %s, walletID %s, oldThreshold = %d, newThreshold = %d",
		s.GetPartyID(), s.GetPartyIDs(), s.WalletID, s.Threshold, s.resharingParams.NewThreshold())
	return nil
}

func (s *resharingSession) Reshare(done func()) {
	logger.Info("Starting resharing", "walletID", s.WalletID, "partyID", s.SelfPartyID)
	go func() {
		if err := s.Party.Start(); err != nil {
			s.ErrCh <- err
		}
	}()

	for {
		select {
		case saveData := <-s.endCh:
			// skip for old committee
			if saveData.ECDSAPub != nil {

				keyBytes, err := json.Marshal(saveData)
				if err != nil {
					s.ErrCh <- err
					return
				}

				newVersion := s.GetVersion() + 1
				key := s.ComposeKey(core.WalletIDWithVersion(s.WalletID, newVersion))
				if err := s.KVStore.Put(key, keyBytes); err != nil {
					s.ErrCh <- err
					return
				}

				keyInfo := node.KeyInfo{
					ParticipantPeerIDs: s.newPeerIDs,
					Threshold:          s.resharingParams.NewThreshold(),
					Version:            newVersion,
				}

				// Save key info with resharing flag
				if err := s.KeyinfoStore.Save(s.ComposeKey(s.WalletID), &keyInfo); err != nil {
					s.ErrCh <- err
					return
				}
				// Get public key
				publicKey := saveData.ECDSAPub
				pubKey := &ecdsa.PublicKey{
					Curve: publicKey.Curve(),
					X:     publicKey.X(),
					Y:     publicKey.Y(),
				}

				pubKeyBytes, err := encoding.EncodeS256PubKey(pubKey)
				if err != nil {
					logger.Error("failed to encode public key", err)
					s.ErrCh <- fmt.Errorf("failed to encode public key: %w", err)
					return
				}

				// Set the public key bytes
				s.PubkeyBytes = pubKeyBytes
				logger.Info("Generated public key bytes",
					"walletID", s.WalletID,
					"pubKeyBytes", pubKeyBytes)
			}

			done()
			err := s.Close()
			if err != nil {
				logger.Error("Failed to close session", err)
			}
			return
		case msg := <-s.OutCh:
			// Handle the message
			s.HandleTssMessage(msg)
		}
	}
}
