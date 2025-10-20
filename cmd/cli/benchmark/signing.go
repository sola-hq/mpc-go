package benchmark

import (
	"fmt"
	"sync"
	"time"

	"github.com/fystack/mpcium/pkg/types"
	"github.com/spf13/cobra"
)

var (
	signNumOperations int
	signWalletID      string
	signMessage       string
	signTimeout       int
	signBatchSize     int
)

// newSigningBenchmarkCmd creates a new signing benchmark command
func newSigningBenchmarkCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "signing -n<num_operations> -t<timeout> -b<batch_size> -w<wallet_id> -m<message>",
		Short: "Benchmark signing operations",
		Long:  "Benchmark signing operations for ECDSA or EdDSA keys.",
		RunE:  runSigningBenchmark,
	}

	// Add flags
	cmd.Flags().StringVar(&signWalletID, "wallet-id", "", "Wallet ID to use for signing")
	cmd.Flags().StringVar(&signMessage, "message", "This is a test message for signing benchmark.", "Message to sign")
	cmd.Flags().IntVarP(&signTimeout, "timeout", "t", 60, "Timeout per operation in seconds")
	cmd.Flags().IntVarP(&signBatchSize, "batch-size", "b", 10, "Number of operations per batch")
	cmd.Flags().IntVarP(&signNumOperations, "num-operations", "n", 1, "Number of operations to run")
	_ = cmd.MarkFlagRequired("wallet-id")

	return cmd
}

func runSigningBenchmark(cmd *cobra.Command, args []string) error {

	n := signNumOperations
	timeout := time.Duration(signTimeout) * time.Second

	mpcClient, err := createMPCClient()
	if err != nil {
		return err
	}

	fmt.Printf("Starting signing benchmark with %d operations...\n", n)
	fmt.Printf("Wallet ID: %s\nMessage: '%s'\n", signWalletID, signMessage)

	var results []OperationResult
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Set up result listener
	err = mpcClient.OnSignResult(func(result types.SigningResponse) {
		mu.Lock()
		defer mu.Unlock()

		for i := range results {
			if results[i].ID == result.TxID && !results[i].Completed {
				results[i].EndTime = time.Now()
				results[i].Completed = true
				if results[i].ErrorCode != "" {
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
	for i := range n {
		txID := generateUniqueID(fmt.Sprintf("benchmark-%s-sign-%d", signWalletID, i))

		result := OperationResult{
			ID:        txID,
			StartTime: time.Now(),
		}

		mu.Lock()
		results = append(results, result)
		mu.Unlock()

		wg.Add(1)

		req := types.SigningMessage{
			TxID:     txID,
			WalletID: signWalletID,
			Tx:       []byte(signMessage),
			KeyType:  types.KeyTypeSecp256k1,
		}

		err := mpcClient.SignTransaction(&req)
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

	// Calculate and print results
	benchResult := calculateBenchmarkResult(results, totalTime, signBatchSize, []time.Duration{totalTime})
	if err := printBenchmarkResult("Signing", benchResult); err != nil {
		return fmt.Errorf("failed to write benchmark results: %w", err)
	}

	return nil
}
