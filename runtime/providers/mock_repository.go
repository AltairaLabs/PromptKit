package providers

import (
	"context"
	"fmt"
	"os"

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

	// GetTurn retrieves a mock turn response that may include tool calls.
	// This extends GetResponse to support structured turn data with tool call simulation.
	GetTurn(ctx context.Context, params MockResponseParams) (*MockTurn, error)
}

// MockResponseParams contains parameters for looking up mock responses.
// Different implementations may use different subsets of these fields.
type MockResponseParams struct {
	ScenarioID string // Optional: ID of the scenario being executed
	TurnNumber int    // Optional: Turn number in a multi-turn conversation
	ProviderID string // Optional: ID of the provider being mocked
	ModelName  string // Optional: Model name being mocked
}

// MockTurn represents a structured mock response that may include tool calls.
// This extends simple text responses to support tool call simulation.
type MockTurn struct {
	Type      string         `yaml:"type"`                 // "text" or "tool_calls"
	Content   string         `yaml:"content,omitempty"`    // Text content for the response
	ToolCalls []MockToolCall `yaml:"tool_calls,omitempty"` // Tool calls to simulate
}

// MockToolCall represents a simulated tool call from the LLM.
type MockToolCall struct {
	Name      string                 `yaml:"name"`      // Name of the tool to call
	Arguments map[string]interface{} `yaml:"arguments"` // Arguments to pass to the tool
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
	// Supports both simple string responses (backward compatibility) and structured MockTurn responses
	Turns map[int]interface{} `yaml:"turns,omitempty"`

	// Tool execution responses for repository-backed tool mocking
	ToolResponses map[string][]MockToolResponse `yaml:"tool_responses,omitempty"`
}

// MockToolResponse represents a configured response for tool execution.
type MockToolResponse struct {
	CallArgs map[string]interface{} `yaml:"call_args"`        // Match these arguments
	Result   interface{}            `yaml:"result,omitempty"` // Return this result
	Error    *MockToolError         `yaml:"error,omitempty"`  // Or return this error
}

// MockToolError represents an error response for tool execution.
type MockToolError struct {
	Type    string `yaml:"type"`    // Error type/category
	Message string `yaml:"message"` // Error message
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
	// Use GetTurn to get structured response, then extract text content
	turn, err := r.GetTurn(ctx, params)
	if err != nil {
		return "", err
	}

	return turn.Content, nil
}

// GetTurn retrieves a structured mock turn response that may include tool calls.
// This method supports both backward-compatible string responses and new structured MockTurn responses.
func (r *FileMockRepository) GetTurn(ctx context.Context, params MockResponseParams) (*MockTurn, error) {
	logger.Debug("FileMockRepository GetTurn",
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
						"response_type", fmt.Sprintf("%T", turnResponse))

					return r.parseTurnResponse(turnResponse)
				}
				logger.Debug("No turn-specific response found", "turn_number", params.TurnNumber)
			}

			// Try scenario default
			if scenario.DefaultResponse != "" {
				logger.Debug("Using scenario default response", "scenario_id", params.ScenarioID, "response", scenario.DefaultResponse)
				return &MockTurn{
					Type:    "text",
					Content: scenario.DefaultResponse,
				}, nil
			}
			logger.Debug("No scenario default response configured", "scenario_id", params.ScenarioID)
		} else {
			logger.Debug("Scenario not found in config", "scenario_id", params.ScenarioID, "available_scenarios", getScenarioKeys(r.config.Scenarios))
		}
	}

	// Try global default
	if r.config.DefaultResponse != "" {
		logger.Debug("Using global default response", "response", r.config.DefaultResponse)
		return &MockTurn{
			Type:    "text",
			Content: r.config.DefaultResponse,
		}, nil
	}

	// Final fallback
	fallback := fmt.Sprintf("Mock response for provider %s model %s", params.ProviderID, params.ModelName)
	logger.Debug("Using final fallback response", "response", fallback)
	return &MockTurn{
		Type:    "text",
		Content: fallback,
	}, nil
}

