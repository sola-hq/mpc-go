package initiator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"filippo.io/age"
	"github.com/fystack/mpcium/cmd/cli/utils"
	"github.com/fystack/mpcium/pkg/encryption"
	"github.com/fystack/mpcium/pkg/filesystem"
	"github.com/fystack/mpcium/pkg/types"
	"github.com/spf13/cobra"
)

var (
	nodeName  string
	outputDir string
	encrypt   bool
	overwrite bool
	algorithm string
)

// NewGenerateInitiatorCmd creates a new generate initiator command
func NewGenerateInitiatorCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "generate",
		Short: "Generate initiator identity files",
		Long:  "Generate initiator identity files",
		RunE:  runGenerateInitiatorIdentity,
	}

	// Add flags
	cmd.Flags().StringVarP(&nodeName, "node-name", "n", "event_initiator", "Name for the initiator node")
	cmd.Flags().StringVarP(&outputDir, "output-dir", "o", ".", "Output directory for identity files")
	cmd.Flags().BoolVarP(&encrypt, "encrypt", "e", false, "Encrypt private key with Age (recommended for production)")
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "f", false, "Overwrite identity files if they already exist")
	cmd.Flags().StringVarP(&algorithm, "algorithm", "a", "ed25519", "Algorithm to use for key generation (ed25519,p256)")

	return cmd
}

// InitiatorIdentity struct to store node metadata
type InitiatorIdentity struct {
	NodeName    string `json:"node_name"`
	Algorithm   string `json:"algorithm,omitempty"`
	PublicKey   string `json:"public_key"`
	CreatedAt   string `json:"created_at"`
	CreatedBy   string `json:"created_by"`
	MachineOS   string `json:"machine_os"`
	MachineName string `json:"machine_name"`
}

func runGenerateInitiatorIdentity(cmd *cobra.Command, args []string) error {
	nodeName := nodeName
	outputDir := outputDir
	encrypt := encrypt
	overwrite := overwrite

	if algorithm == "" {
		algorithm = string(types.EventInitiatorKeyTypeEd25519)
	}

	if !slices.Contains(
		[]string{string(types.EventInitiatorKeyTypeEd25519), string(types.EventInitiatorKeyTypeP256)},
		algorithm,
	) {
		return fmt.Errorf("invalid algorithm: %s. Must be %s or %s",
			algorithm,
			types.EventInitiatorKeyTypeEd25519,
			types.EventInitiatorKeyTypeP256,
		)
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Check if files already exist before proceeding
	identityPath := filepath.Join(outputDir, fmt.Sprintf("%s.identity.json", nodeName))
	keyPath := filepath.Join(outputDir, fmt.Sprintf("%s.key", nodeName))
	encKeyPath := keyPath + ".age"

	// Check for existing identity file
	if _, err := os.Stat(identityPath); err == nil && !overwrite {
		return fmt.Errorf(
			"identity file already exists: %s (use --overwrite to force)",
			identityPath,
		)
	}

	// Check for existing key files
	if _, err := os.Stat(keyPath); err == nil && !overwrite {
		return fmt.Errorf("key file already exists: %s (use --overwrite to force)", keyPath)
	}

	if encrypt {
		if _, err := os.Stat(encKeyPath); err == nil && !overwrite {
			return fmt.Errorf(
				"encrypted key file already exists: %s (use --overwrite to force)",
				encKeyPath,
			)
		}
	}

	// Generate keys based on algorithm
	var keyData encryption.KeyData
	var err error

	if algorithm == string(types.EventInitiatorKeyTypeEd25519) {
		keyData, err = encryption.GenerateEd25519Keys()
	} else if algorithm == string(types.EventInitiatorKeyTypeP256) {
		keyData, err = encryption.GenerateP256Keys()
	}

	if err != nil {
		return fmt.Errorf("failed to generate %s keys: %w", algorithm, err)
	}

	// Get current user
	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	// Get hostname
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	// Create Identity object
	identity := InitiatorIdentity{
		NodeName:    nodeName,
		Algorithm:   algorithm,
		PublicKey:   keyData.PublicKeyHex,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		CreatedBy:   currentUser.Username,
		MachineOS:   runtime.GOOS,
		MachineName: hostname,
	}

	// Save identity JSON
	identityBytes, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal identity JSON: %w", err)
	}

	if err := os.WriteFile(identityPath, identityBytes, 0600); err != nil {
		return fmt.Errorf("failed to save identity file: %w", err)
	}

	// Handle private key (with optional encryption)
	if encrypt {
		// Use utils.RequestPassword function instead of inline password handling
		passphrase, err := utils.RequestPassword()
		if err != nil {
			return err
		}

		// Create encrypted key file
		encKeyPath := keyPath + ".age"

		// Validate the encrypted key path for security
		if err := filesystem.ValidateFilePath(encKeyPath); err != nil {
			return fmt.Errorf("invalid encrypted key file path: %w", err)
		}

		outFile, err := os.Create(encKeyPath)
		if err != nil {
			return fmt.Errorf("failed to create encrypted private key file: %w", err)
		}
		defer outFile.Close()

		// Set up age encryption
		recipient, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			return fmt.Errorf("failed to create scrypt recipient: %w", err)
		}

		identityWriter, err := age.Encrypt(outFile, recipient)
		if err != nil {
			return fmt.Errorf("failed to create age encryption writer: %w", err)
		}

		// Write the encrypted private key
		if _, err := identityWriter.Write([]byte(keyData.PrivateKeyHex)); err != nil {
			return fmt.Errorf("failed to write encrypted private key: %w", err)
		}

		if err := identityWriter.Close(); err != nil {
			return fmt.Errorf("failed to finalize age encryption: %w", err)
		}

		fmt.Println("✅ Successfully generated:")
		fmt.Println("- Encrypted Private Key:", encKeyPath)
		fmt.Println("- Identity JSON:", identityPath)
		return nil
	} else {
		fmt.Println("WARNING: You are generating the private key without encryption.")
		fmt.Println("This is less secure. Consider using --encrypt flag for better security.")

		if err := os.WriteFile(keyPath, []byte(keyData.PrivateKeyHex), 0600); err != nil {
			return fmt.Errorf("failed to save private key: %w", err)
		}
	}

	fmt.Println("✅ Successfully generated:")
	fmt.Println("- Private Key:", keyPath)
	fmt.Println("- Identity JSON:", identityPath)
	return nil
}
