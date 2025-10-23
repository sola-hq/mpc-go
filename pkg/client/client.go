package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fystack/mpcium/pkg/client/signer"
	"github.com/fystack/mpcium/pkg/constant"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/types"
	"github.com/nats-io/nats.go"
)

type initiator struct {
	signingBroker   messaging.MessageBroker
	keygenBroker    messaging.MessageBroker
	resharingBroker messaging.MessageBroker

	keygenResultQueue    messaging.MessageQueue
	signingResultQueue   messaging.MessageQueue
	resharingResultQueue messaging.MessageQueue
	signer               signer.Signer
}

// Options defines configuration options for creating a new MPCClient
type Options struct {
	// NATS connection
	NatsConn *nats.Conn

	// Signer for signing messages
	Signer signer.Signer
}

// NewMPCClient creates a new MPC client using the provided options.
// The signer must be provided to handle message signing.
func NewMPCClient(opts Options) types.Initiator {
	// 1) Validate the options
	if opts.Signer == nil {
		logger.Fatal("Signer is required", nil)
	}

	// 2) Create the PubSub for both publish & subscribe
	signingBroker, err := messaging.NewJetStreamBroker(
		context.Background(),
		opts.NatsConn,
		constant.SigningBrokerStream,
		[]string{constant.SigningRequestTopic},
	)
	if err != nil {
		logger.Fatal("Failed to create signing jetstream broker", err)
	}

	keygenBroker, err := messaging.NewJetStreamBroker(
		context.Background(),
		opts.NatsConn,
		constant.KeygenBrokerStream,
		[]string{constant.KeygenRequestTopic},
	)
	if err != nil {
		logger.Fatal("Failed to create keygen jetstream broker", err)
	}

	resharingBroker, err := messaging.NewJetStreamBroker(
		context.Background(),
		opts.NatsConn,
		constant.ResharingBrokerStream,
		[]string{constant.ResharingRequestTopic},
	)
	if err != nil {
		logger.Fatal("Failed to create resharing jetstream broker", err)
	}

	subjects := []string{
		constant.KeygenResultTopic,
		constant.SigningResultTopic,
		constant.ResharingResultTopic,
	}

	messageQueueMgr := messaging.NewNATsMessageQueueManager(constant.StreamName, subjects, opts.NatsConn)

	keygenResultQueue := messageQueueMgr.NewMessageQueue(constant.KeygenResultQueueName)
	signingResultQueue := messageQueueMgr.NewMessageQueue(constant.SigningResultQueueName)
	resharingResultQueue := messageQueueMgr.NewMessageQueue(constant.ResharingResultQueueName)

	return &initiator{
		signingBroker:   signingBroker,
		keygenBroker:    keygenBroker,
		resharingBroker: resharingBroker,

		keygenResultQueue:    keygenResultQueue,
		signingResultQueue:   signingResultQueue,
		resharingResultQueue: resharingResultQueue,

		signer: opts.Signer,
	}
}

// CreateWallet generates a GenerateKeyMessage, signs it, and publishes it.
func (c *initiator) CreateWallet(walletID string) error {
	// build the message
	msg := &types.KeygenMessage{
		WalletID: walletID,
	}
	// compute the canonical raw bytes
	raw, err := msg.Raw()
	if err != nil {
		return fmt.Errorf("CreateWallet: raw payload error: %w", err)
	}
	signature, err := c.signer.Sign(raw)
	if err != nil {
		return fmt.Errorf("CreateWallet: failed to sign message: %w", err)
	}
	msg.Signature = signature

	bytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("CreateWallet: marshal error: %w", err)
	}

	topic := constant.FormatKeygenRequestTopic(walletID)
	if err := c.keygenBroker.PublishMessage(context.Background(), topic, bytes); err != nil {
		return fmt.Errorf("CreateWallet: publish error: %w", err)
	}
	return nil
}

// The callback will be invoked whenever a wallet creation result is received.
func (c *initiator) OnWalletCreationResult(callback func(event types.KeygenResponse)) error {
	err := c.keygenResultQueue.Dequeue(constant.KeygenResultTopic, func(msg []byte) error {
		var event types.KeygenResponse
		err := json.Unmarshal(msg, &event)
		if err != nil {
			return err
		}
		callback(event)
		return nil
	})

	if err != nil {
		return fmt.Errorf("OnWalletCreationResult: subscribe error: %w", err)
	}

	return nil
}

// SignTransaction builds a SigningMessage, signs it, and publishes it.
func (c *initiator) SignTransaction(msg *types.SigningMessage) error {
	// compute the canonical raw bytes (omitting Signature field)
	raw, err := msg.Raw()
	if err != nil {
		return fmt.Errorf("SignTransaction: raw payload error: %w", err)
	}
	signature, err := c.signer.Sign(raw)
	if err != nil {
		return fmt.Errorf("SignTransaction: failed to sign message: %w", err)
	}
	msg.Signature = signature

	bytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("SignTransaction: marshal error: %w", err)
	}

	topic := constant.FormatSigningRequestTopic(msg.TxID)
	if err := c.signingBroker.PublishMessage(context.Background(), topic, bytes); err != nil {
		return fmt.Errorf("SignTransaction: publish error: %w", err)
	}
	return nil
}

func (c *initiator) OnSignResult(callback func(event types.SigningResponse)) error {
	err := c.signingResultQueue.Dequeue(constant.SigningResultCompleteTopic, func(msg []byte) error {
		var event types.SigningResponse
		err := json.Unmarshal(msg, &event)
		if err != nil {
			return err
		}
		callback(event)
		return nil
	})

	if err != nil {
		return fmt.Errorf("OnSignResult: subscribe error: %w", err)
	}

	return nil
}

func (c *initiator) Resharing(msg *types.ResharingMessage) error {
	// compute the canonical raw bytes
	raw, err := msg.Raw()
	if err != nil {
		return fmt.Errorf("Resharing: raw payload error: %w", err)
	}
	signature, err := c.signer.Sign(raw)
	if err != nil {
		return fmt.Errorf("Resharing: failed to sign message: %w", err)
	}
	msg.Signature = signature

	bytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("Resharing: marshal error: %w", err)
	}

	topic := constant.FormatResharingRequestTopic(msg.WalletID)
	if err := c.resharingBroker.PublishMessage(context.Background(), topic, bytes); err != nil {
		return fmt.Errorf("Resharing: publish error: %w", err)
	}
	return nil
}

func (c *initiator) OnResharingResult(callback func(event types.ResharingResponse)) error {

	err := c.resharingResultQueue.Dequeue(constant.ResharingResultTopic, func(msg []byte) error {
		logger.Info("Received resharing success message", "raw", string(msg))
		var event types.ResharingResponse
		err := json.Unmarshal(msg, &event)
		if err != nil {
			logger.Error("Failed to unmarshal resharing success event", err, "raw", string(msg))
			return err
		}
		logger.Info("Deserialized resharing success event", "event", event)
		callback(event)
		return nil
	})

	if err != nil {
		return fmt.Errorf("OnResharingResult: subscribe error: %w", err)
	}

	return nil
}
