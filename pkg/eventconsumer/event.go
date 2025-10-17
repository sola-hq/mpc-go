package eventconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/fystack/mpcium/pkg/event"
	"github.com/fystack/mpcium/pkg/identity"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/mpc"
	"github.com/fystack/mpcium/pkg/mpc/core"
	"github.com/fystack/mpcium/pkg/types"
	"github.com/nats-io/nats.go"
	"github.com/spf13/viper"
)

const (
	MPCGenerateEvent  = "mpc:generate"
	MPCSignEvent      = "mpc:signing"
	MPCResharingEvent = "mpc:resharing"

	DefaultConcurrentKeygen   = 2
	DefaultConcurrentSigning  = 20
	DefaultSessionWarmUpDelay = 200

	KeyGenTimeOut = 30 * time.Second
)

type EventConsumer interface {
	Run()
	Close() error
}

type eventConsumer struct {
	node          *mpc.Node
	pubsub        messaging.PubSub
	identityStore identity.Store
	mpcThreshold  int

	keygenResultQueue    messaging.MessageQueue
	signingResultQueue   messaging.MessageQueue
	resharingResultQueue messaging.MessageQueue

	keygenSub    messaging.Subscription
	signingSub   messaging.Subscription
	resharingSub messaging.Subscription

	keygenMsgChan        chan *nats.Msg
	signingMsgChan       chan *nats.Msg
	maxConcurrentKeygen  int
	maxConcurrentSigning int

	// Track active sessions with timestamps for session cleanup routine
	activeSessions       map[string]time.Time // Maps "walletID-txID" to creation time
	sessionsLock         sync.RWMutex
	sessionWarmUpDelayMs int
	sessionInterval      time.Duration // How often to run session cleanup routine
	sessionTimeout       time.Duration // How long before a session is considered stale
	sessionStopChan      chan struct{} // Signal to stop session cleanup routine
}

func NewEventConsumer(
	node *mpc.Node,
	pubsub messaging.PubSub,
	keygenResultQueue messaging.MessageQueue,
	signingResultQueue messaging.MessageQueue,
	resharingResultQueue messaging.MessageQueue,
	identityStore identity.Store,
) EventConsumer {
	maxConcurrentKeygen := viper.GetInt("max_concurrent_keygen")
	if maxConcurrentKeygen == 0 {
		maxConcurrentKeygen = DefaultConcurrentKeygen
	}

	maxConcurrentSigning := viper.GetInt("max_concurrent_signing")
	if maxConcurrentSigning == 0 {
		maxConcurrentSigning = DefaultConcurrentSigning
	}

	sessionWarmUpDelayMs := viper.GetInt("session_warm_up_delay_ms")
	if sessionWarmUpDelayMs == 0 {
		sessionWarmUpDelayMs = DefaultSessionWarmUpDelay
	}

	logger.Info(
		"Initializing event consumer",
		"max_concurrent_keygen",
		maxConcurrentKeygen,
		"max_concurrent_signing",
		maxConcurrentSigning,
		"session_warm_up_delay_ms",
		sessionWarmUpDelayMs,
	)

	ec := &eventConsumer{
		node:                 node,
		pubsub:               pubsub,
		keygenResultQueue:    keygenResultQueue,
		signingResultQueue:   signingResultQueue,
		resharingResultQueue: resharingResultQueue,
		activeSessions:       make(map[string]time.Time),
		sessionInterval:      5 * time.Minute,     // Run cleanup every 5 minutes
		sessionTimeout:       30 * time.Minute,    // Consider sessions older than 30 minutes stale
		sessionStopChan:      make(chan struct{}), // Signal to stop session cleanup routine
		mpcThreshold:         viper.GetInt("mpc_threshold"),
		maxConcurrentKeygen:  maxConcurrentKeygen,
		maxConcurrentSigning: maxConcurrentSigning,
		identityStore:        identityStore,
		keygenMsgChan:        make(chan *nats.Msg, 100),
		signingMsgChan:       make(chan *nats.Msg, 200), // Larger buffer for signing
		sessionWarmUpDelayMs: sessionWarmUpDelayMs,
	}

	go ec.startKeyGenEventWorker()
	go ec.startSigningEventWorker()
	// Start background cleanup goroutine
	go ec.sessionCleanupRoutine()

	return ec
}

