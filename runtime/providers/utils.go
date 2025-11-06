package providers

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StringPtr is a helper function that returns a pointer to a string.
// This is commonly used across provider implementations for optional fields.
func StringPtr(s string) *string {
	return &s
}

// LoadFileAsBase64 reads a file and returns its content as a base64-encoded string.
// It supports home directory expansion (~/path) and is used by multimodal providers
// to load image files.
func LoadFileAsBase64(filePath string) (string, error) {
	// Expand home directory if needed
	if strings.HasPrefix(filePath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		filePath = filepath.Join(home, filePath[2:])
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return base64.StdEncoding.EncodeToString(data), nil
}
