package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/constant"
	"github.com/fystack/mpcium/pkg/eventconsumer"
	"github.com/fystack/mpcium/pkg/identity"
	"github.com/fystack/mpcium/pkg/infra"
	"github.com/fystack/mpcium/pkg/keyinfo"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/mpc"
	"github.com/spf13/cobra"
)

// NewStartCmd creates a new start command
func NewStartCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "start",
		Short: "Start an MPC node",
		Long:  "Start an MPC node with the specified configuration",
		RunE:  runNode,
	}

	cmd.Flags().StringP("name", "n", "", "Node name (required)")
	cmd.Flags().StringP("config", "c", "", "Path to configuration file")
	cmd.Flags().BoolP("decrypt-private-key", "d", false, "Decrypt node private key")
	cmd.Flags().BoolP("prompt-credentials", "p", false, "Prompt for sensitive parameters")
	cmd.Flags().StringP("password-file", "f", "", "Path to file containing BadgerDB password")
	cmd.Flags().StringP("identity-password-file", "k", "", "Path to file containing password for decrypting .age encrypted node private key")
	cmd.Flags().Bool("debug", false, "Enable debug logging")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runNode(cmd *cobra.Command, args []string) error {
	nodeName, _ := cmd.Flags().GetString("name")
	configPath, _ := cmd.Flags().GetString("config")
	decryptPrivateKey, _ := cmd.Flags().GetBool("decrypt-private-key")
	usePrompts, _ := cmd.Flags().GetBool("prompt-credentials")
	passwordFile, _ := cmd.Flags().GetString("password-file")
	agePasswordFile, _ := cmd.Flags().GetString("identity-password-file")
	debug, _ := cmd.Flags().GetBool("debug")

	ctx := context.Background()

	if configPath != "" {
		config.SetEnvConfigPath(configPath)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger.Init(cfg.Environment, debug)

	// Handle password file if provided
	if passwordFile != "" {
		if err := loadPasswordFromFile(cfg, passwordFile); err != nil {
			return fmt.Errorf("failed to load password from file: %w", err)
		}
	}
	// Handle configuration based on prompt flag
	if usePrompts {
		promptForSensitiveCredentials(cfg)
	} else {
		// Validate the config values
		checkRequiredConfigValues(cfg)
	}

	consulClient := infra.GetConsulClient(cfg.Environment)
	keyinfoStore := keyinfo.NewStore(consulClient.KV())
	peers := LoadPeersFromConsul(consulClient)
	nodeID := GetIDFromName(nodeName, peers)

	badgerKV := NewBadgerKV(nodeName, nodeID)
	defer badgerKV.Close()

	// Start background backup job
	backupEnabled := cfg.BackupEnabled
	if backupEnabled {
		backupPeriodSeconds := cfg.BackupPeriodSeconds
		stopBackup := StartPeriodicBackup(ctx, badgerKV, backupPeriodSeconds)
		defer stopBackup()
	}

	identityStore, err := identity.NewFileStore("identity", nodeName, decryptPrivateKey, agePasswordFile)
	if err != nil {
		logger.Fatal("Failed to create identity store", err)
	}

	natsConn, err := messaging.GetNATSConnection()
	if err != nil {
		logger.Fatal("Failed to connect to NATS", err)
	}

	pubsub := messaging.NewNATSPubSub(natsConn)
	keygenBroker, err := messaging.NewJetStreamBroker(ctx, natsConn, constant.KeygenBrokerStream, []string{
		constant.KeygenRequestTopic,
	})
	if err != nil {
		logger.Fatal("Failed to create keygen jetstream broker", err)
	}

	signingBroker, err := messaging.NewJetStreamBroker(ctx, natsConn, constant.SigningBrokerStream, []string{
		constant.SigningRequestTopic,
	})
	if err != nil {
		logger.Fatal("Failed to create signing jetstream broker", err)
	}

	resharingBroker, err := messaging.NewJetStreamBroker(ctx, natsConn, constant.ResharingBrokerStream, []string{
		constant.ResharingRequestTopic,
	})
	if err != nil {
		logger.Fatal("Failed to create resharing jetstream broker", err)
	}

	subjects := []string{
		constant.KeygenResultTopic,
		constant.SigningResultTopic,
		constant.ResharingResultTopic,
	}

	directMessaging := messaging.NewNatsDirectMessaging(natsConn)
	messageQueueMgr := messaging.NewNATsMessageQueueManager(
		constant.StreamName,
		subjects,
		natsConn,
	)

	genKeyResultQueue := messageQueueMgr.NewMessageQueue(constant.KeygenResultQueueName)
	defer genKeyResultQueue.Close()

	signingResultQueue := messageQueueMgr.NewMessageQueue(constant.SigningResultQueueName)
	defer signingResultQueue.Close()

	resharingResultQueue := messageQueueMgr.NewMessageQueue(constant.ResharingResultQueueName)
	defer resharingResultQueue.Close()

	logger.Info("Node is running", "ID", nodeID, "name", nodeName)

	peerNodeIDs := GetPeerIDs(peers)
	peerRegistry := mpc.NewRegistry(nodeID, peerNodeIDs, consulClient.KV(), directMessaging, pubsub, identityStore)

	mpcNode := mpc.NewNode(
		nodeID,
		peerNodeIDs,
		pubsub,
		directMessaging,
		badgerKV,
		keyinfoStore,
		peerRegistry,
		identityStore,
	)
	defer mpcNode.Close()

	eventConsumer := eventconsumer.NewEventConsumer(
		mpcNode,
		pubsub,
		genKeyResultQueue,
		signingResultQueue,
		resharingResultQueue,
		identityStore,
	)
	eventConsumer.Run()
	defer eventConsumer.Close()

	timeoutConsumer := eventconsumer.NewTimeOutConsumer(
		natsConn,
		signingResultQueue,
	)

	timeoutConsumer.Run()
	defer timeoutConsumer.Close()

	keygenConsumer := eventconsumer.NewKeygenConsumer(natsConn, keygenBroker, pubsub, peerRegistry, genKeyResultQueue)
	signingConsumer := eventconsumer.NewSigningConsumer(natsConn, signingBroker, pubsub, peerRegistry, signingResultQueue)
	resharingConsumer := eventconsumer.NewResharingConsumer(natsConn, resharingBroker, pubsub, peerRegistry, resharingResultQueue)

	// Make the node ready before starting the signing consumer
	if err := peerRegistry.Ready(); err != nil {
		logger.Error("Failed to mark peer registry as ready", err)
	}
	logger.Info("[READY] Node is ready", "nodeID", nodeID)

	logger.Info("Starting consumers", "nodeID", nodeID)
	appContext, cancel := context.WithCancel(context.Background())
	//Setup signal handling to cancel context on termination signals.
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		logger.Warn("Shutdown signal received, canceling context...")
		cancel()

		// Resign from peer registry first (before closing NATS)
		if err := peerRegistry.Resign(); err != nil {
			logger.Error("Failed to resign from peer registry", err)
		}

		// Gracefully close consumers
		if err := keygenConsumer.Close(); err != nil {
			logger.Error("Failed to close keygen consumer", err)
		}
		if err := signingConsumer.Close(); err != nil {
			logger.Error("Failed to close signing consumer", err)
		}
		if err := resharingConsumer.Close(); err != nil {
			logger.Error("Failed to close resharing consumer", err)
		}

		err := natsConn.Drain()
		if err != nil {
			logger.Error("Failed to drain NATS connection", err)
		}
	}()

	var wg sync.WaitGroup
	errChan := make(chan error, 3)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := keygenConsumer.Run(appContext); err != nil {
			logger.Error("error running keygen consumer", err)
			errChan <- fmt.Errorf("keygen consumer error: %w", err)
			return
		}
		logger.Info("Keygen consumer finished successfully")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := signingConsumer.Run(appContext); err != nil {
			logger.Error("error running signing consumer", err)
			errChan <- fmt.Errorf("signing consumer error: %w", err)
			return
		}
		logger.Info("Signing consumer finished successfully")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := resharingConsumer.Run(appContext); err != nil {
			logger.Error("error running resharing consumer", err)
			errChan <- fmt.Errorf("resharing consumer error: %w", err)
			return
		}
		logger.Info("Resharing consumer finished successfully")
	}()

	go func() {
		wg.Wait()
		logger.Info("All consumers have finished")
		close(errChan)
	}()

	for err := range errChan {
		if err != nil {
			logger.Error("Consumer error received", err)
			cancel()
			return err
		}
	}

	return nil
}