func (ec *eventConsumer) Run() {
	err := ec.consumeKeyGenerationEvent()
	if err != nil {
		log.Fatal("Failed to consume key reconstruction event", err)
	}

	err = ec.consumeTxSigningEvent()
	if err != nil {
		log.Fatal("Failed to consume tx signing event", err)
	}

	err = ec.consumeResharingEvent()
	if err != nil {
		log.Fatal("Failed to consume resharing event", err)
	}

	logger.Info("MPC Event consumer started...!")
}

func (ec *eventConsumer) warmUpSession() {
	time.Sleep(time.Duration(ec.sessionWarmUpDelayMs) * time.Millisecond)
}

func (ec *eventConsumer) handleKeyGenEvent(natMsg *nats.Msg) {
	baseCtx, baseCancel := context.WithTimeout(context.Background(), KeyGenTimeOut)
	defer baseCancel()

	raw := natMsg.Data
	var msg types.GenerateKeyMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		logger.Error("Failed to unmarshal keygen message", err)
		ec.handleKeygenSessionError(msg.WalletID, err, "Failed to unmarshal keygen message", natMsg)
		return
	}

	if err := ec.identityStore.VerifyInitiatorMessage(&msg); err != nil {
		logger.Error("Failed to verify initiator message", err)
		ec.handleKeygenSessionError(msg.WalletID, err, "Failed to verify initiator message", natMsg)
		return
	}

	walletID := msg.WalletID
	ecdsaSession, err := ec.node.CreateKeyGenSession(core.SessionTypeECDSA, walletID, ec.mpcThreshold, ec.keygenResultQueue)
	if err != nil {
		ec.handleKeygenSessionError(walletID, err, "Failed to create ECDSA key generation session", natMsg)
		return
	}
	eddsaSession, err := ec.node.CreateKeyGenSession(core.SessionTypeEDDSA, walletID, ec.mpcThreshold, ec.keygenResultQueue)
	if err != nil {
		ec.handleKeygenSessionError(walletID, err, "Failed to create EdDSA key generation session", natMsg)
		return
	}
	ecdsaSession.Init()
	eddsaSession.Init()

	ctxEcdsa, doneEcdsa := context.WithCancel(baseCtx)
	ctxEddsa, doneEddsa := context.WithCancel(baseCtx)

	successEvent := &event.KeygenResultEvent{WalletID: walletID, ResultType: event.ResultTypeSuccess}
	var wg sync.WaitGroup
	wg.Add(2)

	// Channel to communicate errors from goroutines to main function
	errorChan := make(chan error, 2)

	go func() {
		defer wg.Done()
		select {
		case <-ctxEcdsa.Done():
			successEvent.ECDSAPubKey = ecdsaSession.GetPubKeyResult()
		case err := <-ecdsaSession.GetErrChan():
			logger.Error("ECDSA keygen session error", err)
			ec.handleKeygenSessionError(walletID, err, "ECDSA keygen session error", natMsg)
			errorChan <- err
			doneEcdsa()
		}
	}()
	go func() {
		defer wg.Done()
		select {
		case <-ctxEddsa.Done():
			successEvent.EDDSAPubKey = eddsaSession.GetPubKeyResult()
		case err := <-eddsaSession.GetErrChan():
			logger.Error("EdDSA keygen session error", err)
			ec.handleKeygenSessionError(walletID, err, "EdDSA keygen session error", natMsg)
			errorChan <- err
			doneEddsa()
		}
	}()

	ecdsaSession.ListenToIncomingMessageAsync()
	eddsaSession.ListenToIncomingMessageAsync()

	// Temporary delay for peer setup
	ec.warmUpSession()
	go ecdsaSession.GenerateKey(doneEcdsa)
	go eddsaSession.GenerateKey(doneEddsa)

	// Wait for completion or timeout
	doneAll := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneAll)
	}()

	select {
	case <-doneAll:
		// Check if any errors occurred during execution
		select {
		case <-errorChan:
			// Error already handled by the goroutine, just return early
			return
		default:
			// No errors, continue with success
		}
	case <-baseCtx.Done():
		// timeout occurred
		logger.Warn("Key generation timed out", "walletID", walletID, "timeout", KeyGenTimeOut)
		ec.handleKeygenSessionError(walletID, fmt.Errorf("keygen session timed out after %v", KeyGenTimeOut), "Key generation timed out", natMsg)
		return
	}

	payload, err := json.Marshal(successEvent)
	if err != nil {
		logger.Error("Failed to marshal keygen success event", err)
		ec.handleKeygenSessionError(walletID, err, "Failed to marshal keygen success event", natMsg)
		return
	}

	key := fmt.Sprintf(core.TypeGenerateWalletResultFmt, walletID)
	if err := ec.keygenResultQueue.Enqueue(
		key,
		payload,
		&messaging.EnqueueOptions{IdempotentKey: composeKeygenResultIdempotentKey(walletID, natMsg)},
	); err != nil {
		logger.Error("Failed to publish key generation success message", err)
		ec.handleKeygenSessionError(walletID, err, "Failed to publish key generation success message", natMsg)
		return
	}
	ec.sendReplyToRemoveMsg(natMsg)
	logger.Info("[COMPLETED KEY GEN] Key generation completed successfully", "walletID", walletID)
}

