package utils

import (
	"fmt"
	"syscall"

	"golang.org/x/term"
)

// RequestPassword prompts for password, confirms it, validates strength, and reminds to back it up
func RequestPassword() (string, error) {
	// Warn user about password backup
	fmt.Println("IMPORTANT: Please ensure you back up your password securely.")
	fmt.Println("If lost, you won't be able to recover your private key.")

	// First password entry
	fmt.Print("Enter passphrase to encrypt private key: ")
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // newline after prompt
	if err != nil {
		return "", fmt.Errorf("failed to read passphrase: %w", err)
	}
	passphrase := string(bytePassword)

	// Confirm password
	fmt.Print("Confirm passphrase: ")
	byteConfirmation, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // newline after prompt
	if err != nil {
		return "", fmt.Errorf("failed to read confirmation passphrase: %w", err)
	}
	confirmation := string(byteConfirmation)

	// Check if passwords match
	if passphrase != confirmation {
		return "", fmt.Errorf("passphrases do not match")
	}

	// Validate password strength
	if len(passphrase) < 12 {
		return "", fmt.Errorf("passphrase too short (minimum 12 characters recommended)")
	}
	if !ContainsAtLeastNSpecial(passphrase, 1) {
		return "", fmt.Errorf("passphrase must contain at least 2 special characters")
	}

	return passphrase, nil
}

// ContainsAtLeastNSpecial checks if a string contains at least n special characters
func ContainsAtLeastNSpecial(s string, n int) bool {
	count := 0
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			count++
			if count >= n {
				return true
			}
		}
	}
	return false
}

// PromptPassword securely prompts for a password without echoing to terminal
// This is a simpler version without confirmation and validation
func PromptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // Add newline after password input
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}

	password := string(passwordBytes)
	if len(password) == 0 {
		return "", fmt.Errorf("password cannot be empty")
	}

	return password, nil
}
