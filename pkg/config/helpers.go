package config

import (
	"path/filepath"
)

// ResolveOutputPath resolves an output file path relative to the output directory.
// If the filename is an absolute path, it is returned as-is.
// If the filename is empty, an empty string is returned.
// Otherwise, the filename is joined with the output directory.
func ResolveOutputPath(outDir, filename string) string {
	if filename == "" {
		return ""
	}

	if filepath.IsAbs(filename) {
		return filename
	}

	return filepath.Join(outDir, filename)
}

// ResolveFilePath resolves a file path relative to a base directory
func ResolveFilePath(basePath, filePath string) string {
	if filepath.IsAbs(filePath) {
		return filePath
	}
	return filepath.Join(filepath.Dir(basePath), filePath)
}

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
		return filepath.Dir(configFilePath)
	}

	// Default to current working directory
	return "."
}
