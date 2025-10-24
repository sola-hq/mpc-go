package consumer

import (
	"encoding/json"
	"fmt"

	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/types"
	"github.com/nats-io/nats.go"
)

// Constants for timeout handling
const (
	maxDeliveriesExceededSubject = "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>"

	// Error messages
	errorMsgSigningTimeout   = "Signing failed: maximum delivery attempts exceeded"
	errorMsgResharingTimeout = "Resharing failed: maximum delivery attempts exceeded"
	errorMsgKeygenTimeout    = "Keygen failed: maximum delivery attempts exceeded"
)

// StreamHandler defines the interface for handling different stream types
type StreamHandler interface {
	HandleTimeout(stream string, streamSeq uint64, data []byte) (idempotentKey string, err error)
}

// SigningStreamHandler handles signing stream timeouts
type SigningStreamHandler struct {
	messageQueue messaging.MessageQueue
}

func (h *SigningStreamHandler) HandleTimeout(stream string, streamSeq uint64, data []byte) (idempotentKey string, err error) {
	var signResponse types.SigningResponse
	if err := json.Unmarshal(data, &signResponse); err != nil {
		return "", fmt.Errorf("failed to unmarshal signing response: %w", err)
	}

	signResponse.ErrorCode = types.ErrorCodeMaxDeliveryAttempts
	signResponse.IsTimeout = true
	signResponse.ErrorReason = errorMsgSigningTimeout

	signResponseBytes, err := json.Marshal(signResponse)
	if err != nil {
		err = fmt.Errorf("failed to marshal signing response: %w", err)
		return
	}

	err = h.messageQueue.Enqueue(
		messaging.SigningResultTopic,
		signResponseBytes,
		&messaging.EnqueueOptions{
			IdempotentKey: signResponse.TxID,
		},
	)
	if err != nil {
		err = fmt.Errorf("failed to enqueue signing response: %w", err)
		return
	}
	return signResponse.TxID, nil
}

// ResharingStreamHandler handles resharing stream timeouts
type ResharingStreamHandler struct {
	messageQueue messaging.MessageQueue
}

func (h *ResharingStreamHandler) HandleTimeout(stream string, streamSeq uint64, data []byte) (idempotentKey string, err error) {
	var resharingResponse types.ResharingResponse
	if err := json.Unmarshal(data, &resharingResponse); err != nil {
		return "", fmt.Errorf("failed to unmarshal resharing response: %w", err)
	}

	resharingResponse.ErrorCode = types.ErrorCodeMaxDeliveryAttempts
	resharingResponse.ErrorReason = errorMsgResharingTimeout

	resharingResponseBytes, err := json.Marshal(resharingResponse)
	if err != nil {
		err = fmt.Errorf("failed to marshal resharing response: %w", err)
		return
	}

	err = h.messageQueue.Enqueue(messaging.ResharingResultTopic, resharingResponseBytes, &messaging.EnqueueOptions{
		IdempotentKey: resharingResponse.WalletID,
	})
	if err != nil {
		err = fmt.Errorf("failed to enqueue resharing response: %w", err)
		return
	}
	return resharingResponse.WalletID, nil
}

// KeygenStreamHandler handles keygen stream timeouts
type KeygenStreamHandler struct {
	messageQueue messaging.MessageQueue
}

func (h *KeygenStreamHandler) HandleTimeout(stream string, streamSeq uint64, data []byte) (idempotentKey string, err error) {
	var keygenResponse types.KeygenResponse
	if err := json.Unmarshal(data, &keygenResponse); err != nil {
		return "", fmt.Errorf("failed to unmarshal keygen response: %w", err)
	}

	keygenResponse.ErrorCode = types.ErrorCodeMaxDeliveryAttempts
	keygenResponse.ErrorReason = errorMsgKeygenTimeout

	keygenResponseBytes, err := json.Marshal(keygenResponse)
	if err != nil {
		return "", fmt.Errorf("failed to marshal keygen response: %w", err)
	}

	err = h.messageQueue.Enqueue(
		messaging.KeygenResultTopic,
		keygenResponseBytes,
		&messaging.EnqueueOptions{
			IdempotentKey: keygenResponse.WalletID,
		},
	)
	if err != nil {
		err = fmt.Errorf("failed to enqueue keygen response: %w", err)
		return
	}
	return keygenResponse.WalletID, nil
}

