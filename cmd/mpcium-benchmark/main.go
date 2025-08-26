package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fystack/mpcium/pkg/client"
	"github.com/fystack/mpcium/pkg/event"
	"github.com/fystack/mpcium/pkg/types"
	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v3"
)

type BenchmarkResult struct {
	TotalOperations  int
	SuccessfulOps    int
	FailedOps        int
	TotalTime        time.Duration
	AverageTime      time.Duration
	MedianTime       time.Duration
	OperationTimes   []time.Duration
	ErrorRate        float64
	OperationsPerSec float64
}

func main() {
	app := &cli.Command{
		Name:        "mpcium-benchmark",
		Usage:       "Benchmark tool for MPC operations",
		Description: "Run benchmarks for keygen, signing (ECDSA/EdDSA), and resharing operations",
		Commands: []*cli.Command{
			keygenBenchmarkCommand(),
			ecdsaSignBenchmarkCommand(),
			eddsaSignBenchmarkCommand(),
			reshareBenchmarkCommand(),
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "nats-url",
				Usage:    "NATS server URL",
				Value:    "nats://localhost:4222",
				Category: "connection",
			},
			&cli.StringFlag{
				Name:     "key-path",
				Usage:    "Path to event initiator private key",
				Value:    "./event_initiator.key",
				Category: "authentication",
			},
			&cli.StringFlag{
				Name:     "password",
				Usage:    "Password for encrypted key (if needed)",
				Category: "authentication",
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func keygenBenchmarkCommand() *cli.Command {
	return &cli.Command{
		Name:      "keygen",
		Usage:     "Benchmark keygen operations",
		ArgsUsage: "<num_operations>",
		Action:    runKeygenBenchmark,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "timeout",
				Usage:   "Timeout per operation in seconds",
				Value:   30,
				Aliases: []string{"t"},
			},
		},
	}
}

func ecdsaSignBenchmarkCommand() *cli.Command {
	return &cli.Command{
		Name:      "sign-ecdsa",
		Usage:     "Benchmark ECDSA signing operations",
		ArgsUsage: "<num_operations> <wallet_id>",
		Action:    runECDSASignBenchmark,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "timeout",
				Usage:   "Timeout per operation in seconds",
				Value:   30,
				Aliases: []string{"t"},
			},
		},
	}
}

func eddsaSignBenchmarkCommand() *cli.Command {
	return &cli.Command{
		Name:      "sign-eddsa",
		Usage:     "Benchmark EdDSA signing operations",
		ArgsUsage: "<num_operations> <wallet_id>",
		Action:    runEdDSASignBenchmark,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "timeout",
				Usage:   "Timeout per operation in seconds",
				Value:   30,
				Aliases: []string{"t"},
			},
		},
	}
}

func reshareBenchmarkCommand() *cli.Command {
	return &cli.Command{
		Name:      "reshare",
		Usage:     "Benchmark reshare operations",
		ArgsUsage: "<num_operations> <wallet_id>",
		Action:    runReshareBenchmark,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "timeout",
				Usage:   "Timeout per operation in seconds",
				Value:   30,
				Aliases: []string{"t"},
			},
			&cli.IntFlag{
				Name:    "new-threshold",
				Usage:   "New threshold for resharing",
				Value:   2,
				Aliases: []string{"nt"},
			},
			&cli.StringSliceFlag{
				Name:    "node-ids",
				Usage:   "Node IDs for resharing (comma separated)",
				Aliases: []string{"n"},
			},
		},
	}
}

func createMPCClient(cmd *cli.Command) (client.MPCClient, error) {
	natsURL := cmd.String("nats-url")
	keyPath := cmd.String("key-path")
	password := cmd.String("password")

	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	opts := client.Options{
		NatsConn: nc,
		KeyPath:  keyPath,
		Password: password,
	}
	return client.NewMPCClient(opts), nil
}

