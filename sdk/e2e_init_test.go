//go:build e2e

package sdk

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

func init() {
	// Try to load .env file from SDK directory first, then project root
	envPaths := []string{
		".env",
		"../.env",
		filepath.Join(os.Getenv("HOME"), ".promptkit.env"),
	}

	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			break
		}
	}
}
