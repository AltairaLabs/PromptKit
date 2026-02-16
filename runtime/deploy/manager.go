package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// AdapterManager discovers and manages deploy adapter binaries.
type AdapterManager struct {
	projectDir string
	homeDir    string
}

// NewAdapterManager creates a manager that searches for adapters
// relative to the given project directory.
func NewAdapterManager(projectDir string) *AdapterManager {
	home, _ := os.UserHomeDir()
	return &AdapterManager{
		projectDir: projectDir,
		homeDir:    home,
	}
}

// AdapterBinaryName returns the expected binary name for a provider.
func AdapterBinaryName(provider string) string {
	return fmt.Sprintf("promptarena-deploy-%s", provider)
}

// Discover finds the adapter binary for the given provider.
// Search precedence:
//  1. .promptarena/adapters/ (project-local)
//  2. ~/.promptarena/adapters/ (user-level)
//  3. $PATH (system-installed)
//
// Returns the full path to the binary or an error if not found.
func (m *AdapterManager) Discover(provider string) (string, error) {
	binaryName := AdapterBinaryName(provider)

	// 1. Project-local
	localPath := filepath.Join(m.projectDir, ".promptarena", "adapters", binaryName)
	if isExecutable(localPath) {
		return localPath, nil
	}

	// 2. User-level
	if m.homeDir != "" {
		userPath := filepath.Join(m.homeDir, ".promptarena", "adapters", binaryName)
		if isExecutable(userPath) {
			return userPath, nil
		}
	}

	// 3. System PATH
	systemPath, err := exec.LookPath(binaryName)
	if err == nil {
		return systemPath, nil
	}

	return "", fmt.Errorf("adapter binary %q not found in project, user, or system paths", binaryName)
}

// isExecutable checks if a file exists and is executable.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Mode()&0o111 != 0
}