// handleKeygenSessionError handles errors that occur during key generation
func (ec *eventConsumer) handleKeygenSessionError(walletID string, err error, contextMsg string, natMsg *nats.Msg) {
	fullErrMsg := fmt.Sprintf("%s: %v", contextMsg, err)
	errorCode := event.GetErrorCodeFromError(err)
	keygenResult := event.KeygenResultEvent{
		ResultType:  event.ResultTypeError,
		ErrorCode:   string(errorCode),
		WalletID:    walletID,
		ErrorReason: fullErrMsg,
	}

	keygenResultBytes, err := json.Marshal(keygenResult)
	if err != nil {
		logger.Error("Failed to marshal keygen result event", err,
			"walletID", walletID,
		)
		return
	}

	key := fmt.Sprintf(core.TypeGenerateWalletResultFmt, walletID)
	err = ec.keygenResultQueue.Enqueue(key, keygenResultBytes, &messaging.EnqueueOptions{
		IdempotentKey: composeKeygenResultIdempotentKey(walletID, natMsg),
	})
	if err != nil {
		logger.Error("Failed to enqueue keygen result event", err,
			"walletID", walletID,
			"payload", string(keygenResultBytes),
		)
	}
	ec.sendReplyToRemoveMsg(natMsg)
}

func (ec *eventConsumer) startKeyGenEventWorker() {
	// semaphore to limit concurrency
	semaphore := make(chan struct{}, ec.maxConcurrentKeygen)

	for natMsg := range ec.keygenMsgChan {
		semaphore <- struct{}{} // acquire a slot
		go func(msg *nats.Msg) {
			defer func() { <-semaphore }() // release the slot when done
			ec.handleKeyGenEvent(msg)
		}(natMsg)
	}
}

func (ec *eventConsumer) startSigningEventWorker() {
	// semaphore to limit concurrency
	semaphore := make(chan struct{}, ec.maxConcurrentSigning)

	for natMsg := range ec.signingMsgChan {
		semaphore <- struct{}{} // acquire a slot
		go func(msg *nats.Msg) {
			defer func() { <-semaphore }() // release the slot when done
			ec.handleSigningEvent(msg)
		}(natMsg)
	}
}

func (ec *eventConsumer) consumeKeyGenerationEvent() error {
	sub, err := ec.pubsub.Subscribe(MPCGenerateEvent, func(natMsg *nats.Msg) {
		ec.keygenMsgChan <- natMsg
	})

	ec.keygenSub = sub
	if err != nil {
		return err
	}
	return nil
}

