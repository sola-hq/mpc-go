package eddsa

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/bnb-chain/tss-lib/v2/common"
	"github.com/bnb-chain/tss-lib/v2/eddsa/keygen"
	"github.com/bnb-chain/tss-lib/v2/eddsa/signing"
	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/fystack/mpcium/pkg/common/errors"
	"github.com/fystack/mpcium/pkg/event"
	"github.com/fystack/mpcium/pkg/identity"
	"github.com/fystack/mpcium/pkg/keyinfo"
	"github.com/fystack/mpcium/pkg/kvstore"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/mpc/core"
	"github.com/samber/lo"
)

type eddsaSigningSession struct {
	*core.PartySession
	endCh chan *common.SignatureData
	data  *keygen.LocalPartySaveData
	tx    *big.Int
	txID  string
}

func NewEDDSASigningSession(
	walletID string,
	txID string,
	pubSub messaging.PubSub,
	direct messaging.DirectMessaging,
	participantPeerIDs []string,
	selfID *tss.PartyID,
	partyIDs []*tss.PartyID,
	threshold int,
	kvstore kvstore.KVStore,
	keyinfoStore keyinfo.Store,
	resultQueue messaging.MessageQueue,
	identityStore identity.Store,
	idempotentKey string,
) core.SigningSession {
	return &eddsaSigningSession{
		PartySession: &core.PartySession{
			WalletID:           walletID,
			PubSub:             pubSub,
			Direct:             direct,
			Threshold:          threshold,
			ParticipantPeerIDs: participantPeerIDs,
			SelfPartyID:        selfID,
			PartyIDs:           partyIDs,
			OutCh:              make(chan tss.Message),
			ErrCh:              make(chan error),
			Kvstore:            kvstore,
			KeyinfoStore:       keyinfoStore,
			TopicComposer: &core.TopicComposer{
				ComposeBroadcastTopic: func() string {
					return fmt.Sprintf("sign:eddsa:broadcast:%s:%s", walletID, txID)
				},
				ComposeDirectTopic: func(fromID string, toID string) string {
					return fmt.Sprintf("sign:eddsa:direct:%s:%s:%s", fromID, toID, txID)
				},
			},
			ComposeKey: func(waleltID string) string {
				return fmt.Sprintf("eddsa:%s", waleltID)
			},
			GetRoundFunc:  GetEddsaMsgRound,
			ResultQueue:   resultQueue,
			IdentityStore: identityStore,
			IdempotentKey: idempotentKey,
			SessionType:   core.SessionTypeEDDSA,
		},
		endCh: make(chan *common.SignatureData),
		txID:  txID,
	}
}

func (s *eddsaSigningSession) Init(tx *big.Int) error {
	logger.Infof("Initializing signing session with partyID: %s, peerIDs %s", s.SelfPartyID, s.PartyIDs)
	ctx := tss.NewPeerContext(s.PartyIDs)
	params := tss.NewParameters(tss.Edwards(), ctx, s.SelfPartyID, len(s.PartyIDs), s.Threshold)

	keyInfo, err := s.KeyinfoStore.Get(s.ComposeKey(s.WalletID))
	if err != nil {
		return errors.Wrap(err, "Failed to get key info data")
	}

	if len(s.ParticipantPeerIDs) < keyInfo.Threshold+1 {
		logger.Warn("Not enough participants to sign, expected %d, got %d", keyInfo.Threshold+1, len(s.ParticipantPeerIDs))
		return core.ErrNotEnoughParticipants
	}

	// check if t+1 participants are present
	result := lo.Intersect(s.ParticipantPeerIDs, keyInfo.ParticipantPeerIDs)
	if len(result) < keyInfo.Threshold+1 {
		return fmt.Errorf(
			"incompatible peerIDs to participate in signing. Current participants: %v, expected participants: %v",
			s.ParticipantPeerIDs,
			keyInfo.ParticipantPeerIDs,
		)
	}

	logger.Info("Have enough participants to sign", "participants", s.ParticipantPeerIDs)
	key := s.ComposeKey(core.WalletIDWithVersion(s.WalletID, keyInfo.Version))
	keyData, err := s.Kvstore.Get(key)
	if err != nil {
		return errors.Wrap(err, "Failed to get wallet data from KVStore")
	}
	// Check if all the participants of the key are present
	var data keygen.LocalPartySaveData
	err = json.Unmarshal(keyData, &data)
	if err != nil {
		return errors.Wrap(err, "Failed to unmarshal wallet data")
	}

	s.Party = signing.NewLocalParty(tx, params, data, s.OutCh, s.endCh)
	s.data = &data
	s.Version = keyInfo.Version
	s.tx = tx
	logger.Info("Initialized sigining session successfully!")
	return nil
}

func (s *eddsaSigningSession) Sign(onSuccess func(data []byte)) {
	logger.Info("Starting signing", "walletID", s.WalletID)
	go func() {
		if err := s.Party.Start(); err != nil {
			s.ErrCh <- err
		}
	}()

	for {

		select {
		case msg := <-s.OutCh:
			s.HandleTssMessage(msg)
		case sig := <-s.endCh:
			publicKey := *s.data.EDDSAPub
			pk := edwards.PublicKey{
				Curve: tss.Edwards(),
				X:     publicKey.X(),
				Y:     publicKey.Y(),
			}

			ok := edwards.Verify(&pk, s.tx.Bytes(), new(big.Int).SetBytes(sig.R), new(big.Int).SetBytes(sig.S))
			if !ok {
				s.ErrCh <- errors.New("Failed to verify signature")
				return
			}

			r := event.SigningResultEvent{
				ResultType: event.ResultTypeSuccess,
				WalletID:   s.WalletID,
				TxID:       s.txID,
				Signature:  sig.Signature,
			}

			bytes, err := json.Marshal(r)
			if err != nil {
				s.ErrCh <- errors.Wrap(err, "Failed to marshal raw signature")
				return
			}

			err = s.ResultQueue.Enqueue(event.SigningResultCompleteTopic, bytes, &messaging.EnqueueOptions{
				IdempotentKey: s.IdempotentKey,
			})
			if err != nil {
				s.ErrCh <- errors.Wrap(err, "Failed to publish sign success message")
				return
			}

			logger.Info("[SIGN] Sign successfully", "walletID", s.WalletID)

			err = s.Close()
			if err != nil {
				logger.Error("Failed to close session", err)
			}

			onSuccess(bytes)
			return
		}

	}
}
