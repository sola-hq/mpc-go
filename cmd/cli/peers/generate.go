package peers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// newGeneratePeersCmd creates a new generate peers command
func newGeneratePeersCmd() *cobra.Command {
	// 文件内私有变量
	var (
		generateNodes      int
		generateOutputPath string
	)

	var cmd = &cobra.Command{
		Use:   "generate",
		Short: "Generate a new peers.json file",
		Long:  "Generate a new peers.json file with the specified number of nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return generatePeers(generateNodes, generateOutputPath)
		},
	}

	// Add flags
	cmd.Flags().IntVarP(&generateNodes, "number", "n", 0, "Number of nodes to generate (required)")
	cmd.Flags().StringVarP(&generateOutputPath, "output", "o", PEERS_FILE_NAME, "Output file path")
	cmd.MarkFlagRequired("number")

	return cmd
}

func generatePeers(numberOfNodes int, outputPath string) error {
	if numberOfNodes < 1 {
		return fmt.Errorf("number of nodes must be at least 1")
	}

	// Check if file already exists
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("file %s already exists, won't overwrite", outputPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("error checking file status: %w", err)
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(outputPath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Generate peers data
	peers := make(map[string]string)
	for i := 0; i < numberOfNodes; i++ {
		nodeName := fmt.Sprintf("node%d", i)
		id, err := uuid.NewRandom()
		if err != nil {
			return fmt.Errorf("failed to generate UUID: %w", err)
		}
		peers[nodeName] = id.String()
	}

	// Convert to JSON
	peersJSON, err := json.MarshalIndent(peers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Write to file
	if err := os.WriteFile(outputPath, peersJSON, 0600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("Successfully generated peers file at %s with %d nodes\n", outputPath, numberOfNodes)
	return nil
}
