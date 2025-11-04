package providers

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"gopkg.in/yaml.v3"
)

// MockResponseRepository provides an interface for retrieving mock responses.
// This abstraction allows mock data to come from various sources (files, databases, etc.)
// and makes MockProvider reusable across different contexts (Arena, SDK examples, unit tests).
type MockResponseRepository interface {
	// GetResponse retrieves a mock response for the given context.
	// Parameters can include scenario ID, turn number, provider ID, etc.
	// Returns the response text and any error encountered.
	GetResponse(ctx context.Context, params MockResponseParams) (string, error)
}

// MockResponseParams contains parameters for looking up mock responses.
// Different implementations may use different subsets of these fields.
type MockResponseParams struct {
	ScenarioID string // Optional: ID of the scenario being executed
	TurnNumber int    // Optional: Turn number in a multi-turn conversation
	ProviderID string // Optional: ID of the provider being mocked
	ModelName  string // Optional: Model name being mocked
}

// MockConfig represents the structure of a mock configuration file.
// This allows scenario-specific and turn-specific responses to be defined.
type MockConfig struct {
	// Default response if no specific match is found
	DefaultResponse string `yaml:"defaultResponse"`

	// Scenario-specific responses keyed by scenario ID
	Scenarios map[string]ScenarioMockConfig `yaml:"scenarios,omitempty"`
}

// ScenarioMockConfig defines mock responses for a specific scenario.
type ScenarioMockConfig struct {
	// Default response for this scenario (overrides global default)
	DefaultResponse string `yaml:"defaultResponse,omitempty"`

	// Turn-specific responses keyed by turn number (1-indexed)
	Turns map[int]string `yaml:"turns,omitempty"`
}

// FileMockRepository loads mock responses from a YAML configuration file.
// This is the default implementation for file-based mock configurations.
type FileMockRepository struct {
	config *MockConfig
}

// NewFileMockRepository creates a repository that loads mock responses from a YAML file.
// The file should follow the MockConfig structure with scenarios and turn-specific responses.
func NewFileMockRepository(configPath string) (*FileMockRepository, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read mock config file: %w", err)
	}

	var config MockConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse mock config YAML: %w", err)
	}

	return &FileMockRepository{
		config: &config,
	}, nil
}

// GetResponse retrieves a mock response based on the provided parameters.
// It follows this priority order:
// 1. Scenario + Turn specific response
// 2. Scenario default response
// 3. Global default response
// 4. Generic fallback message
func (r *FileMockRepository) GetResponse(ctx context.Context, params MockResponseParams) (string, error) {
	logger.Debug("FileMockRepository GetResponse",
		"scenario_id", params.ScenarioID,
		"turn_number", params.TurnNumber,
		"provider_id", params.ProviderID,
		"model", params.ModelName,
		"available_scenarios", getScenarioKeys(r.config.Scenarios))

	// Try scenario + turn specific response
	if params.ScenarioID != "" {
		if scenario, exists := r.config.Scenarios[params.ScenarioID]; exists {
			logger.Debug("Found scenario in config", "scenario_id", params.ScenarioID)
			if params.TurnNumber > 0 {
				if turnResponse, ok := scenario.Turns[params.TurnNumber]; ok {
					logger.Debug("Using scenario+turn specific response",
						"scenario_id", params.ScenarioID,
						"turn_number", params.TurnNumber,
						"response", turnResponse)
					return turnResponse, nil
				}
				logger.Debug("No turn-specific response found", "turn_number", params.TurnNumber, "available_turns", getTurnKeys(scenario.Turns))
			}

			// Try scenario default
			if scenario.DefaultResponse != "" {
				logger.Debug("Using scenario default response", "scenario_id", params.ScenarioID, "response", scenario.DefaultResponse)
				return scenario.DefaultResponse, nil
			}
			logger.Debug("No scenario default response configured", "scenario_id", params.ScenarioID)
		} else {
			logger.Debug("Scenario not found in config", "scenario_id", params.ScenarioID, "available_scenarios", getScenarioKeys(r.config.Scenarios))
		}
	}

	// Try global default
	if r.config.DefaultResponse != "" {
		logger.Debug("Using global default response", "response", r.config.DefaultResponse)
		return r.config.DefaultResponse, nil
	}

	// Final fallback
	fallback := fmt.Sprintf("Mock response for provider %s model %s", params.ProviderID, params.ModelName)
	logger.Debug("Using final fallback response", "response", fallback)
	return fallback, nil
}

// InMemoryMockRepository stores mock responses in memory.
// This is useful for testing and programmatic configuration without files.
type InMemoryMockRepository struct {
	responses       map[string]string // key: "scenario:turn" -> response
	defaultResponse string
}

// NewInMemoryMockRepository creates an in-memory repository with a default response.
func NewInMemoryMockRepository(defaultResponse string) *InMemoryMockRepository {
	return &InMemoryMockRepository{
		responses:       make(map[string]string),
		defaultResponse: defaultResponse,
	}
}

// SetResponse sets a mock response for a specific scenario and turn.
// Use turnNumber = 0 for scenario default, or -1 for global default.
func (r *InMemoryMockRepository) SetResponse(scenarioID string, turnNumber int, response string) {
	if scenarioID == "" && turnNumber == -1 {
		r.defaultResponse = response
		return
	}

	key := fmt.Sprintf("%s:%d", scenarioID, turnNumber)
	r.responses[key] = response
}

// GetResponse retrieves a mock response based on the provided parameters.
func (r *InMemoryMockRepository) GetResponse(ctx context.Context, params MockResponseParams) (string, error) {
	// Try scenario + turn specific
	if params.ScenarioID != "" && params.TurnNumber > 0 {
		key := fmt.Sprintf("%s:%d", params.ScenarioID, params.TurnNumber)
		if response, exists := r.responses[key]; exists {
			return response, nil
		}
	}

	// Try scenario default (turn 0)
	if params.ScenarioID != "" {
		key := fmt.Sprintf("%s:0", params.ScenarioID)
		if response, exists := r.responses[key]; exists {
			return response, nil
		}
	}

	// Global default
	if r.defaultResponse != "" {
		return r.defaultResponse, nil
	}

	// Final fallback
	return fmt.Sprintf("Mock response for provider %s model %s", params.ProviderID, params.ModelName), nil
}

// Helper functions for debug logging
func getScenarioKeys(scenarios map[string]ScenarioMockConfig) []string {
	keys := make([]string, 0, len(scenarios))
	for k := range scenarios {
		keys = append(keys, k)
	}
	return keys
}

func getTurnKeys(turns map[int]string) []string {
	keys := make([]string, 0, len(turns))
	for k := range turns {
		keys = append(keys, strconv.Itoa(k))
	}
	return keys
}
