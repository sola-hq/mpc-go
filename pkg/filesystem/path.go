package filesystem

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafePath validates and constructs a safe file path within a base directory.
func SafePath(baseDir, filename string) (string, error) {
	cleanFilename := filepath.Clean(filename)
	if strings.Contains(cleanFilename, "..") {
		return "", fmt.Errorf("invalid filename: path traversal not allowed")
	}

	fullPath := filepath.Join(baseDir, cleanFilename)

	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for base directory: %w", err)
	}

	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	if !strings.HasPrefix(absPath, absBase) {
		return "", fmt.Errorf("path outside base directory not allowed")
	}

	return fullPath, nil
}

// ValidateFilePath validates a file path for security concerns.
func ValidateFilePath(filePath string) error {
	cleanPath := filepath.Clean(filePath)

	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("invalid file path: path traversal not allowed")
	}

	return nil
}
