package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"
	"github.com/fystack/mpcium/cmd/cli/utils"
	"github.com/fystack/mpcium/pkg/filesystem"
	"github.com/spf13/cobra"
)

var (
	identityNodeName  string
	identityPeersPath string
	identityDir       string
	encryptKey        bool
	overwrite         bool
)

// NewGenerateIdentityCmd creates a new generate identity command
func NewGenerateIdentityCmd(ctx context.Context) *cobra.Command {
	// Create generate command
	var cmd = &cobra.Command{
		Use:   "generate",
		Short: "Generate identity files with optional Age-encrypted private keys for a node",
		Long:  "Generate identity files with optional Age-encrypted private keys for a node",
		RunE:  runGenerateIdentity,
	}

	// Add flags to generate command
	cmd.Flags().StringVarP(&identityNodeName, "node", "n", "", "Node name (e.g., node0) (required)")
	cmd.Flags().StringVarP(&identityPeersPath, "peers", "p", "peers.json", "Path to peers.json file")
	cmd.Flags().StringVarP(&identityDir, "output-dir", "o", "identity", "Output directory for identity files")
	cmd.Flags().BoolVarP(&encryptKey, "encrypt", "e", false, "Encrypt private key with Age (recommended for production)")
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "f", false, "Overwrite identity files if they already exist")
	_ = cmd.MarkFlagRequired("node")

	return cmd
}

// Identity structure (for identity.json)
type Identity struct {
	NodeName  string `json:"node_name"`
	NodeID    string `json:"node_id"`
	PublicKey string `json:"public_key"` // Hex-encoded
	CreatedAt string `json:"created_at"`
}

func runGenerateIdentity(cmd *cobra.Command, args []string) error {
	nodeName := identityNodeName
	peersPath := identityPeersPath

	var passphrase string
	if encryptKey {
		var err error
		passphrase, err = utils.RequestPassword()
		if err != nil {
			return err
		}
	} else {
		fmt.Println("WARNING: Private key will NOT be encrypted. This is not recommended for production environments.")
		fmt.Println("Use --encrypt flag to enable encryption.")
	}

	// Check if peers file exists
	if _, err := os.Stat(peersPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("peers file %s does not exist", peersPath)
		}
		return fmt.Errorf("error checking peers file: %w", err)
	}

	// Validate the peers file path for security
	if err := filesystem.ValidateFilePath(peersPath); err != nil {
		return fmt.Errorf("invalid peers file path: %w", err)
	}

	// Read peers file
	peersData, err := os.ReadFile(peersPath)
	if err != nil {
		return fmt.Errorf("failed to read peers file: %w", err)
	}

	// Parse peers JSON
	var peers map[string]string
	if err := json.Unmarshal(peersData, &peers); err != nil {
		return fmt.Errorf("failed to parse peers JSON: %w", err)
	}

	// Find the node ID
	nodeID, ok := peers[nodeName]
	if !ok {
		return fmt.Errorf("node %s not found in peers file", nodeName)
	}

	// Create identity directory
	if err := os.MkdirAll(identityDir, 0750); err != nil {
		return fmt.Errorf("failed to create identity directory: %w", err)
	}

	// Generate identity for the node
	if err := generateNodeIdentity(nodeName, nodeID, identityDir, encryptKey, passphrase, overwrite); err != nil {
		return fmt.Errorf("failed to generate identity for %s: %w", nodeName, err)
	}

	fmt.Printf("Successfully generated identity files for %s\n", nodeName)
	return nil
}

// Generate identity for a node
func generateNodeIdentity(nodeName, nodeID, identityDir string, encrypt bool, passphrase string, overwrite bool) error {
	// Generate Ed25519 keypair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate keypair: %w", err)
	}

	// Prepare Identity struct
	identity := Identity{
		NodeName:  nodeName,
		NodeID:    nodeID,
		PublicKey: hex.EncodeToString(pubKey),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Save identity.json
	identityPath := filepath.Join(identityDir, fmt.Sprintf("%s_identity.json", nodeName))

	// Check if identity file already exists
	if _, err := os.Stat(identityPath); err == nil && !overwrite {
		return fmt.Errorf("identity file %s already exists. Use --overwrite to force", identityPath)
	}

	identityBytes, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal identity: %w", err)
	}
	if err := os.WriteFile(identityPath, identityBytes, 0600); err != nil {
		return fmt.Errorf("failed to write identity JSON: %w", err)
	}

	privateKeyHex := hex.EncodeToString(privKey)
	privateKeyPath := filepath.Join(identityDir, fmt.Sprintf("%s_private.key", nodeName))

	if encrypt {
		// Path for encrypted key
		encryptedKeyPath := privateKeyPath + ".age"

		// Check if encrypted key file already exists
		if _, err := os.Stat(encryptedKeyPath); err == nil && !overwrite {
			return fmt.Errorf("encrypted key file %s already exists. Use --overwrite to force", encryptedKeyPath)
		}

		// Validate the encrypted key path for security
		if err := filesystem.ValidateFilePath(encryptedKeyPath); err != nil {
			return fmt.Errorf("invalid encrypted key file path: %w", err)
		}

		// Encrypt with age and passphrase
		outFile, err := os.Create(encryptedKeyPath)
		if err != nil {
			return fmt.Errorf("failed to create encrypted private key file: %w", err)
		}
		defer outFile.Close()

		recipient, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			return fmt.Errorf("failed to create scrypt recipient: %w", err)
		}

		identityWriter, err := age.Encrypt(outFile, recipient)
		if err != nil {
			return fmt.Errorf("failed to create age encryption writer: %w", err)
		}

		if _, err := identityWriter.Write([]byte(privateKeyHex)); err != nil {
			return fmt.Errorf("failed to write encrypted private key: %w", err)
		}

		if err := identityWriter.Close(); err != nil {
			return fmt.Errorf("failed to finalize age encryption: %w", err)
		}

		fmt.Printf("Generated encrypted identity for %s: %s, %s\n", nodeName, identityPath, encryptedKeyPath)
	} else {
		// Check if unencrypted key file already exists
		if _, err := os.Stat(privateKeyPath); err == nil && !overwrite {
			return fmt.Errorf("private key file %s already exists. Use --overwrite to force", privateKeyPath)
		}

		// Save unencrypted private key
		if err := os.WriteFile(privateKeyPath, []byte(privateKeyHex), 0600); err != nil {
			return fmt.Errorf("failed to write private key: %w", err)
		}
		fmt.Printf("Generated unencrypted identity for %s: %s, %s\n", nodeName, identityPath, privateKeyPath)
	}

	return nil
}
