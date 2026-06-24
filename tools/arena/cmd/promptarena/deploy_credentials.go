package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// deployCredential is a stored deploy secret plus the non-secret coordinates it
// belongs to (for display and disambiguation). The token is kept out of the
// arena config file on purpose — see `deploy login`.
type deployCredential struct {
	Token     string `json:"token"`
	Endpoint  string `json:"endpoint,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

// deployCredentialsFilePerms keeps the credentials file owner-only; it holds tokens.
const deployCredentialsFilePerms = 0o600

// deployCredentialsDirPerms is the mode for the ~/.promptarena directory.
const deployCredentialsDirPerms = 0o700

// deployCredentialsPath returns the path to the credentials store
// (~/.promptarena/credentials).
func deployCredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, promptarenaDotDir, "credentials"), nil
}

// deployCredentialKey identifies a stored credential by the adapter provider and
// the absolute config file it configured — generic (no assumptions about the
// provider's config field names) and unambiguous per config.
func deployCredentialKey(provider, configPath string) string {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		abs = configPath
	}
	return provider + "|" + abs
}

// loadDeployCredentials reads the credentials store. A missing file is not an
// error — it returns an empty map.
func loadDeployCredentials() (map[string]deployCredential, error) {
	path, err := deployCredentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is derived from the user's home dir
	if os.IsNotExist(err) {
		return map[string]deployCredential{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials %s: %w", path, err)
	}
	creds := map[string]deployCredential{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &creds); err != nil {
			return nil, fmt.Errorf("failed to parse credentials %s: %w", path, err)
		}
	}
	return creds, nil
}

// storeDeployCredential upserts a credential keyed by {provider, configPath} and
// persists the store with owner-only permissions.
func storeDeployCredential(provider, configPath string, cred deployCredential) error {
	creds, err := loadDeployCredentials()
	if err != nil {
		return err
	}
	creds[deployCredentialKey(provider, configPath)] = cred

	path, err := deployCredentialsPath()
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), deployCredentialsDirPerms); mkErr != nil {
		return fmt.Errorf("failed to create credentials directory: %w", mkErr)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}
	if err := os.WriteFile(path, data, deployCredentialsFilePerms); err != nil {
		return fmt.Errorf("failed to write credentials %s: %w", path, err)
	}
	return nil
}

// lookupDeployCredential returns the stored token for {provider, configPath}, or
// ("", false) when absent.
func lookupDeployCredential(provider, configPath string) (string, bool) {
	creds, err := loadDeployCredentials()
	if err != nil {
		return "", false
	}
	cred, ok := creds[deployCredentialKey(provider, configPath)]
	if !ok || cred.Token == "" {
		return "", false
	}
	return cred.Token, true
}
