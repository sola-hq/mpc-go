package messaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fystack/mpcium/pkg/logger"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var (
	ErrPermanent = errors.New("permanent messaging error")
)

type MessageQueue interface {
	Enqueue(topic string, message []byte, options *EnqueueOptions) error
	Dequeue(topic string, handler func(message []byte) error) error
	Close()
}

type EnqueueOptions struct {
	IdempotentKey string
}

type messageQueue struct {
	consumerName string
	js           jetstream.JetStream
	consumer     jetstream.Consumer
	context      jetstream.ConsumeContext
}

type NATsMessageQueueManager struct {
	queueName string
	js        jetstream.JetStream
}

func NewNATsMessageQueueManager(queueName string, subjectWildCards []string, nc *nats.Conn) *NATsMessageQueueManager {
	js, err := jetstream.New(nc)
	if err != nil {
		logger.Fatal("Error creating JetStream context: ", err)
	}

	ctx := context.Background()
	stream, err := js.Stream(ctx, queueName)
	if err != nil {
		logger.Warn("Stream not found, creating new stream", "stream", queueName)
	}
	if stream != nil {
		info, _ := stream.Info(ctx)
		logger.Debug("Stream found", "info", info)

	}

	_, err = js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:        queueName,
		Description: "Stream for " + queueName,
		Subjects:    subjectWildCards,
		MaxBytes:    10_485_760, // Light Production (Low Traffic) (10 MB)
		Storage:     jetstream.FileStorage,
		Retention:   jetstream.WorkQueuePolicy,
	})
	if err != nil {
		logger.Fatal("Error creating JetStream stream: ", err)
	}
	logger.Info("Creating apex NATs Jetstream context successfully!", "streamName", queueName, "subjects", subjectWildCards)

	return &NATsMessageQueueManager{
		queueName: queueName,
		js:        js,
	}
}

func (m *NATsMessageQueueManager) NewMessageQueue(consumerName string) MessageQueue {
	mq := &messageQueue{
		consumerName: consumerName,
		js:           m.js,
	}
	consumerWildCard := fmt.Sprintf("%s.%s.*", m.queueName, consumerName)
	cfg := jetstream.ConsumerConfig{
		Name:          consumerName,
		Durable:       consumerName,
		MaxAckPending: 1000,
		// If a message isn't acknowledged within AckWait, it will be redelivered up to MaxDelive
		AckWait:   30 * time.Second,
		AckPolicy: jetstream.AckExplicitPolicy,
		FilterSubjects: []string{
			consumerWildCard,
		},
		MaxDeliver: 3,
	}
	logger.Info("Creating consumer for subject", "consumerName", consumerName, "queueName", m.queueName, "filterSubject", consumerWildCard, "config", cfg)
	consumer, err := m.js.CreateOrUpdateConsumer(context.Background(), m.queueName, cfg)
	if err != nil {
		logger.Fatal("Error creating JetStream consumer: ", err)
	}

	mq.consumer = consumer
	return mq
}

func (mq *messageQueue) Enqueue(topic string, message []byte, options *EnqueueOptions) error {
	header := nats.Header{}
	if options != nil {
		header.Add("Nats-Msg-Id", options.IdempotentKey)
	}

	logger.Info("Publishing message", "topic", topic, "consumerName", mq.consumerName)
	_, err := mq.js.PublishMsg(context.Background(), &nats.Msg{
		Subject: topic,
		Data:    message,
		Header:  header,
	})

	if err != nil {
		logger.Error("Failed to publish message to JetStream", err, "topic", topic, "consumerName", mq.consumerName)
		return fmt.Errorf("error enqueueing message: %w", err)
	}

	return nil
}

func (mq *messageQueue) Dequeue(topic string, handler func(message []byte) error) error {
	c, err := mq.consumer.Consume(func(msg jetstream.Msg) {
		meta, _ := msg.Metadata()
		logger.Debug("Received message", "meta", meta)
		err := handler(msg.Data())
		if err != nil {
			if errors.Is(err, ErrPermanent) {
				logger.Info("Permanent error on message", "meta", meta)
				if err := msg.Term(); err != nil {
					logger.Error("Failed to terminate message", err)
				}
				return
			}

			logger.Error("Error handling message: ", err)
			if err := msg.Nak(); err != nil {
				logger.Error("Failed to nak message", err)
			}
			return
		}

		logger.Debug("Message Acknowledged", "meta", meta)
		err = msg.Ack()
		if err != nil {
			logger.Error("Error acknowledging message: ", err)
		}
	})
	mq.context = c
	return err
}

func (mq *messageQueue) Close() {
	// only close consumer if it was created - dequeue
	if mq.context != nil {
		mq.context.Stop()
	}
}
