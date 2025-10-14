package recovery

import (
	"fmt"
	"os"
	"syscall"

	"github.com/fystack/mpcium/pkg/kvstore"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewRecoveryCmd creates a new recovery command group
func NewRecoveryCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "recovery",
		Short: "Recovery commands",
		Long:  "Commands for database recovery and backup operations",
	}

	// Add subcommands
	cmd.AddCommand(newRecoverCmd())

	return cmd
}

var (
	backupDir    string
	recoveryPath string
	force        bool
)

// newRecoverCmd creates a new recover command
func newRecoverCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "recover",
		Short: "Recover database from encrypted backup files",
		Long:  "Recover database from encrypted backup files",
		RunE:  recoverDatabase,
	}

	// Add flags
	cmd.Flags().StringVarP(&backupDir, "backup-dir", "b", "", "Directory containing encrypted backup files (required)")
	cmd.Flags().StringVarP(&recoveryPath, "recovery-path", "r", "", "Target path for database recovery (required)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force overwrite if recovery path already exists")
	cmd.MarkFlagRequired("backup-dir")
	cmd.MarkFlagRequired("recovery-path")

	return cmd
}

// recoverDatabase handles the database recovery from encrypted backup files
func recoverDatabase(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return fmt.Errorf("backup directory does not exist: %s", backupDir)
	}

	if _, err := os.Stat(recoveryPath); err == nil && !force {
		return fmt.Errorf("recovery path already exists: %s (use --force to overwrite)", recoveryPath)
	}

	// Prompt for encryption key
	var key []byte
	fmt.Print("Enter backup encryption key: ")
	keyBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return fmt.Errorf("failed to read encryption key: %w", err)
	}
	fmt.Println() // Add newline after password input
	key = keyBytes
	if len(key) == 0 {
		return fmt.Errorf("encryption key cannot be empty")
	}

	// Remove existing recovery path if force flag is set
	if force {
		if err := os.RemoveAll(recoveryPath); err != nil {
			return fmt.Errorf("failed to remove existing recovery path: %w", err)
		}
	}

	fmt.Printf("Starting database recovery...\n")
	fmt.Printf("Backup directory: %s\n", backupDir)
	fmt.Printf("Recovery path: %s\n", recoveryPath)

	// Create a temporary backup executor to access the backup files
	tempExecutor := kvstore.NewBadgerBackupExecutor("temp", nil, key, backupDir)

	// Perform the recovery using the existing method with specified recovery path
	if err := tempExecutor.RestoreAllBackupsEncrypted(recoveryPath, key); err != nil {
		return fmt.Errorf("recovery failed: %w", err)
	}

	fmt.Printf("✅ Database recovery completed successfully!\n")
	fmt.Printf("Restored database is available at: %s\n", recoveryPath)
	return nil
}