func runKeygenBenchmark(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("missing required argument: num_operations")
	}

	numOps := cmd.Args().Get(0)
	n, err := parseNumOps(numOps)
	if err != nil {
		return err
	}

	timeout := time.Duration(cmd.Int("timeout")) * time.Second

	mpcClient, err := createMPCClient(cmd)
	if err != nil {
		return err
	}

	fmt.Printf("Starting keygen benchmark with %d operations...\n", n)

	var results []OperationResult
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Set up result listener
	err = mpcClient.OnWalletCreationResult(func(result event.KeygenResultEvent) {
		mu.Lock()
		defer mu.Unlock()

		for i := range results {
			if results[i].ID == result.WalletID && !results[i].Completed {
				results[i].EndTime = time.Now()
				results[i].Completed = true
				results[i].Success = result.ResultType == event.ResultTypeSuccess
				if !results[i].Success {
					results[i].ErrorReason = result.ErrorReason
					results[i].ErrorCode = result.ErrorCode
				}
				wg.Done()
				break
			}
		}
	})
	if err != nil {
		return fmt.Errorf("failed to set up result listener: %w", err)
	}

	// Run operations
	startTime := time.Now()
	for i := 0; i < n; i++ {
		walletID := fmt.Sprintf("benchmark-keygen-%d-%d", time.Now().UnixNano(), i)

		result := OperationResult{
			ID:        walletID,
			StartTime: time.Now(),
		}

		mu.Lock()
		results = append(results, result)
		mu.Unlock()

		wg.Add(1)

		err := mpcClient.CreateWallet(walletID)
		if err != nil {
			mu.Lock()
			results[i].Completed = true
			results[i].Success = false
			results[i].ErrorReason = err.Error()
			results[i].EndTime = time.Now()
			mu.Unlock()
			wg.Done()
		}

		// Add small delay between operations to avoid overwhelming the system
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for all operations with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All operations completed
	case <-time.After(timeout * time.Duration(n)):
		fmt.Println("Timeout reached, some operations may still be pending")
	}

	totalTime := time.Since(startTime)

	// Calculate results
	benchResult := calculateBenchmarkResult(results, totalTime)
	printBenchmarkResult("Keygen", benchResult)

	return nil
}

func runECDSASignBenchmark(ctx context.Context, cmd *cli.Command) error {
	return runSignBenchmark(ctx, cmd, types.KeyTypeSecp256k1, "ECDSA")
}

func runEdDSASignBenchmark(ctx context.Context, cmd *cli.Command) error {
	return runSignBenchmark(ctx, cmd, types.KeyTypeEd25519, "EdDSA")
}