// timeOutConsumer handles timeout events for various MPC operations
type timeOutConsumer struct {
	natsConn     *nats.Conn
	messageQueue messaging.MessageQueue
	subscription messaging.Subscription
	handlers     map[string]StreamHandler
}

// NewTimeOutConsumer creates a new timeout consumer
func NewTimeOutConsumer(natsConn *nats.Conn, messageQueue messaging.MessageQueue) *timeOutConsumer {
	handlers := map[string]StreamHandler{
		messaging.SigningBrokerStream: &SigningStreamHandler{
			messageQueue: messageQueue,
		},
		messaging.ResharingBrokerStream: &ResharingStreamHandler{
			messageQueue: messageQueue,
		},
		messaging.KeygenBrokerStream: &KeygenStreamHandler{
			messageQueue: messageQueue,
		},
	}

	return &timeOutConsumer{
		natsConn:     natsConn,
		messageQueue: messageQueue,
		handlers:     handlers,
	}
}

// Run starts the timeout consumer
func (tc *timeOutConsumer) Run() {
	logger.Info("Starting advisory consumer for max deliveries exceeded")

	sub, err := tc.natsConn.Subscribe(maxDeliveriesExceededSubject, tc.handleDeadlineExceeded)
	if err != nil {
		logger.Error("Failed to subscribe to max deliveries exceeded subject", err)
		return
	}

	tc.subscription = sub
}

// handleDeadlineExceeded processes deadline exceeded messages
func (tc *timeOutConsumer) handleDeadlineExceeded(msg *nats.Msg) {
	var advisory struct {
		Stream    string `json:"stream"`
		StreamSeq uint64 `json:"stream_seq"`
	}

	if err := json.Unmarshal(msg.Data, &advisory); err != nil {
		logger.Error("Failed to unmarshal advisory message", err)
		return
	}

	logger.Info("Received max deliveries exceeded advisory",
		"stream", advisory.Stream,
		"stream_seq", advisory.StreamSeq)

	// Get the handler for this stream type
	handler, exists := tc.handlers[advisory.Stream]
	if !exists {
		logger.Info("No handler found for stream", "stream", advisory.Stream)
		return
	}

	// Retrieve the failed message
	js, err := tc.natsConn.JetStream()
	if err != nil {
		logger.Error("Failed to get JetStream context", err)
		return
	}

	failedMsg, err := js.GetMsg(advisory.Stream, advisory.StreamSeq)
	if err != nil {
		logger.Error("Failed to retrieve failed message", err,
			"stream", advisory.Stream,
			"stream_seq", advisory.StreamSeq,
		)
		return
	}

	// Handle the timeout using the appropriate handler
	idempotentKey, err := handler.HandleTimeout(advisory.Stream, advisory.StreamSeq, failedMsg.Data)
	if err != nil {
		logger.Error(
			"Failed to handle timeout", err,
			"stream", advisory.Stream,
			"stream_seq", advisory.StreamSeq,
		)
		return
	}

	logger.Info(
		"Successfully handled timeout",
		"stream", advisory.Stream,
		"stream_seq", advisory.StreamSeq,
		"idempotent_key", idempotentKey,
	)
}

// Close stops the timeout consumer
func (tc *timeOutConsumer) Close() error {
	if tc.subscription != nil {
		if err := tc.subscription.Unsubscribe(); err != nil {
			logger.Error("Failed to unsubscribe from max deliveries exceeded subject", err)
			return err
		}
	}
	logger.Info("Unsubscribed from max deliveries exceeded subject")
	return nil
}
