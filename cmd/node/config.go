package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/security"
	"golang.org/x/term"
)

// loadPasswordFromFile reads the BadgerDB password from a file
func loadPasswordFromFile(cfg *config.Config, filePath string) error {
	passwordBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read password file %s: %w", filePath, err)
	}

	// Trim whitespace/newlines without altering content
	password := strings.TrimSpace(string(passwordBytes))

	if password == "" {
		security.ZeroBytes(passwordBytes)
		return fmt.Errorf("password file %s is empty", filePath)
	}

	config.SetBadgerPassword(password)
	if cfg != nil {
		cfg.BadgerPassword = password
	}
	security.ZeroBytes(passwordBytes)
	security.ZeroString(&password)

	return nil
}

// Prompt user for sensitive configuration values
func promptForSensitiveCredentials(cfg *config.Config) {
	if cfg.StorageType != config.StorageTypeBadger {
		checkRequiredConfigValues(cfg)
		return
	}

	fmt.Println("WARNING: Please back up your Badger DB password in a secure location.")
	fmt.Println("If you lose this password, you will permanently lose access to your data!")

	// Prompt for badger password with confirmation
	var badgerPass []byte
	var confirmPass []byte
	var err error

	// Ensure sensitive buffers are zeroed on exit
	defer func() {
		security.ZeroBytes(badgerPass)
		security.ZeroBytes(confirmPass)
	}()

	for {
		fmt.Print("Enter Badger DB password: ")
		badgerPass, err = term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			logger.Fatal("Failed to read badger password", err)
		}
		fmt.Println() // Add newline after password input

		if len(badgerPass) == 0 {
			fmt.Println("Password cannot be empty. Please try again.")
			continue
		}

		fmt.Print("Confirm Badger DB password: ")
		confirmPass, err = term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			logger.Fatal("Failed to read confirmation password", err)
		}
		fmt.Println() // Add newline after password input

		if string(badgerPass) != string(confirmPass) {
			fmt.Println("Passwords do not match. Please try again.")
			continue
		}

		break
	}

	// Show masked password for confirmation
	passwordStr := string(badgerPass)
	maskedPassword := maskString(passwordStr)
	fmt.Printf("Password set: %s\n", maskedPassword)
	config.SetBadgerPassword(passwordStr)
	if cfg != nil {
		cfg.BadgerPassword = passwordStr
	}
	security.ZeroString(&passwordStr)
	checkRequiredConfigValues(cfg)
}

// maskString shows the first and last character of a string, replacing the middle with asterisks
func maskString(s string) string {
	if len(s) <= 2 {
		return s // Too short to mask
	}

	masked := s[0:1]
	for i := 0; i < len(s)-2; i++ {
		masked += "*"
	}
	masked += s[len(s)-1:]

	return masked
}

// Check required configuration values are present
func checkRequiredConfigValues(cfg *config.Config) {

	if cfg.EventInitiatorPubKey == "" {
		logger.Fatal("Event initiator public key is required", nil)
	}

	switch cfg.StorageType {
	case config.StorageTypeBadger:
		if config.BadgerPassword() == "" {
			logger.Fatal("Badger password is required", nil)
		}
	case config.StorageTypePostgres:
		if cfg.PostgresDSN == "" {
			logger.Fatal("Postgres DSN is required", nil)
		}
	}
}
