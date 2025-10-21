package benchmark

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fystack/mpcium/pkg/types"
	"github.com/spf13/cobra"
)

var (
	reshareNumOperations int
	reshareWalletID      string
	reshareTimeout       int
	reshareBatchSize     int
	reshareNewThreshold  int
	reshareNodeIDs       string
)

// newResharingBenchmarkCmd creates a new resharing benchmark command
func newResharingBenchmarkCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "resharing <num_operations>",
		Short: "Benchmark resharing operations",
		Long:  "Benchmark resharing operations for ECDSA or EdDSA keys.",
		Args:  cobra.ExactArgs(1),
		RunE:  runResharingBenchmark,
	}

	// Add flags
	cmd.Flags().IntVarP(&reshareNumOperations, "num-operations", "n", 1, "Number of operations to run")
	cmd.Flags().StringVar(&reshareWalletID, "wallet-id", "", "Wallet ID to use for resharing")
	cmd.Flags().IntVarP(&reshareTimeout, "timeout", "t", 60, "Timeout per operation in seconds")
	cmd.Flags().IntVarP(&reshareBatchSize, "batch-size", "b", 10, "Number of operations per batch")
	cmd.Flags().IntVarP(&reshareNewThreshold, "new-threshold", "", 2, "New threshold for resharing")
	cmd.Flags().StringVarP(&reshareNodeIDs, "node-ids", "", "", "Node IDs for resharing (comma separated)")
	_ = cmd.MarkFlagRequired("wallet-id")
	_ = cmd.MarkFlagRequired("node-ids")

	return cmd
}

func runResharingBenchmark(cmd *cobra.Command, args []string) error {

	// Validate required flags
	if reshareWalletID == "" {
		return fmt.Errorf("wallet-id is required")
	}
	if reshareNodeIDs == "" {
		return fmt.Errorf("node-ids is required")
	}

	timeout := time.Duration(reshareTimeout) * time.Second
	newThreshold := reshareNewThreshold
	nodeIDs := strings.Split(reshareNodeIDs, ",")
	n := reshareNumOperations

	mpcClient, err := createMPCClient()
	if err != nil {
		return err
	}

	fmt.Printf("Starting resharing benchmark for wallet %s...\n", reshareWalletID)

	var results []OperationResult
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Set up result listener
	err = mpcClient.OnResharingResult(func(result types.ResharingResponse) {
		mu.Lock()
		defer mu.Unlock()

		// For resharing, find the first incomplete operation for this wallet
		found := false
		for i := range results {
			if strings.HasPrefix(results[i].ID, result.WalletID) && !results[i].Completed {
				results[i].EndTime = time.Now()
				results[i].Completed = true
				results[i].ErrorReason = result.ErrorReason
				results[i].ErrorCode = result.ErrorCode
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
	var batchTimes []time.Duration

	for i := range n {
		batchStart := time.Now()
		// Generate unique session ID to avoid duplicates across runs
		sessionID := generateUniqueID(fmt.Sprintf("benchmark-reshare-%s-%d", reshareWalletID, i))

		msg := &types.ResharingMessage{
			SessionID:    sessionID,
			NodeIDs:      nodeIDs,
			NewThreshold: newThreshold,
			KeyType:      types.KeyTypeSecp256k1, // Default to secp256k1
			WalletID:     reshareWalletID,
		}

		result := OperationResult{
			ID:        fmt.Sprintf("%s-%d", reshareWalletID, i),
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
		// All operations completed
	case <-time.After(timeout * time.Duration(n)):
		fmt.Println("Timeout reached, some operations may still be pending")
	}

	totalTime := time.Since(startTime)

	// Calculate results
	benchResult := calculateBenchmarkResult(results, totalTime, reshareBatchSize, batchTimes)
	if err := printBenchmarkResult("Reshare", benchResult); err != nil {
		return fmt.Errorf("failed to write benchmark results: %w", err)
	}

	return nil
}