func runSignBenchmark(ctx context.Context, cmd *cli.Command, keyType types.KeyType, keyTypeName string) error {
	if cmd.Args().Len() < 2 {
		return fmt.Errorf("missing required arguments: num_operations and wallet_id")
	}

	numOps := cmd.Args().Get(0)
	walletID := cmd.Args().Get(1)

	n, err := parseNumOps(numOps)
	if err != nil {
		return err
	}

	timeout := time.Duration(cmd.Int("timeout")) * time.Second

	mpcClient, err := createMPCClient(cmd)
	if err != nil {
		return err
	}

	fmt.Printf("Starting %s signing benchmark with %d operations for wallet %s...\n", keyTypeName, n, walletID)

	var results []OperationResult
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Set up result listener
	err = mpcClient.OnSignResult(func(result event.SigningResultEvent) {
		mu.Lock()
		defer mu.Unlock()

		for i := range results {
			if results[i].ID == result.TxID && !results[i].Completed {
				results[i].EndTime = time.Now()
				results[i].Completed = true
				results[i].Success = result.ResultType == event.ResultTypeSuccess
				if !results[i].Success {
					results[i].ErrorReason = result.ErrorReason
					results[i].ErrorCode = string(result.ErrorCode)
				}
				wg.Done()
				break
			}
		}
	})
	if err != nil {
		return fmt.Errorf("failed to set up result listener: %w", err)
	}

	// Run operations
	startTime := time.Now()
	for i := 0; i < n; i++ {
		txID := fmt.Sprintf("benchmark-%s-sign-%d-%d", keyTypeName, time.Now().UnixNano(), i)

		// Generate random transaction data
		txData := make([]byte, 32)
		rand.Read(txData)

		msg := &types.SignTxMessage{
			KeyType:             keyType,
			WalletID:            walletID,
			NetworkInternalCode: "benchmark",
			TxID:                txID,
			Tx:                  txData,
		}

		result := OperationResult{
			ID:        txID,
			StartTime: time.Now(),
		}

		mu.Lock()
		results = append(results, result)
		mu.Unlock()

		wg.Add(1)

		err := mpcClient.SignTransaction(msg)
		if err != nil {
			mu.Lock()
			results[i].Completed = true
			results[i].Success = false
			results[i].ErrorReason = err.Error()
			results[i].EndTime = time.Now()
			mu.Unlock()
			wg.Done()
		}

		// Add small delay between operations
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for all operations with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All operations completed
	case <-time.After(timeout * time.Duration(n)):
		fmt.Println("Timeout reached, some operations may still be pending")
	}

	totalTime := time.Since(startTime)

	// Calculate results
	benchResult := calculateBenchmarkResult(results, totalTime)
	printBenchmarkResult(fmt.Sprintf("%s Signing", keyTypeName), benchResult)

	return nil
}