// parseTurnResponse parses a turn response that could be either a string (backward compatibility)
// or a structured MockTurn object.
func (r *FileMockRepository) parseTurnResponse(response interface{}) (*MockTurn, error) {
	switch v := response.(type) {
	case string:
		return r.parseStringResponse(v), nil
	case map[string]interface{}:
		return r.parseStructuredResponse(v)
	default:
		return nil, fmt.Errorf("unsupported turn response type: %T", response)
	}
}

// parseStringResponse creates a simple text MockTurn from a string response.
func (r *FileMockRepository) parseStringResponse(content string) *MockTurn {
	return &MockTurn{
		Type:    "text",
		Content: content,
	}
}

// parseStructuredResponse parses a map into a MockTurn structure.
func (r *FileMockRepository) parseStructuredResponse(responseMap map[string]interface{}) (*MockTurn, error) {
	turn := MockTurn{
		Type: "text", // default type
	}

	// Parse type field
	if typeVal, ok := responseMap["type"].(string); ok {
		turn.Type = typeVal
	}

	// Parse content field (supports both "content" and "response" for backward compatibility)
	if contentVal, ok := responseMap["content"].(string); ok {
		turn.Content = contentVal
	} else if responseVal, ok := responseMap["response"].(string); ok {
		// Support legacy "response" field name
		turn.Content = responseVal
	}

	// Parse tool calls field
	if toolCallsVal, ok := responseMap["tool_calls"].([]interface{}); ok {
		toolCalls, err := r.parseToolCalls(toolCallsVal)
		if err != nil {
			return nil, fmt.Errorf("failed to parse tool calls: %w", err)
		}
		turn.ToolCalls = toolCalls
		// Automatically set type to tool_calls if tool calls are present
		if turn.Type == "text" {
			turn.Type = "tool_calls"
		}
	}

	return &turn, nil
}

// parseToolCalls parses tool call data from interface{} slice.
func (r *FileMockRepository) parseToolCalls(toolCallsData []interface{}) ([]MockToolCall, error) {
	toolCalls := make([]MockToolCall, len(toolCallsData))

	for i, tc := range toolCallsData {
		tcMap, ok := tc.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("tool call %d is not a map", i)
		}

		mockTC := MockToolCall{}

		if name, ok := tcMap["name"].(string); ok {
			mockTC.Name = name
		}

		if args, ok := tcMap["arguments"].(map[string]interface{}); ok {
			mockTC.Arguments = args
		}

		toolCalls[i] = mockTC
	}

	return toolCalls, nil
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
	turn, err := r.GetTurn(ctx, params)
	if err != nil {
		return "", err
	}
	return turn.Content, nil
}

// GetTurn retrieves a structured mock turn response.
// InMemoryMockRepository currently only supports simple text responses.
func (r *InMemoryMockRepository) GetTurn(ctx context.Context, params MockResponseParams) (*MockTurn, error) {
	var content string

	// Try scenario + turn specific
	if params.ScenarioID != "" && params.TurnNumber > 0 {
		key := fmt.Sprintf("%s:%d", params.ScenarioID, params.TurnNumber)
		if response, exists := r.responses[key]; exists {
			content = response
		}
	}

	// Try scenario default (turn 0)
	if content == "" && params.ScenarioID != "" {
		key := fmt.Sprintf("%s:0", params.ScenarioID)
		if response, exists := r.responses[key]; exists {
			content = response
		}
	}

	// Global default
	if content == "" && r.defaultResponse != "" {
		content = r.defaultResponse
	}

	// Final fallback
	if content == "" {
		content = fmt.Sprintf("Mock response for provider %s model %s", params.ProviderID, params.ModelName)
	}

	return &MockTurn{
		Type:    "text",
		Content: content,
	}, nil
}

// Helper functions for debug logging
func getScenarioKeys(scenarios map[string]ScenarioMockConfig) []string {
	keys := make([]string, 0, len(scenarios))
	for k := range scenarios {
		keys = append(keys, k)
	}
	return keys
}
