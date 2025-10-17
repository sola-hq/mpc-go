package benchmark

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fystack/mpcium/cmd/cli/utils"
	"github.com/fystack/mpcium/pkg/client"
	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/types"
	"github.com/spf13/cobra"
)

// NewBenchmarkCmd creates a new benchmark command group
func NewBenchmarkCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "benchmark",
		Short: "Benchmark tool for MPC operations",
		Long:  "Run benchmarks for keygen, signing (ECDSA/EdDSA), and resharing operations",
	}

	// Add persistent flags
	cmd.PersistentFlags().StringVar(&keyPath, "key-path", "./event_initiator.key", "Path to event initiator private key")
	cmd.PersistentFlags().BoolVarP(&promptPassword, "prompt-password", "p", false, "Prompt for encrypted key password (secure)")
	cmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug logging")
	cmd.PersistentFlags().StringVarP(&outputFile, "output", "o", "benchmark/output.txt", "Output file for benchmark results")

	// Add subcommands
	cmd.AddCommand(newKeygenBenchmarkCmd())

	return cmd
}

var (
	keyPath        string
	promptPassword bool
	debug          bool
	outputFile     string
)

func createMPCClient() (client.MPCClient, error) {
	configPath := ""
	keyPath := keyPath
	promptPassword := promptPassword
	debug := debug

	// Initialize configuration
	config.InitViperConfig(configPath)
	appConfig := config.LoadConfig()
	environment := appConfig.Environment

	// Initialize logger
	logger.Init(environment, debug)

	// Create NATS connection
	conn, err := messaging.GetNATSConnection(environment, appConfig.NATs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Handle password prompting
	var password string
	if promptPassword {
		password, err = utils.PromptPassword("Enter password for encrypted key: ")
		if err != nil {
			return nil, fmt.Errorf("failed to get password: %w", err)
		}
	}

	// Create a LocalSigner with the provided key path and password
	signerOpts := client.LocalSignerOptions{
		KeyPath:  keyPath,
		Password: password,
	}

	// Default to Ed25519 for event initiator keys
	signer, err := client.NewLocalSigner(types.EventInitiatorKeyTypeEd25519, signerOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	opts := client.Options{
		NatsConn: conn,
		Signer:   signer,
	}
	return client.NewMPCClient(opts), nil
}

// generateUniqueID creates a highly unique ID for benchmark operations
func generateUniqueID(prefix string) string {
	// Generate random bytes for extra uniqueness
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to timestamp-only if random generation fails
		return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), os.Getpid())
	}
	randomHex := hex.EncodeToString(randomBytes)

	// Combine timestamp, process ID, and random bytes
	return fmt.Sprintf("%s-%d-%d-%s", prefix, time.Now().UnixNano(), os.Getpid(), randomHex)
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

func calculateBenchmarkResult(results []OperationResult, totalTime time.Duration, batchSize int, batchTimes []time.Duration) BenchmarkResult {
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

		// Calculate operations per minute
		operationsPerSec = float64(successfulOps) / totalTime.Minutes()
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
		OperationsPerMin: operationsPerSec,
		BatchSize:        batchSize,
		TotalBatches:     len(batchTimes),
		BatchTimes:       batchTimes,
	}
}

func printBenchmarkResult(operationType string, result BenchmarkResult) error {
	// Generate the benchmark report content
	reportContent := generateBenchmarkReport(operationType, result)

	// Print to console
	fmt.Print(reportContent)

	// Write to file if specified
	if outputFile != "" {
		if err := writeBenchmarkToFile(reportContent, outputFile, operationType); err != nil {
			return fmt.Errorf("failed to write to file %s: %w", outputFile, err)
		}
		fmt.Printf("\nBenchmark results written to: %s\n", outputFile)
	}

	return nil
}

func generateBenchmarkReport(operationType string, result BenchmarkResult) string {
	var report strings.Builder

	report.WriteString("\n")
	report.WriteString("===============================\n")
	report.WriteString(fmt.Sprintf("%s BENCHMARK RESULTS SUMMARY\n", strings.ToUpper(operationType)))
	report.WriteString("===============================\n")
	report.WriteString(fmt.Sprintf("Timestamp: %s\n", time.Now().Format(time.RFC3339)))
	report.WriteString(fmt.Sprintf("Operation Type: %s\n", operationType))
	report.WriteString(fmt.Sprintf("Total benchmark time: %v\n", result.TotalTime))
	report.WriteString(fmt.Sprintf("Total batches sent: %d\n", result.TotalBatches))
	report.WriteString(fmt.Sprintf("Total requests sent: %d\n", result.TotalOperations))
	report.WriteString(fmt.Sprintf("Successful completions: %d\n", result.SuccessfulOps))
	report.WriteString(fmt.Sprintf("Failed operations: %d\n", result.FailedOps))
	report.WriteString(fmt.Sprintf("Success rate: %.2f%%\n", 100.0-result.ErrorRate))
	report.WriteString(fmt.Sprintf("Error rate: %.2f%%\n", result.ErrorRate))
	report.WriteString(fmt.Sprintf("Average operations per minute: %.2f\n", result.OperationsPerMin))

	if len(result.OperationTimes) > 0 {
		report.WriteString(fmt.Sprintf("Average operation time: %v\n", result.AverageTime))
		report.WriteString(fmt.Sprintf("Median operation time: %v\n", result.MedianTime))
	}

	report.WriteString("\n")
	report.WriteString("------------------------------\n")
	report.WriteString(fmt.Sprintf("%d REQUEST ANALYSIS\n", result.BatchSize))
	report.WriteString("------------------------------\n")

	if len(result.OperationTimes) >= result.BatchSize {
		firstNResults := result.OperationTimes[:result.BatchSize]
		if len(firstNResults) > len(result.OperationTimes) {
			firstNResults = result.OperationTimes
		}

		completedCount := len(firstNResults)
		if completedCount > result.BatchSize {
			completedCount = result.BatchSize
		}

		report.WriteString(fmt.Sprintf("Completed from first %d: %d/%d\n", result.BatchSize, completedCount, result.BatchSize))

		if len(firstNResults) > 0 {
			var totalTime time.Duration
			minTime := firstNResults[0]
			maxTime := firstNResults[0]

			for _, t := range firstNResults {
				totalTime += t
				if t < minTime {
					minTime = t
				}
				if t > maxTime {
					maxTime = t
				}
			}

			report.WriteString(fmt.Sprintf("Fastest (first %d): %v\n", result.BatchSize, minTime))
			report.WriteString(fmt.Sprintf("Slowest (first %d): %v\n", result.BatchSize, maxTime))
		}
	}

	report.WriteString("\n")

	return report.String()
}

func writeBenchmarkToFile(content, outputFile, operationType string) (err error) {
	// Create directory if it doesn't exist
	dir := filepath.Dir(outputFile)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Open file for appending (create if doesn't exist)
	file, err := os.OpenFile(outputFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	defer func() {
		if err := file.Close(); err != nil {
			logger.Error("failed to close file", err)
		}
	}()

	// Write content to file
	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	// Add separator for multiple benchmark runs
	separator := fmt.Sprintf("\n%s\n\n", strings.Repeat("=", 80))
	if _, err := file.WriteString(separator); err != nil {
		return fmt.Errorf("failed to write separator: %w", err)
	}

	return nil
}
