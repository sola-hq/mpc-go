package benchmark

import (
	"fmt"
	"sync"
	"time"

	"github.com/fystack/mpcium/pkg/types"
	"github.com/spf13/cobra"
)

var (
	keygenNumOperations int
	keygenTimeout       int
	keygenBatchSize     int
)

// newKeygenBenchmarkCmd creates a new keygen benchmark command
func newKeygenBenchmarkCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "keygen -n<num_operations> -t<timeout> -b<batch_size>",
		Short: "Benchmark keygen operations",
		Long:  "Benchmark keygen operations",
		RunE:  runKeygenBenchmark,
	}

	// Add flags
	cmd.Flags().IntVarP(&keygenNumOperations, "num-operations", "n", 1, "Number of operations to run")
	cmd.Flags().IntVarP(&keygenTimeout, "timeout", "t", 60, "Timeout per operation in seconds")
	cmd.Flags().IntVarP(&keygenBatchSize, "batch-size", "b", 10, "Number of operations per batch")

	return cmd
}

func runKeygenBenchmark(cmd *cobra.Command, args []string) error {
	n := keygenNumOperations

	timeout := time.Duration(keygenTimeout) * time.Second * time.Duration(n)

	mpcClient, err := createMPCClient()
	if err != nil {
		return err
	}

	fmt.Printf("Starting keygen benchmark with %d operations...\n", n)
	fmt.Printf("Timeout: %v seconds\n", timeout.Seconds())

	var results []OperationResult
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Set up result listener
	err = mpcClient.OnWalletCreationResult(func(response types.KeygenResponse) {
		mu.Lock()
		defer mu.Unlock()

		for i := range results {
			if results[i].ID == response.WalletID && !results[i].Completed {
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
	var batchTimes []time.Duration

	for i := range n {
		batchStart := time.Now()
		// Generate unique wallet ID to avoid duplicates across runs
		reqID := generateUniqueID(fmt.Sprintf("benchmark-keygen-%d", i))

		result := OperationResult{
			ID:        reqID,
			StartTime: time.Now(),
		}

		mu.Lock()
		results = append(results, result)
		mu.Unlock()

		wg.Add(1)

		err := mpcClient.CreateWallet(reqID)
		if err != nil {
			fmt.Printf("CreateWallet failed for wallet %s: %v\n", reqID, err)
			mu.Lock()
			results[i].Completed = true
			results[i].ErrorCode = types.GetErrorCodeFromError(err)
			results[i].ErrorReason = err.Error()
			results[i].EndTime = time.Now()
			mu.Unlock()
			wg.Done()
		}

		// Add small delay between operations to avoid overwhelming the system
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
	case <-done: // All operations completed
	case <-time.After(timeout):
		fmt.Println("Timeout reached, some operations may still be pending")
	}

	totalTime := time.Since(startTime)

	// Calculate results
	benchResult := calculateBenchmarkResult(results, totalTime, keygenBatchSize, batchTimes)
	if err := printBenchmarkResult("Keygen", benchResult); err != nil {
		return fmt.Errorf("failed to write benchmark results: %w", err)
	}

	return nil
}