func (ec *eventConsumer) handleSigningEvent(natMsg *nats.Msg) {
	raw := natMsg.Data
	var msg types.SignTxMessage
	err := json.Unmarshal(raw, &msg)
	if err != nil {
		logger.Error("Failed to unmarshal signing message", err)
		return
	}

	err = ec.identityStore.VerifyInitiatorMessage(&msg)
	if err != nil {
		logger.Error("Failed to verify initiator message", err)
		return
	}

	logger.Info(
		"Received signing event",
		"walletID",
		msg.WalletID,
		"type",
		msg.KeyType,
		"tx",
		msg.TxID,
		"Id",
		ec.node.ID(),
	)

	// Check for duplicate session and track if new
	if ec.checkDuplicateSession(msg.WalletID, msg.TxID) {
		duplicateErr := fmt.Errorf("duplicate signing request detected for walletID=%s txID=%s", msg.WalletID, msg.TxID)
		ec.handleSigningSessionError(
			msg.WalletID,
			msg.TxID,
			msg.NetworkInternalCode,
			duplicateErr,
			"Duplicate session",
			natMsg,
		)
		return
	}

	var session core.SigningSession
	idempotentKey := composeSigningResultIdempotentKey(msg.TxID, natMsg)
	var sessionErr error
	switch msg.KeyType {
	case types.KeyTypeSecp256k1:
		session, sessionErr = ec.node.CreateSigningSession(
			core.SessionTypeECDSA,
			msg.WalletID,
			msg.TxID,
			msg.NetworkInternalCode,
			ec.signingResultQueue,
			idempotentKey,
		)
	case types.KeyTypeEd25519:
		session, sessionErr = ec.node.CreateSigningSession(
			core.SessionTypeEDDSA,
			msg.WalletID,
			msg.TxID,
			msg.NetworkInternalCode,
			ec.signingResultQueue,
			idempotentKey,
		)
	default:
		sessionErr = fmt.Errorf("unsupported key type: %s", msg.KeyType)
	}
	if sessionErr != nil {
		if errors.Is(sessionErr, core.ErrNotEnoughParticipants) {
			logger.Info(
				"RETRY LATER: Not enough participants to sign",
				"walletID", msg.WalletID,
				"txID", msg.TxID,
				"nodeID", ec.node.ID(),
			)
			// Return for retry later
			return
		}

		if errors.Is(sessionErr, core.ErrNotInParticipantList) {
			logger.Info("Node is not in participant list for this wallet, skipping signing",
				"walletID", msg.WalletID,
				"txID", msg.TxID,
				"nodeID", ec.node.ID(),
			)
			// Skip signing instead of treating as error
			return
		}

		ec.handleSigningSessionError(
			msg.WalletID,
			msg.TxID,
			msg.NetworkInternalCode,
			sessionErr,
			"Failed to create signing session",
			natMsg,
		)
		return
	}

	txBigInt := new(big.Int).SetBytes(msg.Tx)
	err = session.Init(txBigInt)
	if err != nil {
		ec.handleSigningSessionError(
			msg.WalletID,
			msg.TxID,
			msg.NetworkInternalCode,
			err,
			"Failed to init signing session",
			natMsg,
		)
		return
	}

	// Mark session as already processed
	ec.addSession(msg.WalletID, msg.TxID)

	ctx, done := context.WithCancel(context.Background())
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-session.GetErrChan():
				if err != nil {
					ec.handleSigningSessionError(
						msg.WalletID,
						msg.TxID,
						msg.NetworkInternalCode,
						err,
						"Failed to sign tx",
						natMsg,
					)
					return
				}
			}
		}
	}()

	session.ListenToIncomingMessageAsync()
	// TODO: use consul distributed lock here, only sign after all nodes has already completed listing to incoming message async
	// The purpose of the sleep is to be ensuring that the node has properly set up its message listeners
	// before it starts the signing process. If the signing process starts sending messages before other nodes
	// have set up their listeners, those messages might be missed, potentially causing the signing process to fail.
	// One solution:
	// The messaging includes mechanisms for direct point-to-point communication (in point2point.go).
	// The nodes could explicitly coordinate through request-response patterns before starting signing
	ec.warmUpSession()

	onSuccess := func(data []byte) {
		done()
		ec.sendReplyToRemoveMsg(natMsg)
	}
	go session.Sign(onSuccess)
}

