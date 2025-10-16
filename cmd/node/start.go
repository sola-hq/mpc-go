package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/event"
	"github.com/fystack/mpcium/pkg/eventconsumer"
	"github.com/fystack/mpcium/pkg/identity"
	"github.com/fystack/mpcium/pkg/infra"
	"github.com/fystack/mpcium/pkg/keyinfo"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/mpc"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

	viper.SetDefault("backup_enabled", true)
	config.InitViperConfig(configPath)

	appConfig := config.LoadConfig()
	environment := appConfig.Environment
	logger.Init(environment, debug)

	// Handle password file if provided
	if passwordFile != "" {
		if err := loadPasswordFromFile(passwordFile); err != nil {
			return fmt.Errorf("failed to load password from file: %w", err)
		}
	}
	// Handle configuration based on prompt flag
	if usePrompts {
		promptForSensitiveCredentials()
	} else {
		// Validate the config values
		checkRequiredConfigValues(appConfig)
	}

	consulClient := infra.GetConsulClient(environment)
	keyinfoStore := keyinfo.NewStore(consulClient.KV())
	peers := LoadPeersFromConsul(consulClient)
	nodeID := GetIDFromName(nodeName, peers)

	badgerKV := NewBadgerKV(nodeName, nodeID, appConfig)
	defer badgerKV.Close()

	// Start background backup job
	backupEnabled := viper.GetBool("backup_enabled")
	if backupEnabled {
		backupPeriodSeconds := viper.GetInt("backup_period_seconds")
		stopBackup := StartPeriodicBackup(ctx, badgerKV, backupPeriodSeconds)
		defer stopBackup()
	}

	identityStore, err := identity.NewFileStore("identity", nodeName, decryptPrivateKey, agePasswordFile)
	if err != nil {
		logger.Fatal("Failed to create identity store", err)
	}

	natsConn, err := messaging.GetNATSConnection(environment, appConfig.NATs)
	if err != nil {
		logger.Fatal("Failed to connect to NATS", err)
	}

	pubsub := messaging.NewNATSPubSub(natsConn)
	keygenBroker, err := messaging.NewJetStreamBroker(ctx, natsConn, event.KeygenBrokerStream, []string{
		event.KeygenRequestTopic,
	})
	if err != nil {
		logger.Fatal("Failed to create keygen jetstream broker", err)
	}
	signingBroker, err := messaging.NewJetStreamBroker(ctx, natsConn, event.SigningBrokerStream, []string{
		event.SigningRequestTopic,
	})
	if err != nil {
		logger.Fatal("Failed to create signing jetstream broker", err)
	}

	directMessaging := messaging.NewNatsDirectMessaging(natsConn)
	mqManager := messaging.NewNATsMessageQueueManager("mpc", []string{
		event.KeygenResultTopic,
		event.SigningResultTopic,
		event.ResharingResultTopic,
	}, natsConn)

	genKeyResultQueue := mqManager.NewMessageQueue(event.KeygenResultQueueName)
	defer genKeyResultQueue.Close()
	singingResultQueue := mqManager.NewMessageQueue(event.SigningResultQueueName)
	defer singingResultQueue.Close()
	resharingResultQueue := mqManager.NewMessageQueue(event.ResharingResultQueueName)
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
		singingResultQueue,
		resharingResultQueue,
		identityStore,
	)
	eventConsumer.Run()
	defer eventConsumer.Close()

	timeoutConsumer := eventconsumer.NewTimeOutConsumer(
		natsConn,
		singingResultQueue,
	)

	timeoutConsumer.Run()
	defer timeoutConsumer.Close()
	keygenConsumer := eventconsumer.NewKeygenConsumer(natsConn, keygenBroker, pubsub, peerRegistry, genKeyResultQueue)
	signingConsumer := eventconsumer.NewSigningConsumer(natsConn, signingBroker, pubsub, peerRegistry, singingResultQueue)

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