func runReshareBenchmark(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() < 2 {
		return fmt.Errorf("missing required arguments: num_operations and wallet_id")
	}

	numOps := cmd.Args().Get(0)
	walletID := cmd.Args().Get(1)

	n, err := parseNumOps(numOps)
	if err != nil {
		return err
	}

	timeout := time.Duration(cmd.Int("timeout")) * time.Second
	newThreshold := cmd.Int("new-threshold")
	nodeIDs := cmd.StringSlice("node-ids")

	if len(nodeIDs) == 0 {
		return fmt.Errorf("node-ids are required for resharing benchmark")
	}

	mpcClient, err := createMPCClient(cmd)
	if err != nil {
		return err
	}

	fmt.Printf("Starting reshare benchmark with %d operations for wallet %s...\n", n, walletID)

	var results []OperationResult
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Set up result listener
	err = mpcClient.OnResharingResult(func(result event.ResharingResultEvent) {
		mu.Lock()
		defer mu.Unlock()

		// For resharing, find the first incomplete operation for this wallet
		found := false
		for i := range results {
			if strings.HasPrefix(results[i].ID, result.WalletID) && !results[i].Completed {
				results[i].EndTime = time.Now()
				results[i].Completed = true
				results[i].Success = result.ResultType == event.ResultTypeSuccess
				if !results[i].Success {
					results[i].ErrorReason = result.ErrorReason
					results[i].ErrorCode = result.ErrorCode
				}
				wg.Done()
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("Warning: Received reshare result for wallet %s but no matching pending operation found\n", result.WalletID)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to set up result listener: %w", err)
	}

	// Run operations
	startTime := time.Now()
	for i := 0; i < n; i++ {
		sessionID := fmt.Sprintf("benchmark-reshare-%d-%d", time.Now().UnixNano(), i)

		msg := &types.ResharingMessage{
			SessionID:    sessionID,
			NodeIDs:      nodeIDs,
			NewThreshold: newThreshold,
			KeyType:      types.KeyTypeSecp256k1, // Default to secp256k1
			WalletID:     walletID,
		}

		result := OperationResult{
			ID:        fmt.Sprintf("%s-%d", walletID, i),
			StartTime: time.Now(),
		}

		mu.Lock()
		results = append(results, result)
		mu.Unlock()

		wg.Add(1)

		err := mpcClient.Resharing(msg)
		if err != nil {
			mu.Lock()
			results[i].Completed = true
			results[i].Success = false
			results[i].ErrorReason = err.Error()
			results[i].EndTime = time.Now()
			mu.Unlock()
			wg.Done()
		}

		// Add small delay between operations
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for all operations with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All operations completed
	case <-time.After(timeout * time.Duration(n)):
		fmt.Println("Timeout reached, some operations may still be pending")
	}

	totalTime := time.Since(startTime)

	// Calculate results
	benchResult := calculateBenchmarkResult(results, totalTime)
	printBenchmarkResult("Reshare", benchResult)

	return nil
}

type OperationResult struct {
	ID          string
	StartTime   time.Time
	EndTime     time.Time
	Completed   bool
	Success     bool
	ErrorReason string
	ErrorCode   string
}

func parseNumOps(numOps string) (int, error) {
	var n int
	_, err := fmt.Sscanf(numOps, "%d", &n)
	if err != nil {
		return 0, fmt.Errorf("invalid number of operations: %s", numOps)
	}
	if n <= 0 {
		return 0, fmt.Errorf("number of operations must be positive")
	}
	return n, nil
}

func calculateBenchmarkResult(results []OperationResult, totalTime time.Duration) BenchmarkResult {
	var operationTimes []time.Duration
	successfulOps := 0
	failedOps := 0

	for _, result := range results {
		if result.Completed {
			if result.Success {
				successfulOps++
				if !result.EndTime.IsZero() {
					operationTimes = append(operationTimes, result.EndTime.Sub(result.StartTime))
				}
			} else {
				failedOps++
			}
		} else {
			failedOps++ // Uncompleted operations are considered failed
		}
	}

	totalOperations := len(results)
	errorRate := float64(failedOps) / float64(totalOperations) * 100

	var averageTime, medianTime time.Duration
	var operationsPerSec float64

	if len(operationTimes) > 0 {
		// Calculate average
		var totalOpTime time.Duration
		for _, opTime := range operationTimes {
			totalOpTime += opTime
		}
		averageTime = totalOpTime / time.Duration(len(operationTimes))

		// Calculate median
		sort.Slice(operationTimes, func(i, j int) bool {
			return operationTimes[i] < operationTimes[j]
		})
		if len(operationTimes)%2 == 0 {
			medianTime = (operationTimes[len(operationTimes)/2-1] + operationTimes[len(operationTimes)/2]) / 2
		} else {
			medianTime = operationTimes[len(operationTimes)/2]
		}

		// Calculate operations per second
		operationsPerSec = float64(successfulOps) / totalTime.Seconds()
	}

	return BenchmarkResult{
		TotalOperations:  totalOperations,
		SuccessfulOps:    successfulOps,
		FailedOps:        failedOps,
		TotalTime:        totalTime,
		AverageTime:      averageTime,
		MedianTime:       medianTime,
		OperationTimes:   operationTimes,
		ErrorRate:        errorRate,
		OperationsPerSec: operationsPerSec,
	}
}

func printBenchmarkResult(operationType string, result BenchmarkResult) {
	fmt.Printf("\n=== %s Benchmark Results ===\n", operationType)
	fmt.Printf("Total Operations:     %d\n", result.TotalOperations)
	fmt.Printf("Successful:           %d\n", result.SuccessfulOps)
	fmt.Printf("Failed:               %d\n", result.FailedOps)
	fmt.Printf("Error Rate:           %.2f%%\n", result.ErrorRate)
	fmt.Printf("Total Time:           %v\n", result.TotalTime)
	fmt.Printf("Average Time/Op:      %v\n", result.AverageTime)
	fmt.Printf("Median Time/Op:       %v\n", result.MedianTime)
	fmt.Printf("Operations/Second:    %.2f\n", result.OperationsPerSec)

	if len(result.OperationTimes) > 0 {
		fmt.Printf("Min Time/Op:          %v\n", result.OperationTimes[0])
		fmt.Printf("Max Time/Op:          %v\n", result.OperationTimes[len(result.OperationTimes)-1])
	}

	fmt.Println("=====================================")
}
