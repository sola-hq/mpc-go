package eddsa

import (
	"encoding/json"
	"fmt"

	"github.com/bnb-chain/tss-lib/v2/eddsa/keygen"
	"github.com/bnb-chain/tss-lib/v2/eddsa/resharing"
	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/decred/dcrd/dcrec/edwards/v2"
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

func NewEDDSAResharingSession(
	walletID string,
	pubSub messaging.PubSub,
	direct messaging.DirectMessaging,
	participantPeerIDs []string,
	selfID *tss.PartyID,
	oldPartyIDs []*tss.PartyID,
	newPartyIDs []*tss.PartyID,
	threshold int,
	newThreshold int,
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

	partySession := &core.PartySession{
		WalletID:           walletID,
		PubSub:             pubSub,
		Direct:             direct,
		Threshold:          threshold,
		Version:            version,
		ParticipantPeerIDs: participantPeerIDs,
		SelfPartyID:        selfID,
		PartyIDs:           realPartyIDs,
		OutCh:              make(chan tss.Message),
		ErrCh:              make(chan error),
		KVStore:            kvStore,
		KeyinfoStore:       keyinfoStore,
		TopicComposer: &core.TopicComposer{
			ComposeBroadcastTopic: func() string {
				return fmt.Sprintf("resharing:broadcast:eddsa:%s", walletID)
			},
			ComposeDirectTopic: func(fromID string, toID string) string {
				return fmt.Sprintf("resharing:direct:eddsa:%s:%s:%s", fromID, toID, walletID)
			},
		},
		ComposeKey: func(walletID string) string {
			return fmt.Sprintf("eddsa:%s", walletID)
		},
		GetRoundFunc:  GetMsgRound,
		ResultQueue:   resultQueue,
		SessionType:   core.SessionTypeEDDSA,
		IdentityStore: identityStore,
	}

	resharingParams := tss.NewReSharingParameters(
		tss.Edwards(),
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
		PartySession:    partySession,
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
	logger.Infof("Initializing eddsa resharing session with partyID: %s, peerIDs %s", s.SelfPartyID, s.PartyIDs)
	var share keygen.LocalPartySaveData
	if s.isNewParty {
		// Initialize empty share data for new party
		share = keygen.NewLocalPartySaveData(len(s.PartyIDs))
	} else {
		err := s.LoadOldShareDataGeneric(s.WalletID, s.GetVersion(), &share)
		if err != nil {
			return fmt.Errorf("failed to load old share data eddsa: %w", err)
		}
	}
	s.Party = resharing.NewLocalParty(s.resharingParams, share, s.OutCh, s.endCh)
	logger.Infof("[INITIALIZED] Initialized eddsa resharing session successfully partyID: %s, peerIDs %s, walletID %s, oldThreshold = %d, newThreshold = %d",
		s.SelfPartyID, s.PartyIDs, s.WalletID, s.Threshold, s.resharingParams.NewThreshold())

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
			if saveData.EDDSAPub != nil {
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

				// skip for old committee
				if saveData.EDDSAPub != nil {

					// Get public key
					publicKey := saveData.EDDSAPub
					pkX, pkY := publicKey.X(), publicKey.Y()
					pk := edwards.PublicKey{
						Curve: tss.Edwards(),
						X:     pkX,
						Y:     pkY,
					}

					pubKeyBytes := pk.SerializeCompressed()
					s.PubkeyBytes = pubKeyBytes

					logger.Info("Generated public key bytes",
						"walletID", s.WalletID,
						"pubKeyBytes", pubKeyBytes)
				}
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
