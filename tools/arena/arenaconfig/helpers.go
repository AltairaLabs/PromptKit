package arenaconfig

import (
	"path/filepath"
)

// ResolveConfigDir resolves the base directory for config files with proper defaulting behavior:
// 1. If cfg.Defaults.ConfigDir is explicitly set, use it
// 2. Otherwise, if configFilePath is provided, use its directory
// 3. Otherwise, default to current working directory (".")
//
// This ensures all config file types (prompts, providers, scenarios, tools) are resolved
// relative to the same base directory.
func ResolveConfigDir(cfg *Config, configFilePath string) string {
	// Use explicit config_dir if specified
	if cfg.Defaults.ConfigDir != "" {
		return cfg.Defaults.ConfigDir
	}

	// Use directory containing the main config file
	if configFilePath != "" {
		dir := filepath.Dir(configFilePath)
		return dir
	}

	// Default to current working directory
	return "."
}
