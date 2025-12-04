package templates

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultRepoName is the built-in shorthand for the community template repository.
	DefaultRepoName = "community"
	configDirPerm   = 0o750
	configFilePerm  = 0o640
)

// RepositoryType defines the type of template repository
type RepositoryType string

const (
	// RepositoryTypeRemoteGit is a remote git repository (HTTP/HTTPS URL)
	RepositoryTypeRemoteGit RepositoryType = "remoteGit"
	// RepositoryTypeLocal is a local filesystem path
	RepositoryTypeLocal RepositoryType = "local"
)

// Repository represents a template repository
type Repository struct {
	Type RepositoryType `yaml:"type"`
	URL  string         `yaml:"url"`
}

// RepoConfig stores named template repositories.
type RepoConfig struct {
	Repos map[string]Repository `yaml:"repos"`
}

// DefaultRepoConfigPath returns the default config file location for template repos.
func DefaultRepoConfigPath() string {
	if env := os.Getenv("PROMPTARENA_REPO_CONFIG"); env != "" {
		return env
	}
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
		return filepath.Join(os.TempDir(), "promptarena-templates-"+suffix, "repos.yaml")
	}
	return filepath.Join(configDir, "promptarena", "templates", "repos.yaml")
}

// LoadRepoConfig reads repo config from disk. Missing files return defaults.
func LoadRepoConfig(path string) (*RepoConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // user-configured path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := &RepoConfig{}
			cfg.ensureDefaults()
			return cfg, nil
		}
		return nil, fmt.Errorf("read repo config: %w", err)
	}

	// Try old format first (backward compatibility)
	var oldFormat struct {
		Repos map[string]string `yaml:"repos"`
	}
	if err := yaml.Unmarshal(data, &oldFormat); err == nil && len(oldFormat.Repos) > 0 {
		// Check if it's actually the old format by trying to unmarshal first entry
		// If we can successfully parse as map[string]string but not map[string]Repository,
		// it's the old format
		cfg := &RepoConfig{
			Repos: make(map[string]Repository),
		}
		for name, url := range oldFormat.Repos {
			cfg.Repos[name] = Repository{
				Type: detectRepoType(url),
				URL:  url,
			}
		}
		cfg.ensureDefaults()
		return cfg, nil
	}

	// Try new format
	cfg := &RepoConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse repo config: %w", err)
	}

	cfg.ensureDefaults()
	return cfg, nil
}

// Save writes the repo config to disk, creating parent directories as needed.
func (cfg *RepoConfig) Save(path string) error {
	if cfg == nil {
		return fmt.Errorf("repo config is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		return fmt.Errorf("create repo config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal repo config: %w", err)
	}
	if err := os.WriteFile(path, data, configFilePerm); err != nil {
		return fmt.Errorf("write repo config: %w", err)
	}
	return nil
}

// Add inserts or updates a repo entry.
func (cfg *RepoConfig) Add(name, url string) {
	cfg.ensureDefaults()
	cfg.Repos[name] = Repository{
		Type: detectRepoType(url),
		URL:  url,
	}
}

// detectRepoType determines if a URL/path is local or remote
func detectRepoType(urlOrPath string) RepositoryType {
	// Check if it's an HTTP/HTTPS URL
	if strings.HasPrefix(urlOrPath, "http://") || strings.HasPrefix(urlOrPath, "https://") {
		return RepositoryTypeRemoteGit
	}
	// Everything else is treated as a local filesystem path
	return RepositoryTypeLocal
}

// Remove deletes a repo entry (default repo is re-added via ensureDefaults).
func (cfg *RepoConfig) Remove(name string) {
	delete(cfg.Repos, name)
	cfg.ensureDefaults()
}

// ResolveIndex returns the Repository for a short name or creates one from a raw path.
func ResolveIndex(nameOrPath string, cfg *RepoConfig) Repository {
	if nameOrPath == "" {
		nameOrPath = DefaultRepoName
	}
	if cfg != nil {
		if repo, ok := cfg.Repos[nameOrPath]; ok {
			return repo
		}
	}
	// Not a known repo name, treat as a direct path/URL
	return Repository{
		Type: detectRepoType(nameOrPath),
		URL:  nameOrPath,
	}
}

func (cfg *RepoConfig) ensureDefaults() {
	if cfg.Repos == nil {
		cfg.Repos = make(map[string]Repository)
	}
	if _, ok := cfg.Repos[DefaultRepoName]; !ok {
		cfg.Repos[DefaultRepoName] = Repository{
			Type: RepositoryTypeRemoteGit,
			URL:  DefaultGitHubIndex,
		}
	}
	// Normalize keys for accidental spaces.
	for k, v := range cfg.Repos {
		trimmed := strings.TrimSpace(k)
		if trimmed != k {
			delete(cfg.Repos, k)
			cfg.Repos[trimmed] = v
		}
	}
}
