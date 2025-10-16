package ecdsa

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"

	"github.com/bnb-chain/tss-lib/v2/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/fystack/mpcium/pkg/encoding"
	"github.com/fystack/mpcium/pkg/identity"
	"github.com/fystack/mpcium/pkg/keyinfo"
	"github.com/fystack/mpcium/pkg/kvstore"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/mpc/core"
)

type ecdsaKeygenSession struct {
	*core.PartySession
	endCh chan *keygen.LocalPartySaveData
}

func NewECDSAKeygenSession(
	walletID string,
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
) core.KeyGenSession {
	return &ecdsaKeygenSession{
		PartySession: &core.PartySession{
			WalletID:           walletID,
			PubSub:             pubSub,
			Direct:             direct,
			Threshold:          threshold,
			Version:            core.DefaultVersion,
			ParticipantPeerIDs: participantPeerIDs,
			SelfPartyID:        selfID,
			PartyIDs:           partyIDs,
			OutCh:              make(chan tss.Message),
			ErrCh:              make(chan error),
			PreParams:          preParams,
			Kvstore:            kvstore,
			KeyinfoStore:       keyinfoStore,
			TopicComposer: &core.TopicComposer{
				ComposeBroadcastTopic: func() string {
					return fmt.Sprintf("keygen:broadcast:ecdsa:%s", walletID)
				},
				ComposeDirectTopic: func(fromID string, toID string) string {
					return fmt.Sprintf("keygen:direct:ecdsa:%s:%s:%s", fromID, toID, walletID)
				},
			},
			ComposeKey: func(walletID string) string {
				return fmt.Sprintf("ecdsa:%s", walletID)
			},
			GetRoundFunc:  GetEcdsaMsgRound,
			ResultQueue:   resultQueue,
			SessionType:   core.SessionTypeECDSA,
			IdentityStore: identityStore,
		},
		endCh: make(chan *keygen.LocalPartySaveData),
	}
}

func (s *ecdsaKeygenSession) Init() {
	logger.Infof("Initializing session with partyID: %s, peerIDs %s", s.GetPartyID(), s.GetPartyIDs())
	ctx := tss.NewPeerContext(s.GetPartyIDs())
	params := tss.NewParameters(tss.S256(), ctx, s.GetPartyID(), s.GetPartyCount(), s.Threshold)
	s.Party = keygen.NewLocalParty(params, s.OutCh, s.endCh, *s.PreParams)
	logger.Infof("[INITIALIZED] Initialized session successfully partyID: %s, peerIDs %s, walletID %s, threshold = %d", s.GetPartyID(), s.GetPartyIDs(), s.WalletID, s.Threshold)
}

func (s *ecdsaKeygenSession) GenerateKey(done func()) {
	logger.Info("Starting to generate key ECDSA", "walletID", s.WalletID)
	go func() {
		if err := s.Party.Start(); err != nil {
			s.ErrCh <- err
		}
	}()

	for {
		select {
		case msg := <-s.OutCh:
			s.HandleTssMessage(msg)
		case saveData := <-s.endCh:
			keyBytes, err := json.Marshal(saveData)
			if err != nil {
				s.ErrCh <- err
				return
			}

			err = s.Kvstore.Put(s.ComposeKey(core.WalletIDWithVersion(s.WalletID, s.GetVersion())), keyBytes)
			if err != nil {
				logger.Error("Failed to save key", err, "walletID", s.WalletID)
				s.ErrCh <- err
				return
			}

			keyInfo := keyinfo.KeyInfo{
				ParticipantPeerIDs: s.ParticipantPeerIDs,
				Threshold:          s.Threshold,
				Version:            s.GetVersion(),
			}

			err = s.KeyinfoStore.Save(s.ComposeKey(s.WalletID), &keyInfo)
			if err != nil {
				logger.Error("Failed to save keyinfo", err, "walletID", s.WalletID)
				s.ErrCh <- err
				return
			}

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
			s.PubkeyBytes = pubKeyBytes
			err = s.Close()
			if err != nil {
				logger.Error("Failed to close session", err)
			}
			done()
			return
		}
	}
}
