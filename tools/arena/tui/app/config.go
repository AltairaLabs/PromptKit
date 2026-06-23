package app

import (
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

const arenaConfigFilename = "config.arena.yaml"

// DiscoverConfig resolves an arena config path from dir.
//
// If dir is itself a regular file it is returned directly.
// If dir is a directory, DiscoverConfig looks for config.arena.yaml inside it.
// Returns (path, true) on success or ("", false) when no config can be found.
func DiscoverConfig(dir string) (path string, found bool) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", false
	}

	if !info.IsDir() {
		// Caller passed a file path directly.
		return dir, true
	}

	// dir is a directory — look for the standard config filename inside it.
	candidate := filepath.Join(dir, arenaConfigFilename)
	if _, err := os.Stat(candidate); err != nil {
		return "", false
	}
	return candidate, true
}

// LoadConfig loads the arena configuration from path, sets Config and
// ConfigPath on the context, and derives ResultsDir as the out/ directory
// next to the config file.
func (c *AppContext) LoadConfig(path string) error {
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return err
	}
	c.Config = cfg
	c.ConfigPath = path
	c.ResultsDir = filepath.Join(filepath.Dir(path), "out")
	return nil
}
