package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"sync"
	"sync/atomic"
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
	const environment = "development"
	numWallets := flag.Int("n", 1, "Number of wallets to generate")

	flag.Parse()

	config.InitViperConfig("")
	logger.Init(environment, false)

	algorithm := viper.GetString("event_initiator_algorithm")
	if algorithm == "" {
		algorithm = string(types.EventInitiatorKeyTypeEd25519)
	}

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

	var walletStartTimes sync.Map
	var walletIDs []string
	var walletIDsMutex sync.Mutex
	var wg sync.WaitGroup
	var completedCount int32

	startAll := time.Now()

	// STEP 1: Pre-generate wallet IDs and store start times
	for i := 0; i < *numWallets; i++ {
		walletID := uuid.New().String()
		walletStartTimes.Store(walletID, time.Now())

		walletIDsMutex.Lock()
		walletIDs = append(walletIDs, walletID)
		walletIDsMutex.Unlock()
	}

	// STEP 2: Register the result handler AFTER all walletIDs are stored
	err = mpcClient.OnWalletCreationResult(func(response types.KeygenResponse) {
		logger.Info("Received wallet creation result", "response", response)
		now := time.Now()
		startTimeAny, ok := walletStartTimes.Load(response.WalletID)
		if ok {
			startTime := startTimeAny.(time.Time)
			duration := now.Sub(startTime).Seconds()
			accumulated := now.Sub(startAll).Seconds()
			countSoFar := atomic.AddInt32(&completedCount, 1)

			logger.Info("Wallet created",
				"walletID", response.WalletID,
				"duration_seconds", fmt.Sprintf("%.3f", duration),
				"accumulated_time_seconds", fmt.Sprintf("%.3f", accumulated),
				"count_so_far", countSoFar,
			)

			walletStartTimes.Delete(response.WalletID)
		} else {
			logger.Warn("Received wallet result but no start time found", "walletID", response.WalletID)
		}
		wg.Done()
	})
	if err != nil {
		logger.Fatal("Failed to subscribe to wallet-creation results", err)
	}

	// STEP 3: Create wallets
	for _, walletID := range walletIDs {
		wg.Add(1) // Add to WaitGroup BEFORE attempting to create wallet

		if err := mpcClient.CreateWallet(walletID); err != nil {
			logger.Error("CreateWallet failed", err)
			walletStartTimes.Delete(walletID)
			wg.Done() // Now this is safe since we added 1 above
			continue
		}

		logger.Info("CreateWallet sent, awaiting result...", "walletID", walletID)
	}

	// Wait until all wallet creations complete
	go func() {
		wg.Wait()
		totalDuration := time.Since(startAll).Seconds()
		logger.Info(
			"All wallets generated",
			"count",
			completedCount,
			"total_duration_seconds",
			fmt.Sprintf("%.3f", totalDuration),
		)

		// Save wallet IDs to wallets.json
		walletIDsMutex.Lock()
		data, err := json.MarshalIndent(walletIDs, "", "  ")
		walletIDsMutex.Unlock()
		if err != nil {
			logger.Error("Failed to marshal wallet IDs", err)
		} else {
			err = os.WriteFile("wallets.json", data, 0600)
			if err != nil {
				logger.Error("Failed to write wallets.json", err)
			} else {
				logger.Info("wallets.json written", "count", len(walletIDs))
			}
		}
		os.Exit(0)
	}()

	// Block on SIGINT/SIGTERM (Ctrl+C etc.)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	fmt.Println("Shutting down.")
}
