package app

import (
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
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

// ResultsDirFromConfig returns the conventional results directory (out/) next
// to the given config file. Callers that build an AppContext directly (e.g.
// chat/config-inspect subcommands) use this so the path matches what
// LoadConfig would have set.
func ResultsDirFromConfig(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "out")
}

// LoadConfig loads the arena configuration from path, sets Config and
// ConfigPath on the context, and derives ResultsDir as the out/ directory
// next to the config file.
func (c *AppContext) LoadConfig(path string) error {
	cfg, err := arenaconfig.LoadConfig(path)
	if err != nil {
		return err
	}
	c.Config = cfg
	c.ConfigPath = path
	c.ResultsDir = ResultsDirFromConfig(path)
	// Auto-detect a realtime/voice config so navigating to Chat from the hub
	// starts an interactive session (the `chat` subcommand sets this itself).
	c.Voice = DetectInteractiveSession(cfg)
	return nil
}
