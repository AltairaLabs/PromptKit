package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// LoadConfig loads and validates configuration from a YAML file in K8s-style manifest format.
// Reads all referenced resource files (scenarios, providers, tools, personas) and populates
// the Config struct, making it self-contained for programmatic use without physical files.
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var arenaConfig ArenaConfig
	if err := yaml.Unmarshal(data, &arenaConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate required K8s manifest fields
	if arenaConfig.APIVersion == "" {
		return nil, fmt.Errorf("missing required field: apiVersion")
	}
	if arenaConfig.Kind != "Arena" {
		return nil, fmt.Errorf("invalid kind: expected 'Arena', got '%s'", arenaConfig.Kind)
	}
	if arenaConfig.Metadata.Name == "" {
		return nil, fmt.Errorf("missing required field: metadata.name")
	}

	cfg := &arenaConfig.Spec

	// Determine base directory for resolving relative paths
	cfg.ConfigDir = ResolveConfigDir(cfg, filename)

	// Initialize loaded resource maps with appropriate capacity
	cfg.LoadedPromptConfigs = make(map[string]*PromptConfigData, len(cfg.PromptConfigs))
	cfg.LoadedProviders = make(map[string]*Provider, len(cfg.Providers))
	cfg.LoadedScenarios = make(map[string]*Scenario, len(cfg.Scenarios))
	cfg.LoadedTools = make([]ToolData, 0, len(cfg.Tools))
	cfg.LoadedPersonas = make(map[string]*UserPersonaPack)

	// Load all resources
	if err := cfg.loadPromptConfigs(filename); err != nil {
		return nil, err
	}
	if err := cfg.loadScenarios(filename); err != nil {
		return nil, err
	}
	if err := cfg.loadProviders(filename); err != nil {
		return nil, err
	}
	if err := cfg.loadTools(filename); err != nil {
		return nil, err
	}

	// Load self-play resources if enabled
	if cfg.SelfPlay != nil && cfg.SelfPlay.Enabled {
		if err := cfg.loadSelfPlayResources(filename); err != nil {
			return nil, err
		}
	}

	// Validate the loaded configuration (warnings only, doesn't fail)
	validator := NewConfigValidatorWithPath(cfg, filename)
	_ = validator.Validate() // Intentionally ignored - validation warnings accessible via validator.GetWarnings()

	return cfg, nil
}

// k8sManifest is an interface for K8s-style manifest types
type k8sManifest interface {
	GetAPIVersion() string
	GetKind() string
	GetName() string
	SetID(id string)
}

// loadSimpleK8sManifest is a generic loader for K8s-style manifest files
// This is used for simple config types (Scenario, Provider) that don't support legacy formats
func loadSimpleK8sManifest[T k8sManifest](filename string, expectedKind string) (T, error) {
	var zero T

	data, err := os.ReadFile(filename)
	if err != nil {
		return zero, fmt.Errorf("failed to read %s file: %w", expectedKind, err)
	}

	var config T
	if err := yaml.Unmarshal(data, &config); err != nil {
		return zero, fmt.Errorf("failed to parse %s file: %w", expectedKind, err)
	}

	// Validate required K8s manifest fields
	if config.GetAPIVersion() == "" {
		return zero, fmt.Errorf("missing required field: apiVersion")
	}
	if config.GetKind() != expectedKind {
		return zero, fmt.Errorf("invalid kind: expected '%s', got '%s'", expectedKind, config.GetKind())
	}
	if config.GetName() == "" {
		return zero, fmt.Errorf("missing required field: metadata.name")
	}

	// Use metadata.name as the ID
	config.SetID(config.GetName())
	return config, nil
}

// LoadScenario loads and parses a scenario from a YAML file in K8s-style manifest format
func LoadScenario(filename string) (*Scenario, error) {
	config, err := loadSimpleK8sManifest[*ScenarioConfig](filename, "Scenario")
	if err != nil {
		return nil, err
	}
	return &config.Spec, nil
}

// LoadProvider loads and parses a provider configuration from a YAML file in K8s-style manifest format
func LoadProvider(filename string) (*Provider, error) {
	config, err := loadSimpleK8sManifest[*ProviderConfig](filename, "Provider")
	if err != nil {
		return nil, err
	}
	return &config.Spec, nil
}

// loadPromptConfigs loads and parses all referenced prompt configurations
func (c *Config) loadPromptConfigs(configPath string) error {
	for _, ref := range c.PromptConfigs {
		if ref.File == "" {
			continue
		}

		fullPath := ResolveFilePath(configPath, ref.File)

		// Read file once
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("failed to read prompt file %s: %w", ref.File, err)
		}

		// Parse configuration
		promptConfig, err := prompt.ParsePromptConfig(data)
		if err != nil {
			return fmt.Errorf("failed to parse prompt %s: %w", ref.File, err)
		}

		// Store parsed config with metadata
		c.LoadedPromptConfigs[ref.ID] = &PromptConfigData{
			FilePath: ref.File,
			Config:   promptConfig,
			TaskType: promptConfig.Spec.TaskType,
		}
	}
	return nil
}

// loadScenarios loads all referenced scenarios
func (c *Config) loadScenarios(configPath string) error {
	for _, ref := range c.Scenarios {
		fullPath := ResolveFilePath(configPath, ref.File)
		scenario, err := LoadScenario(fullPath)
		if err != nil {
			return fmt.Errorf("failed to load scenario %s: %w", ref.File, err)
		}
		c.LoadedScenarios[scenario.ID] = scenario
	}
	return nil
}

// loadProviders loads all referenced providers
func (c *Config) loadProviders(configPath string) error {
	for _, ref := range c.Providers {
		fullPath := ResolveFilePath(configPath, ref.File)
		provider, err := LoadProvider(fullPath)
		if err != nil {
			return fmt.Errorf("failed to load provider %s: %w", ref.File, err)
		}
		c.LoadedProviders[provider.ID] = provider
	}
	return nil
}

// loadTools loads all referenced tools
func (c *Config) loadTools(configPath string) error {
	for _, ref := range c.Tools {
		fullPath := ResolveFilePath(configPath, ref.File)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("failed to read tool file %s: %w", ref.File, err)
		}
		c.LoadedTools = append(c.LoadedTools, ToolData{
			FilePath: ref.File,
			Data:     data,
		})
	}
	return nil
}

// loadSelfPlayResources loads personas and validates self-play provider references
func (c *Config) loadSelfPlayResources(configPath string) error {
	// Load personas
	for _, ref := range c.SelfPlay.Personas {
		fullPath := ResolveFilePath(configPath, ref.File)
		persona, err := LoadPersona(fullPath)
		if err != nil {
			return fmt.Errorf("failed to load persona %s: %w", ref.File, err)
		}
		c.LoadedPersonas[persona.ID] = persona
	}

	// Validate self-play provider references against main provider registry
	for _, roleConfig := range c.SelfPlay.Roles {
		if roleConfig.Provider == "" {
			return fmt.Errorf("self-play role %s must specify a provider", roleConfig.ID)
		}

		// Verify provider exists (LoadedProviders is populated before this function is called)
		if _, exists := c.LoadedProviders[roleConfig.Provider]; !exists {
			return fmt.Errorf("self-play role %s references unknown provider %s (must be defined in spec.providers)", roleConfig.ID, roleConfig.Provider)
		}
	}

	return nil
}
