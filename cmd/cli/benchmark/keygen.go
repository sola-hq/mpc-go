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
		Args:  cobra.ExactArgs(1),
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

	timeout := time.Duration(keygenTimeout) * time.Second

	mpcClient, err := createMPCClient()
	if err != nil {
		return err
	}

	fmt.Printf("Starting keygen benchmark with %d operations...\n", n)

	var results []OperationResult
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Set up result listener
	err = mpcClient.OnWalletCreationResult(func(result types.KeygenResponse) {
		mu.Lock()
		defer mu.Unlock()

		for i := range results {
			if results[i].ID == result.WalletID && !results[i].Completed {
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
	benchResult := calculateBenchmarkResult(results, totalTime, keygenBatchSize, []time.Duration{totalTime})
	if err := printBenchmarkResult("Keygen", benchResult); err != nil {
		return fmt.Errorf("failed to write benchmark results: %w", err)
	}

	return nil
}
