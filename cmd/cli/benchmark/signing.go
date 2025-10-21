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
	signKeyType       string
	signMessage       string
	signTimeout       int
	signBatchSize     int
)

// newSigningBenchmarkCmd creates a new signing benchmark command
func newSigningBenchmarkCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "signing",
		Short: "Benchmark signing operations",
		Long:  "Benchmark signing operations for ECDSA or EdDSA keys.",
		Args:  cobra.NoArgs,
		RunE:  runSigningBenchmark,
	}

	// Add flags
	cmd.Flags().StringVar(&signWalletID, "wallet-id", "", "Wallet ID to use for signing")
	cmd.Flags().StringVarP(&signKeyType, "key-type", "k", string(types.KeyTypeSecp256k1), "Key type to use for signing")
	cmd.Flags().StringVar(&signMessage, "message", "abcde", "Message to sign")
	cmd.Flags().IntVarP(&signTimeout, "timeout", "t", 60, "Timeout per operation in seconds")
	cmd.Flags().IntVarP(&signBatchSize, "batch-size", "b", 10, "Number of operations per batch")
	cmd.Flags().IntVarP(&signNumOperations, "num-operations", "n", 1, "Number of operations to run")
	_ = cmd.MarkFlagRequired("wallet-id")

	return cmd
}

func runSigningBenchmark(cmd *cobra.Command, args []string) error {

	n := signNumOperations
	keytype := types.KeyType(signKeyType)
	timeout := time.Duration(signTimeout) * time.Second * time.Duration(n)

	mpcClient, err := createMPCClient()
	if err != nil {
		return err
	}

	fmt.Printf("Starting signing benchmark with %d operations...\n", n)
	fmt.Printf("Wallet ID: %s\nMessage: '%s'\n", signWalletID, signMessage)
	fmt.Printf("Timeout: %v seconds\n", timeout.Seconds())

	var results []OperationResult
	var batchTimes []time.Duration
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Set up result listener
	err = mpcClient.OnSignResult(func(response types.SigningResponse) {
		mu.Lock()
		defer mu.Unlock()

		for i := range results {
			if results[i].ID == response.TxID && !results[i].Completed {
				results[i].EndTime = time.Now()
				results[i].Completed = true
				results[i].ErrorCode = response.ErrorCode
				results[i].ErrorReason = response.ErrorReason
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
		batchStart := time.Now()
		// Generate unique wallet ID to avoid duplicates across runs
		txID := generateUniqueID(fmt.Sprintf("benchmark-%s-%s-sign-%d", signWalletID, keytype, i))
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
			KeyType:  keytype,
		}

		err := mpcClient.SignTransaction(&req)
		if err != nil {
			fmt.Printf("SignTransaction failed for tx %s: %v\n", txID, err)
			mu.Lock()
			results[i].Completed = true
			results[i].ErrorCode = types.GetErrorCodeFromError(err)
			results[i].ErrorReason = err.Error()
			results[i].EndTime = time.Now()
			mu.Unlock()
			wg.Done()
		}

		// Add small delay between operations
		time.Sleep(10 * time.Millisecond)

		batchTimes = append(batchTimes, time.Since(batchStart))
	}

	// Wait for all operations with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		fmt.Println("Timeout reached, some operations may still be pending")
	}

	totalTime := time.Since(startTime)

	// Calculate and print results
	benchResult := calculateBenchmarkResult(results, totalTime, signBatchSize, batchTimes)
	if err := printBenchmarkResult(fmt.Sprintf("Signing (%s)", signKeyType), benchResult); err != nil {
		return fmt.Errorf("failed to write benchmark results: %w", err)
	}

	return nil
}
