package eventconsumer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fystack/mpcium/pkg/constant"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/mpc"
	"github.com/fystack/mpcium/pkg/mpc/core"
	"github.com/fystack/mpcium/pkg/types"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/spf13/viper"
)

const (
	resharingResponseTimeout = 60 * time.Second
	resharingPollingInterval = 500 * time.Millisecond
)

// ResharingConsumer represents a consumer that processes resharing events.
type ResharingConsumer interface {
	// Run starts the consumer and blocks until the provided context is canceled.
	Run(ctx context.Context) error
	Close() error
}

// resharingConsumer implements ResharingConsumer.
type resharingConsumer struct {
	natsConn             *nats.Conn
	pubsub               messaging.PubSub
	jsBroker             messaging.MessageBroker
	peerRegistry         mpc.PeerRegistry
	mpcThreshold         int
	resharingResultQueue messaging.MessageQueue

	jsSub messaging.Subscription
}

// NewResharingConsumer returns a new instance of ResharingConsumer.
func NewResharingConsumer(natsConn *nats.Conn, jsBroker messaging.MessageBroker, pubsub messaging.PubSub, peerRegistry mpc.PeerRegistry, resharingResultQueue messaging.MessageQueue) ResharingConsumer {
	mpcThreshold := viper.GetInt("mpc_threshold")
	return &resharingConsumer{
		natsConn:             natsConn,
		pubsub:               pubsub,
		jsBroker:             jsBroker,
		peerRegistry:         peerRegistry,
		mpcThreshold:         mpcThreshold,
		resharingResultQueue: resharingResultQueue,
	}
}

func (sc *resharingConsumer) waitForSufficientPeers(ctx context.Context) error {
	requiredPeers := int64(sc.mpcThreshold + 1)

	logger.Info("ResharingConsumer: Waiting for sufficient peers before consuming messages",
		"required", requiredPeers,
		"threshold", sc.mpcThreshold)

	ticker := time.NewTicker(readinessCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return nil
			}
			return ctx.Err()
		case <-ticker.C:
			readyPeers := sc.peerRegistry.GetReadyPeersCount()
			if readyPeers >= requiredPeers {
				logger.Info("ResharingConsumer: Sufficient peers ready, starting message consumption",
					"ready", readyPeers,
					"t+1", requiredPeers)
				return nil
			}
			logger.Info("ResharingConsumer: Waiting for more peers to be ready",
				"ready", readyPeers,
				"t+1", requiredPeers)
		}
	}
}

// Run subscribes to resharing events and processes them until the context is canceled.
func (sc *resharingConsumer) Run(ctx context.Context) error {
	if err := sc.waitForSufficientPeers(ctx); err != nil {
		if err == context.Canceled {
			return nil
		}
		return fmt.Errorf("failed to wait for sufficient peers: %w", err)
	}

	sub, err := sc.jsBroker.CreateSubscription(
		ctx,
		constant.ResharingConsumerStream,
		constant.ResharingRequestTopic,
		sc.handleResharingEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe to resharing events: %w", err)
	}
	sc.jsSub = sub
	logger.Info("ResharingConsumer: Subscribed to resharing events")

	<-ctx.Done()
	logger.Info("ResharingConsumer: Context cancelled, shutting down")

	return sc.Close()
}

func (sc *resharingConsumer) handleResharingEvent(msg jetstream.Msg) {
	raw := msg.Data()
	var resharingMsg types.ResharingMessage
	sessionID := msg.Headers().Get("SessionID")

	err := json.Unmarshal(raw, &resharingMsg)
	if err != nil {
		logger.Error("ResharingConsumer: Failed to unmarshal resharing message", err)
		sc.handleError(resharingMsg, types.ErrorCodeUnmarshalFailure, err, sessionID)
		_ = msg.Ack()
		return
	}

	if !sc.peerRegistry.AreMajorityReady() {
		requiredPeers := int64(sc.mpcThreshold + 1)
		err := fmt.Errorf("not enough peers to process resharing request: ready=%d, required=%d", sc.peerRegistry.GetReadyPeersCount(), requiredPeers)
		sc.handleError(resharingMsg, types.ErrorCodeNotMajority, err, sessionID)
		_ = msg.Ack()
		return
	}

	replyInbox := nats.NewInbox()
	replySub, err := sc.natsConn.SubscribeSync(replyInbox)
	if err != nil {
		logger.Error("ResharingConsumer: Failed to subscribe to reply inbox", err)
		_ = msg.Nak()
		return
	}
	defer func() {
		if err := replySub.Unsubscribe(); err != nil {
			logger.Warn("ResharingConsumer: Failed to unsubscribe from reply inbox", "error", err)
		}
	}()

	headers := map[string]string{
		"SessionID": uuid.New().String(),
	}
	if err := sc.pubsub.PublishWithReply(MPCResharingEvent, replyInbox, msg.Data(), headers); err != nil {
		logger.Error("ResharingConsumer: Failed to publish resharing event with reply", err)
		_ = msg.Nak()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), resharingResponseTimeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			logger.Warn("ResharingConsumer: Timeout waiting for resharing event response")
			_ = msg.Nak()
			return
		default:
			replyMsg, err := replySub.NextMsg(resharingPollingInterval)
			if err != nil {
				if err == nats.ErrTimeout {
					continue
				}
				logger.Error("ResharingConsumer: Error receiving reply message", err)
				_ = msg.Nak()
				return
			}
			if replyMsg != nil {
				logger.Info("ResharingConsumer: Completed resharing event; reply received")
				if ackErr := msg.Ack(); ackErr != nil {
					logger.Error("ResharingConsumer: ACK failed", ackErr)
				}
				return
			}
		}
	}
}

func (sc *resharingConsumer) handleError(msg types.ResharingMessage, errorCode types.ErrorCode, err error, sessionID string) {
	resharingResult := types.ResharingResponse{
		ErrorCode:    errorCode,
		WalletID:     msg.WalletID,
		KeyType:      msg.KeyType,
		NewThreshold: msg.NewThreshold,
		ErrorReason:  err.Error(),
	}

	resharingResultBytes, err := json.Marshal(resharingResult)
	if err != nil {
		logger.Error("Failed to marshal resharing result event", err, "walletID", msg.WalletID)
		return
	}

	err = sc.resharingResultQueue.Enqueue(constant.ResharingResultCompleteTopic, resharingResultBytes, &messaging.EnqueueOptions{
		IdempotentKey: buildIdempotentKey(msg.WalletID, sessionID, core.TypeResharingWalletResultFmt),
	})
	if err != nil {
		logger.Error("Failed to enqueue resharing result event", err, "walletID", msg.WalletID)
	}
}

func (sc *resharingConsumer) Close() error {
	if sc.jsSub != nil {
		if err := sc.jsSub.Unsubscribe(); err != nil {
			logger.Error("ResharingConsumer: Failed to unsubscribe from JetStream", err)
			return err
		}
		logger.Info("ResharingConsumer: Unsubscribed from JetStream")
	}
	return nil
}
