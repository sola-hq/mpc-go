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

	// 2) Once wallet exists, immediately fire a SignTransaction
	txID := uuid.New().String()
	dummyTx := []byte("deadbeef") // replace with real transaction bytes
	signingDone := make(chan bool, 1)

	// Record signing start time
	startTime := time.Now()

	txMsg := &types.SigningMessage{
		KeyType:  types.KeyTypeEd25519,
		WalletID: "a3c1ee50-ff6e-455c-a8e2-37456c8143f7", // Use the generated wallet ID
		TxID:     txID,
		Tx:       dummyTx,
	}
	err = mpcClient.SignTransaction(txMsg)
	if err != nil {
		logger.Fatal("SignTransaction failed", err)
	}
	fmt.Printf("SignTransaction(%q) sent, awaiting result...\n", txID)

	// 3) Listen for signing results
	err = mpcClient.OnSignResult(func(response types.SigningResponse) {
		// Calculate signing duration
		duration := time.Since(startTime)

		logger.Info("Signing result received",
			"txID", response.TxID,
			"errcode", response.ErrorCode,
			"errreason", response.ErrorReason,
			"signature", fmt.Sprintf("%x", response.Signature),
			"duration", duration.String(),
			"duration(ms)", duration.Milliseconds(),
		)
		// Notify main program to exit
		signingDone <- true
	})
	if err != nil {
		logger.Fatal("Failed to subscribe to OnSignResult", err)
	}

	// Wait for syscall signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-signingDone:
		// Calculate total duration
		totalDuration := time.Since(startTime)
		fmt.Printf("Signing completed cost %s (%dms)",
			totalDuration.String(), totalDuration.Milliseconds())
	case <-stop:
		fmt.Println("Received interrupt signal. Shutting down.")
	}
}