func (ec *eventConsumer) consumeTxSigningEvent() error {
	sub, err := ec.pubsub.Subscribe(MPCSignEvent, func(natMsg *nats.Msg) {
		ec.signingMsgChan <- natMsg // Send to worker instead of processing directly
	})

	ec.signingSub = sub
	if err != nil {
		return err
	}

	return nil
}
func (ec *eventConsumer) handleSigningSessionError(walletID, txID, networkInternalCode string, err error, contextMsg string, natMsg *nats.Msg) {
	fullErrMsg := fmt.Sprintf("%s: %v", contextMsg, err)
	errorCode := event.GetErrorCodeFromError(err)

	logger.Warn("Signing session error",
		"walletID", walletID,
		"txID", txID,
		"networkInternalCode", networkInternalCode,
		"error", err.Error(),
		"errorCode", errorCode,
		"context", contextMsg,
	)

	signingResult := event.SigningResultEvent{
		ResultType:          event.ResultTypeError,
		ErrorCode:           errorCode,
		NetworkInternalCode: networkInternalCode,
		WalletID:            walletID,
		TxID:                txID,
		ErrorReason:         fullErrMsg,
	}

	signingResultBytes, err := json.Marshal(signingResult)
	if err != nil {
		logger.Error("Failed to marshal signing result event", err,
			"walletID", walletID,
			"txID", txID,
		)
		return
	}
	err = ec.signingResultQueue.Enqueue(event.SigningResultCompleteTopic, signingResultBytes, &messaging.EnqueueOptions{
		IdempotentKey: composeSigningResultIdempotentKey(txID, natMsg),
	})
	if err != nil {
		logger.Error("Failed to enqueue signing result event", err,
			"walletID", walletID,
			"txID", txID,
			"payload", string(signingResultBytes),
		)
	}
	ec.sendReplyToRemoveMsg(natMsg)
}

func (ec *eventConsumer) sendReplyToRemoveMsg(natMsg *nats.Msg) {
	msg := natMsg.Data

	if natMsg.Reply == "" {
		logger.Warn("No reply inbox specified for sign success message", "msg", string(msg))
		return
	}

	err := ec.pubsub.Publish(natMsg.Reply, msg)
	if err != nil {
		logger.Error("Failed to reply message", err, "reply", natMsg.Reply)
		return
	}
}

