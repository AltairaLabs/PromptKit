package config

import (
	"path/filepath"
	"testing"
)

// TestResolveConfigDir validates the config directory resolution behavior
func TestResolveConfigDir(t *testing.T) {
	tests := []struct {
		name           string
		configDir      string // ConfigDir field value
		configFilePath string // Main config file path
		expected       string
	}{
		{
			name:           "explicit config_dir takes precedence",
			configDir:      "/custom/config/dir",
			configFilePath: "/some/path/arena.yaml",
			expected:       "/custom/config/dir",
		},
		{
			name:           "defaults to config file directory when config_dir not set",
			configDir:      "",
			configFilePath: "/workspace/examples/customer-support/arena.yaml",
			expected:       "/workspace/examples/customer-support",
		},
		{
			name:           "defaults to current directory when neither is set",
			configDir:      "",
			configFilePath: "",
			expected:       ".",
		},
		{
			name:           "relative config_dir is used as-is",
			configDir:      "configs",
			configFilePath: "/workspace/arena.yaml",
			expected:       "configs",
		},
		{
			name:           "handles config file in current directory",
			configDir:      "",
			configFilePath: "arena.yaml",
			expected:       ".",
		},
		{
			name:           "handles deeply nested config file path",
			configDir:      "",
			configFilePath: "/workspace/team/project/config/arena.yaml",
			expected:       "/workspace/team/project/config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Defaults: Defaults{
					ConfigDir: tt.configDir,
				},
			}

			result := ResolveConfigDir(cfg, tt.configFilePath)
			if result != tt.expected {
				t.Errorf("ResolveConfigDir() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// TestResolveConfigDir_WithFileResolution tests that config files are resolved
// correctly relative to the config directory
func TestResolveConfigDir_WithFileResolution(t *testing.T) {
	tests := []struct {
		name           string
		configDir      string
		configFilePath string
		relativeFile   string
		expectedPath   string
	}{
		{
			name:           "resolve prompt config relative to explicit config_dir",
			configDir:      "/workspace/configs",
			configFilePath: "/workspace/arena.yaml",
			relativeFile:   "prompts/support.yaml",
			expectedPath:   "/workspace/configs/prompts/support.yaml",
		},
		{
			name:           "resolve provider config relative to config file directory",
			configDir:      "",
			configFilePath: "/workspace/examples/customer-support/arena.yaml",
			relativeFile:   "providers/claude.yaml",
			expectedPath:   "/workspace/examples/customer-support/providers/claude.yaml",
		},
		{
			name:           "resolve scenario relative to current directory",
			configDir:      "",
			configFilePath: "",
			relativeFile:   "scenarios/test.yaml",
			expectedPath:   "scenarios/test.yaml",
		},
		{
			name:           "absolute paths are not modified",
			configDir:      "/workspace/configs",
			configFilePath: "/workspace/arena.yaml",
			relativeFile:   "/absolute/path/to/config.yaml",
			expectedPath:   "/absolute/path/to/config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Defaults: Defaults{
					ConfigDir: tt.configDir,
				},
			}

			configDir := ResolveConfigDir(cfg, tt.configFilePath)

			// Simulate how files are resolved in the actual code
			var resolvedPath string
			if filepath.IsAbs(tt.relativeFile) {
				resolvedPath = tt.relativeFile
			} else {
				resolvedPath = filepath.Join(configDir, tt.relativeFile)
			}

			if resolvedPath != tt.expectedPath {
				t.Errorf("Resolved path = %q, expected %q", resolvedPath, tt.expectedPath)
			}
		})
	}
}

// TestResolveConfigDir_RealWorldExamples tests real-world usage patterns
func TestResolveConfigDir_RealWorldExamples(t *testing.T) {
	t.Run("standard project layout - no config_dir specified", func(t *testing.T) {
		// User runs: promptarena run -c examples/customer-support/arena.yaml
		cfg := &Config{
			Defaults: Defaults{
				ConfigDir: "", // Not specified
			},
		}
		configFilePath := "examples/customer-support/arena.yaml"

		configDir := ResolveConfigDir(cfg, configFilePath)
		expected := "examples/customer-support"

		if configDir != expected {
			t.Errorf("ConfigDir = %q, expected %q", configDir, expected)
		}

		// Verify prompt file resolution
		promptFile := "prompts/support-bot.yaml"
		resolvedPrompt := filepath.Join(configDir, promptFile)
		expectedPrompt := "examples/customer-support/prompts/support-bot.yaml"

		if resolvedPrompt != expectedPrompt {
			t.Errorf("Resolved prompt = %q, expected %q", resolvedPrompt, expectedPrompt)
		}
	})

	t.Run("centralized config directory", func(t *testing.T) {
		// User has all configs in a shared directory
		cfg := &Config{
			Defaults: Defaults{
				ConfigDir: "/shared/configs", // Explicit shared directory
			},
		}
		configFilePath := "/workspace/arena.yaml"

		configDir := ResolveConfigDir(cfg, configFilePath)
		if configDir != "/shared/configs" {
			t.Errorf("ConfigDir = %q, expected /shared/configs", configDir)
		}

		// All config files resolved from shared directory
		providerFile := "providers/openai.yaml"
		resolvedProvider := filepath.Join(configDir, providerFile)
		expectedProvider := "/shared/configs/providers/openai.yaml"

		if resolvedProvider != expectedProvider {
			t.Errorf("Resolved provider = %q, expected %q", resolvedProvider, expectedProvider)
		}
	})

	t.Run("running from current directory", func(t *testing.T) {
		// User runs: promptarena run -c arena.yaml (in current dir)
		cfg := &Config{
			Defaults: Defaults{
				ConfigDir: "", // Not specified
			},
		}
		configFilePath := "arena.yaml"

		configDir := ResolveConfigDir(cfg, configFilePath)
		expected := "."

		if configDir != expected {
			t.Errorf("ConfigDir = %q, expected %q", configDir, expected)
		}

		// Files resolved from current directory
		scenarioFile := "scenarios/test.yaml"
		resolvedScenario := filepath.Join(configDir, scenarioFile)
		expectedScenario := "scenarios/test.yaml"

		if resolvedScenario != expectedScenario {
			t.Errorf("Resolved scenario = %q, expected %q", resolvedScenario, expectedScenario)
		}
	})

	t.Run("programmatic usage without file path", func(t *testing.T) {
		// Config created programmatically, no file path available
		cfg := &Config{
			Defaults: Defaults{
				ConfigDir: "", // Not specified
			},
		}
		configFilePath := "" // No file path

		configDir := ResolveConfigDir(cfg, configFilePath)
		expected := "."

		if configDir != expected {
			t.Errorf("ConfigDir = %q, expected %q (should default to current dir)", configDir, expected)
		}
	})
}
