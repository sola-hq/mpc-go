package main

import (
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/fystack/mpcium/pkg/client"
	"github.com/fystack/mpcium/pkg/client/signer"
	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/types"
	"github.com/google/uuid"
	"github.com/spf13/viper"
)

func main() {
	const environment = "dev"
	config.InitViperConfig("")
	logger.Init(environment, true)

	algorithm := viper.GetString("event_initiator_algorithm")
	if algorithm == "" {
		algorithm = string(types.EventInitiatorKeyTypeEd25519)
	}

	// Validate algorithm
	if !slices.Contains(
		[]string{
			string(types.EventInitiatorKeyTypeEd25519),
			string(types.EventInitiatorKeyTypeP256),
		},
		algorithm,
	) {
		logger.Fatal(
			fmt.Sprintf(
				"invalid algorithm: %s. Must be %s or %s",
				algorithm,
				types.EventInitiatorKeyTypeEd25519,
				types.EventInitiatorKeyTypeP256,
			),
			nil,
		)
	}
	appConfig := config.LoadConfig()
	natsConn, err := messaging.GetNATSConnection(environment, appConfig.NATs)
	if err != nil {
		logger.Fatal("Failed to connect to NATS", err)
	}
	defer natsConn.Drain()
	defer natsConn.Close()

	// Record resharing start time
	startTime := time.Now()

	localSigner, err := signer.NewLocalSigner(types.EventInitiatorKeyType(algorithm), signer.LocalSignerOptions{
		KeyPath: "./event_initiator.key",
	})
	if err != nil {
		logger.Fatal("Failed to create local signer", err)
	}

	mpcClient := client.NewMPCClient(client.Options{
		NatsConn: natsConn,
		Signer:   localSigner,
	})
	resharingDone := make(chan bool, 1)

	// 3) Listen for signing results
	err = mpcClient.OnResharingResult(func(result types.ResharingResponse) {
		resharingDone <- true
		logger.Info("Resharing result received",
			"walletID", result.WalletID,
			"pubKey", fmt.Sprintf("%x", result.PubKey),
			"newThreshold", result.NewThreshold,
			"keyType", result.KeyType,
		)
	})
	if err != nil {
		logger.Fatal("Failed to subscribe to OnResharingResult", err)
	}
	walletID := "81c8a932-e618-488f-acd8-5b5219df574d"

	resharingMsg := &types.ResharingMessage{
		SessionID: uuid.NewString(),
		WalletID:  walletID,
		NodeIDs: []string{
			"FD97D371-4C75-4F59-9159-759F4C71543C",
			"504ACBA3-0958-4E10-B6E6-6B274579BF6A",
		}, // new peer IDs

		NewThreshold: 1, // t+1 <= len(NodeIDs)
		KeyType:      types.KeyTypeEd25519,
	}
	err = mpcClient.Resharing(resharingMsg)
	if err != nil {
		logger.Fatal("Resharing failed", err)
	}
	fmt.Printf("Resharing(%q) sent, awaiting result...\n", resharingMsg.WalletID)

	// Wait for syscall signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// Wait for resharing to complete or receive interrupt signal
	select {
	case <-resharingDone:
		// Calculate total duration
		totalDuration := time.Since(startTime)
		fmt.Printf("Resharing(%q) completed cost %s (%dms)", walletID,
			totalDuration.String(), totalDuration.Milliseconds())
	case <-stop:
		fmt.Println("Received interrupt signal. Shutting down.")
	}
}