func (ec *eventConsumer) consumeResharingEvent() error {
	sub, err := ec.pubsub.Subscribe(MPCResharingEvent, func(natMsg *nats.Msg) {
		var msg types.ResharingMessage
		if err := json.Unmarshal(natMsg.Data, &msg); err != nil {
			logger.Error("Failed to unmarshal resharing message", err)
			ec.handleResharingSessionError(msg.WalletID, msg.KeyType, msg.NewThreshold, err, "Failed to unmarshal resharing message", natMsg)
			return
		}

		if msg.SessionID == "" {
			ec.handleResharingSessionError(
				msg.WalletID,
				msg.KeyType,
				msg.NewThreshold,
				errors.New("validation: session ID is empty"),
				"Session ID is empty",
				natMsg,
			)
			return
		}

		if err := ec.identityStore.VerifyInitiatorMessage(&msg); err != nil {
			logger.Error("Failed to verify initiator message", err)
			ec.handleResharingSessionError(msg.WalletID, msg.KeyType, msg.NewThreshold, err, "Failed to verify initiator message", natMsg)
			return
		}

		walletID := msg.WalletID
		keyType := msg.KeyType

		sessionType, err := sessionTypeFromKeyType(keyType)
		if err != nil {
			logger.Error("Failed to get session type", err)
			ec.handleResharingSessionError(walletID, keyType, msg.NewThreshold, err, "Failed to get session type", natMsg)
			return
		}

		createSession := func(isNewPeer bool) (core.ResharingSession, error) {
			return ec.node.CreateResharingSession(
				sessionType,
				walletID,
				msg.NewThreshold,
				msg.NodeIDs,
				isNewPeer,
				ec.resharingResultQueue,
			)
		}

		oldSession, err := createSession(false)
		if err != nil {
			logger.Error("Failed to create old resharing session", err, "walletID", walletID)
			ec.handleResharingSessionError(walletID, keyType, msg.NewThreshold, err, "Failed to create old resharing session", natMsg)
			return
		}
		newSession, err := createSession(true)
		if err != nil {
			logger.Error("Failed to create new resharing session", err, "walletID", walletID)
			ec.handleResharingSessionError(walletID, keyType, msg.NewThreshold, err, "Failed to create new resharing session", natMsg)
			return
		}

		if oldSession == nil && newSession == nil {
			logger.Info("Node is not participating in this resharing (neither old nor new)", "walletID", walletID)
			return
		}

		ctx := context.Background()
		var wg sync.WaitGroup

		successEvent := &event.ResharingResultEvent{
			WalletID:     walletID,
			NewThreshold: msg.NewThreshold,
			KeyType:      msg.KeyType,
			ResultType:   event.ResultTypeSuccess,
		}

		if oldSession != nil {
			err := oldSession.Init()
			if err != nil {
				ec.handleResharingSessionError(walletID, keyType, msg.NewThreshold, err, "Failed to init old resharing session", natMsg)
				return
			}
			oldSession.ListenToIncomingMessageAsync()
		}

		if newSession != nil {
			err := newSession.Init()
			if err != nil {
				ec.handleResharingSessionError(walletID, keyType, msg.NewThreshold, err, "Failed to init new resharing session", natMsg)
				return
			}
			newSession.ListenToIncomingMessageAsync()
			// In resharing process, we need to ensure that the new session is aware of the old committee peers.
			// Then new committee peers can start listening to the old committee peers
			// and thus enable receiving direct messages from them.
			extraOldCommitteePeers := newSession.GetLegacyCommitteePeers()
			newSession.ListenToPeersAsync(extraOldCommitteePeers)
		}

		ec.warmUpSession()
		if oldSession != nil {
			ctxOld, doneOld := context.WithCancel(ctx)
			go oldSession.Reshare(doneOld)

			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-ctxOld.Done():
						return
					case err := <-oldSession.GetErrChan():
						logger.Error("Old reshare session error", err)
						ec.handleResharingSessionError(walletID, keyType, msg.NewThreshold, err, "Old reshare session error", natMsg)
						doneOld()
						return
					}
				}
			}()
		}

		if newSession != nil {
			ctxNew, doneNew := context.WithCancel(ctx)
			go newSession.Reshare(doneNew)
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-ctxNew.Done():
						successEvent.PubKey = newSession.GetPubKeyResult()
						return
					case err := <-newSession.GetErrChan():
						logger.Error("New reshare session error", err)
						ec.handleResharingSessionError(walletID, keyType, msg.NewThreshold, err, "New reshare session error", natMsg)
						doneNew()
						return
					}
				}
			}()
		}

		wg.Wait()
		logger.Info("Reshare session finished", "walletID", walletID, "pubKey", fmt.Sprintf("%x", successEvent.PubKey))

		if newSession != nil {
			successBytes, err := json.Marshal(successEvent)
			if err != nil {
				logger.Error("Failed to marshal resharing success event", err)
				ec.handleResharingSessionError(walletID, keyType, msg.NewThreshold, err, "Failed to marshal reshare success event", natMsg)
				return
			}

			key := fmt.Sprintf(core.TypeResharingWalletResultFmt, msg.SessionID)
			err = ec.resharingResultQueue.Enqueue(
				key,
				successBytes,
				&messaging.EnqueueOptions{
					IdempotentKey: composeResharingResultIdempotentKey(msg.SessionID, natMsg),
				})
			if err != nil {
				logger.Error("Failed to publish resharing success message", err)
				ec.handleResharingSessionError(walletID, keyType, msg.NewThreshold, err, "Failed to publish reshare success message", natMsg)
				return
			}
			logger.Info("[COMPLETED RESHARE] Successfully published", "walletID", walletID)
		} else {
			logger.Info("[COMPLETED RESHARE] Done (not a new party)", "walletID", walletID)
		}
	})

	ec.resharingSub = sub
	return err
}

// handleResharingSessionError handles errors that occur during resharing operations
func (ec *eventConsumer) handleResharingSessionError(
	walletID string,
	keyType types.KeyType,
	newThreshold int,
	err error,
	contextMsg string,
	natMsg *nats.Msg,
) {
	fullErrMsg := fmt.Sprintf("%s: %v", contextMsg, err)
	errorCode := event.GetErrorCodeFromError(err)

	logger.Warn("Resharing session error",
		"walletID", walletID,
		"keyType", keyType,
		"newThreshold", newThreshold,
		"error", err.Error(),
		"errorCode", errorCode,
		"context", contextMsg,
	)

	resharingResult := event.ResharingResultEvent{
		ResultType:   event.ResultTypeError,
		ErrorCode:    string(errorCode),
		WalletID:     walletID,
		KeyType:      keyType,
		NewThreshold: newThreshold,
		ErrorReason:  fullErrMsg,
	}

	resharingResultBytes, err := json.Marshal(resharingResult)
	if err != nil {
		logger.Error("Failed to marshal resharing result event", err,
			"walletID", walletID,
		)
		return
	}

	key := fmt.Sprintf(core.TypeResharingWalletResultFmt, walletID)
	err = ec.resharingResultQueue.Enqueue(key, resharingResultBytes, &messaging.EnqueueOptions{
		IdempotentKey: composeResharingResultIdempotentKey(walletID, natMsg),
	})
	if err != nil {
		logger.Error("Failed to enqueue resharing result event", err,
			"walletID", walletID,
			"payload", string(resharingResultBytes),
		)
	}
}

