package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigValidator validates configuration consistency and references
type ConfigValidator struct {
	config     *Config
	configPath string // path to the config file for resolving relative paths
	errors     []error
	warns      []string
}

// NewConfigValidator creates a new configuration validator
func NewConfigValidator(cfg *Config) *ConfigValidator {
	return &ConfigValidator{
		config: cfg,
		errors: make([]error, 0),
		warns:  make([]string, 0),
	}
}

// NewConfigValidatorWithPath creates a new configuration validator with a config file path
// for resolving relative file references
func NewConfigValidatorWithPath(cfg *Config, configPath string) *ConfigValidator {
	return &ConfigValidator{
		config:     cfg,
		configPath: configPath,
		errors:     make([]error, 0),
		warns:      make([]string, 0),
	}
}

// Validate performs comprehensive validation of the configuration
func (v *ConfigValidator) Validate() error {
	v.validatePromptConfigs()
	v.validateProviders()
	v.validateScenarios()
	v.validatePersonas()
	v.validateSelfPlay()
	v.validateCrossReferences()

	if len(v.errors) > 0 {
		return fmt.Errorf("configuration validation failed with %d errors: %v", len(v.errors), v.errors)
	}
	return nil
}

// GetWarnings returns all validation warnings
func (v *ConfigValidator) GetWarnings() []string {
	return v.warns
}

// validatePromptConfigs validates prompt configuration entries
func (v *ConfigValidator) validatePromptConfigs() {
	if len(v.config.PromptConfigs) == 0 {
		v.warns = append(v.warns, "no prompt configs defined")
		return
	}

	seen := make(map[string]bool)
	for i, pc := range v.config.PromptConfigs {
		// Validate ID is not empty
		if pc.ID == "" {
			v.errors = append(v.errors, fmt.Errorf("prompt config at index %d missing ID", i))
			continue
		}

		// Check for duplicates
		if seen[pc.ID] {
			v.errors = append(v.errors, fmt.Errorf("duplicate prompt config ID: %s", pc.ID))
		}
		seen[pc.ID] = true

		// Validate file exists if specified
		if pc.File != "" {
			// Resolve path relative to base config directory
			configDir := ResolveConfigDir(v.config, v.configPath)
			checkPath := pc.File
			if !filepath.IsAbs(checkPath) {
				checkPath = filepath.Join(configDir, checkPath)
			}
			if _, err := os.Stat(checkPath); os.IsNotExist(err) {
				v.errors = append(v.errors, fmt.Errorf("prompt config file not found: %s", pc.File))
			}
		} else {
			v.warns = append(v.warns, fmt.Sprintf("prompt config %s has no file specified", pc.ID))
		}
	}
}

// validateProviders validates provider configurations
func (v *ConfigValidator) validateProviders() {
	if len(v.config.Providers) == 0 {
		v.warns = append(v.warns, "no providers defined")
		return
	}

	seen := make(map[string]bool)
	for _, provider := range v.config.Providers {
		// Check for duplicate files
		if seen[provider.File] {
			v.warns = append(v.warns, fmt.Sprintf("duplicate provider file: %s", provider.File))
		}
		seen[provider.File] = true

		// Validate file exists - resolve relative to config path
		checkPath := provider.File
		if v.configPath != "" {
			checkPath = ResolveFilePath(v.configPath, provider.File)
		}
		if _, err := os.Stat(checkPath); os.IsNotExist(err) {
			v.errors = append(v.errors, fmt.Errorf("provider file not found: %s", provider.File))
		}
	}
}

// validateScenarios validates scenario file references
func (v *ConfigValidator) validateScenarios() {
	if len(v.config.Scenarios) == 0 {
		v.warns = append(v.warns, "no scenarios defined")
		return
	}

	seen := make(map[string]bool)
	for _, scenario := range v.config.Scenarios {
		// Check for duplicates
		if seen[scenario.File] {
			v.warns = append(v.warns, fmt.Sprintf("duplicate scenario file: %s", scenario.File))
		}
		seen[scenario.File] = true

		// Validate file exists - resolve relative to config path
		checkPath := scenario.File
		if v.configPath != "" {
			checkPath = ResolveFilePath(v.configPath, scenario.File)
		}
		if _, err := os.Stat(checkPath); os.IsNotExist(err) {
			v.errors = append(v.errors, fmt.Errorf("scenario file not found: %s", scenario.File))
		}
	}
}

// validatePersonas validates persona configurations
func (v *ConfigValidator) validatePersonas() {
	if v.config.SelfPlay == nil || len(v.config.SelfPlay.Personas) == 0 {
		v.warns = append(v.warns, "no personas defined")
		return
	}

	seen := make(map[string]bool)
	for _, personaRef := range v.config.SelfPlay.Personas {
		// Check for duplicate files
		if seen[personaRef.File] {
			v.warns = append(v.warns, fmt.Sprintf("duplicate persona file: %s", personaRef.File))
		}
		seen[personaRef.File] = true

		// Validate file exists - resolve relative to config path
		checkPath := personaRef.File
		if v.configPath != "" {
			checkPath = ResolveFilePath(v.configPath, personaRef.File)
		}
		if _, err := os.Stat(checkPath); os.IsNotExist(err) {
			v.errors = append(v.errors, fmt.Errorf("persona file not found: %s", personaRef.File))
		}
	}
}

// validateSelfPlay validates self-play configuration
func (v *ConfigValidator) validateSelfPlay() {
	if v.config.SelfPlay == nil {
		return // Self-play is optional
	}

	if !v.config.SelfPlay.Enabled {
		return // Disabled, no need to validate
	}

	// Validate roles
	if len(v.config.SelfPlay.Roles) == 0 {
		v.warns = append(v.warns, "self-play enabled but no roles defined")
		return
	}

	seen := make(map[string]bool)

	for _, role := range v.config.SelfPlay.Roles {
		// Check for duplicate role IDs
		if seen[role.ID] {
			v.errors = append(v.errors, fmt.Errorf("duplicate self-play role ID: %s", role.ID))
		}
		seen[role.ID] = true

		// Validate provider ID reference
		if role.Provider == "" {
			v.errors = append(v.errors, fmt.Errorf("self-play role %s missing provider", role.ID))
		}
		// Note: Provider existence validation happens in loadSelfPlayResources
		// after all providers are loaded
	}
}

// validateCrossReferences validates references between components
func (v *ConfigValidator) validateCrossReferences() {
	promptIDs := v.getPromptConfigIDs()

	// Validate scenario task_type references
	for _, scenarioConfig := range v.config.Scenarios {
		scenario, err := LoadScenario(scenarioConfig.File)
		if err != nil {
			// Already reported in validateScenarios
			continue
		}

		// Check task_type exists
		if scenario.TaskType != "" && !promptIDs[scenario.TaskType] {
			v.errors = append(v.errors, fmt.Errorf("scenario %s references unknown task_type: %s", scenarioConfig.File, scenario.TaskType))
		}
	}
}

// Helper methods to build ID sets
func (v *ConfigValidator) getPromptConfigIDs() map[string]bool {
	ids := make(map[string]bool)
	for _, pc := range v.config.PromptConfigs {
		if pc.ID != "" {
			ids[pc.ID] = true
		}
	}
	return ids
}