// Add a cleanup routine that runs periodically
func (ec *eventConsumer) sessionCleanupRoutine() {
	ticker := time.NewTicker(ec.sessionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ec.cleanupStaleSessions()
		case <-ec.sessionStopChan:
			return
		}
	}
}

// Cleanup stale sessions
func (ec *eventConsumer) cleanupStaleSessions() {
	now := time.Now()
	ec.sessionsLock.Lock()
	defer ec.sessionsLock.Unlock()

	for sessionID, creationTime := range ec.activeSessions {
		if now.Sub(creationTime) > ec.sessionTimeout {
			delete(ec.activeSessions, sessionID)
		}
	}
}

// markSessionAsActive marks a session as active with the current timestamp
func (ec *eventConsumer) addSession(walletID, txID string) {
	sessionID := fmt.Sprintf("%s-%s", walletID, txID)
	ec.sessionsLock.Lock()
	ec.activeSessions[sessionID] = time.Now()
	ec.sessionsLock.Unlock()
}

// checkAndTrackSession checks if a session already exists and tracks it if new.
// Returns true if the session is a duplicate.
func (ec *eventConsumer) checkDuplicateSession(walletID, txID string) bool {
	sessionID := fmt.Sprintf("%s-%s", walletID, txID)

	// Check for duplicate
	ec.sessionsLock.RLock()
	_, isDuplicate := ec.activeSessions[sessionID]
	ec.sessionsLock.RUnlock()

	if isDuplicate {
		logger.Info("Duplicate signing request detected", "walletID", walletID, "txID", txID)
		return true
	}

	return false
}

// Close and clean up
func (ec *eventConsumer) Close() error {
	// Signal cleanup routine to stop
	close(ec.sessionStopChan)

	// Close message buffers to stop workers
	close(ec.keygenMsgChan)
	close(ec.signingMsgChan)

	err := ec.keygenSub.Unsubscribe()
	if err != nil {
		return err
	}
	err = ec.signingSub.Unsubscribe()
	if err != nil {
		return err
	}
	err = ec.resharingSub.Unsubscribe()
	if err != nil {
		return err
	}

	return nil
}

func sessionTypeFromKeyType(keyType types.KeyType) (core.SessionType, error) {
	switch keyType {
	case types.KeyTypeSecp256k1:
		return core.SessionTypeECDSA, nil
	case types.KeyTypeEd25519:
		return core.SessionTypeEDDSA, nil
	default:
		logger.Warn("Unsupported key type", "keyType", keyType)
		return "", fmt.Errorf("unsupported key type: %v", keyType)
	}
}

// composeIdempotentKey creates an idempotent key for different MPC operation types
func composeIdempotentKey(baseID string, natMsg *nats.Msg, formatTemplate string) string {
	var uniqueKey string
	sid := natMsg.Header.Get("SessionID")
	if sid != "" {
		uniqueKey = fmt.Sprintf("%s:%s", baseID, sid)
	} else {
		uniqueKey = baseID
	}
	return fmt.Sprintf(formatTemplate, uniqueKey)
}

func composeKeygenResultIdempotentKey(walletID string, natMsg *nats.Msg) string {
	return composeIdempotentKey(walletID, natMsg, core.TypeGenerateWalletResultFmt)
}

func composeSigningResultIdempotentKey(txID string, natMsg *nats.Msg) string {
	return composeIdempotentKey(txID, natMsg, core.TypeSigningResultFmt)
}

func composeResharingResultIdempotentKey(sessionID string, natMsg *nats.Msg) string {
	return composeIdempotentKey(sessionID, natMsg, core.TypeResharingWalletResultFmt)
}
