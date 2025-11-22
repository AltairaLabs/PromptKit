package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestBuildTurnRequest_VarsLookup(t *testing.T) {
	tests := []struct {
		name             string
		loadedConfigs    map[string]*config.PromptConfigData
		scenarioTaskType string
		expectedVars     map[string]string
	}{
		{
			name: "vars found for matching task_type",
			loadedConfigs: map[string]*config.PromptConfigData{
				"restaurant-support": {
					TaskType: "restaurant-support",
					Vars: map[string]string{
						"restaurant_name": "Sushi Haven",
						"cuisine_type":    "Japanese",
					},
				},
			},
			scenarioTaskType: "restaurant-support",
			expectedVars: map[string]string{
				"restaurant_name": "Sushi Haven",
				"cuisine_type":    "Japanese",
			},
		},
		{
			name: "no vars when task_type doesn't match",
			loadedConfigs: map[string]*config.PromptConfigData{
				"product-support": {
					TaskType: "product-support",
					Vars: map[string]string{
						"product_name": "CloudSync",
					},
				},
			},
			scenarioTaskType: "restaurant-support",
			expectedVars:     nil,
		},
		{
			name: "empty vars when config has no vars",
			loadedConfigs: map[string]*config.PromptConfigData{
				"restaurant-support": {
					TaskType: "restaurant-support",
					Vars:     nil,
				},
			},
			scenarioTaskType: "restaurant-support",
			expectedVars:     nil,
		},
		{
			name:             "no vars when LoadedPromptConfigs is empty",
			loadedConfigs:    map[string]*config.PromptConfigData{},
			scenarioTaskType: "restaurant-support",
			expectedVars:     nil,
		},
		{
			name: "vars from correct config among multiple",
			loadedConfigs: map[string]*config.PromptConfigData{
				"restaurant-support": {
					TaskType: "restaurant-support",
					Vars: map[string]string{
						"restaurant_name": "Sushi Haven",
					},
				},
				"product-support": {
					TaskType: "product-support",
					Vars: map[string]string{
						"product_name": "CloudSync",
					},
				},
			},
			scenarioTaskType: "product-support",
			expectedVars: map[string]string{
				"product_name": "CloudSync",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create executor with test prompt registry
			executor := NewDefaultConversationExecutor(
				nil,
				nil,
				nil,
				createTestPromptRegistry(t),
			).(*DefaultConversationExecutor)

			// Create test request
			req := ConversationRequest{
				Config: &config.Config{
					LoadedPromptConfigs: tt.loadedConfigs,
					ConfigDir:           "/test/dir",
					Defaults: config.Defaults{
						Temperature: 0.7,
						MaxTokens:   1000,
						Seed:        42,
					},
				},
				Scenario: &config.Scenario{
					TaskType: tt.scenarioTaskType,
				},
				Provider: &MockProvider{id: "test"},
			}

			scenarioTurn := config.TurnDefinition{
				Role:    "user",
				Content: "Test message",
			}

			// Call buildTurnRequest
			turnReq := executor.buildTurnRequest(req, scenarioTurn)

			// Verify PromptVars field
			if tt.expectedVars == nil {
				if len(turnReq.PromptVars) > 0 {
					t.Errorf("Expected no PromptVars, got %d", len(turnReq.PromptVars))
				}
			} else {
				if len(turnReq.PromptVars) != len(tt.expectedVars) {
					t.Errorf("Expected %d PromptVars, got %d", len(tt.expectedVars), len(turnReq.PromptVars))
				}

				for key, expectedVal := range tt.expectedVars {
					if actualVal, ok := turnReq.PromptVars[key]; !ok {
						t.Errorf("Missing PromptVar %s", key)
					} else if actualVal != expectedVal {
						t.Errorf("PromptVar %s: expected %s, got %s", key, expectedVal, actualVal)
					}
				}
			}

			// Verify other fields are set correctly
			if turnReq.TaskType != tt.scenarioTaskType {
				t.Errorf("Expected TaskType %s, got %s", tt.scenarioTaskType, turnReq.TaskType)
			}

			if turnReq.BaseDir != "/test/dir" {
				t.Errorf("Expected BaseDir /test/dir, got %s", turnReq.BaseDir)
			}

			tolerance := 0.0001
			if turnReq.Temperature < 0.7-tolerance || turnReq.Temperature > 0.7+tolerance {
				t.Errorf("Expected Temperature 0.7, got %f", turnReq.Temperature)
			}

			if turnReq.MaxTokens != 1000 {
				t.Errorf("Expected MaxTokens 1000, got %d", turnReq.MaxTokens)
			}
		})
	}
}

func TestBuildTurnRequest_VarsWithComplexValues(t *testing.T) {
	executor := NewDefaultConversationExecutor(
		nil,
		nil,
		nil,
		createTestPromptRegistry(t),
	).(*DefaultConversationExecutor)

	// Test with complex var values (multiline, special characters)
	req := ConversationRequest{
		Config: &config.Config{
			LoadedPromptConfigs: map[string]*config.PromptConfigData{
				"restaurant-support": {
					TaskType: "restaurant-support",
					Vars: map[string]string{
						"restaurant_name": "Sushi Haven",
						"business_hours":  "12 PM - 11 PM, closed Mondays",
						"dress_code":      "Casual",
						"special_notice":  "We offer a 10% discount for seniors & students!",
					},
				},
			},
			ConfigDir: "/test",
			Defaults: config.Defaults{
				Temperature: 0.7,
			},
		},
		Scenario: &config.Scenario{
			TaskType: "restaurant-support",
		},
		Provider: &MockProvider{id: "test"},
	}

	scenarioTurn := config.TurnDefinition{
		Role:    "user",
		Content: "Test",
	}

	turnReq := executor.buildTurnRequest(req, scenarioTurn)

	// Verify all complex values are preserved
	expected := map[string]string{
		"restaurant_name": "Sushi Haven",
		"business_hours":  "12 PM - 11 PM, closed Mondays",
		"dress_code":      "Casual",
		"special_notice":  "We offer a 10% discount for seniors & students!",
	}

	if len(turnReq.PromptVars) != len(expected) {
		t.Errorf("Expected %d vars, got %d", len(expected), len(turnReq.PromptVars))
	}

	for key, expectedVal := range expected {
		if actualVal, ok := turnReq.PromptVars[key]; !ok {
			t.Errorf("Missing var %s", key)
		} else if actualVal != expectedVal {
			t.Errorf("Var %s: expected %q, got %q", key, expectedVal, actualVal)
		}
	}
}

func TestBuildTurnRequest_TemperatureAndMaxTokens(t *testing.T) {
	executor := NewDefaultConversationExecutor(
		nil,
		nil,
		nil,
		createTestPromptRegistry(t),
	).(*DefaultConversationExecutor)

	tests := []struct {
		name              string
		configTemp        float32
		configMax         int
		reqTemp           *float64
		reqMax            *int
		expectedTemp      float64
		expectedMaxTokens int
	}{
		{
			name:              "use config defaults",
			configTemp:        0.7,
			configMax:         1000,
			reqTemp:           nil,
			reqMax:            nil,
			expectedTemp:      0.7,
			expectedMaxTokens: 1000,
		},
		{
			name:              "override temperature",
			configTemp:        0.7,
			configMax:         1000,
			reqTemp:           float64Ptr(0.9),
			reqMax:            nil,
			expectedTemp:      0.9,
			expectedMaxTokens: 1000,
		},
		{
			name:              "override max_tokens",
			configTemp:        0.7,
			configMax:         1000,
			reqTemp:           nil,
			reqMax:            intPtr(2000),
			expectedTemp:      0.7,
			expectedMaxTokens: 2000,
		},
		{
			name:              "override both",
			configTemp:        0.7,
			configMax:         1000,
			reqTemp:           float64Ptr(0.5),
			reqMax:            intPtr(500),
			expectedTemp:      0.5,
			expectedMaxTokens: 500,
		},
		{
			name:              "zero config means use provider default",
			configTemp:        0,
			configMax:         0,
			reqTemp:           nil,
			reqMax:            nil,
			expectedTemp:      0,
			expectedMaxTokens: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ConversationRequest{
				Config: &config.Config{
					LoadedPromptConfigs: map[string]*config.PromptConfigData{},
					ConfigDir:           "/test",
					Defaults: config.Defaults{
						Temperature: tt.configTemp,
						MaxTokens:   tt.configMax,
					},
				},
				Scenario: &config.Scenario{
					TaskType: "test",
				},
				Provider:    &MockProvider{id: "test"},
				Temperature: tt.reqTemp,
				MaxTokens:   tt.reqMax,
			}

			scenarioTurn := config.TurnDefinition{
				Role:    "user",
				Content: "Test",
			}

			turnReq := executor.buildTurnRequest(req, scenarioTurn)

			tolerance := 0.0001
			if turnReq.Temperature < tt.expectedTemp-tolerance || turnReq.Temperature > tt.expectedTemp+tolerance {
				t.Errorf("Expected Temperature %f, got %f", tt.expectedTemp, turnReq.Temperature)
			}

			if turnReq.MaxTokens != tt.expectedMaxTokens {
				t.Errorf("Expected MaxTokens %d, got %d", tt.expectedMaxTokens, turnReq.MaxTokens)
			}
		})
	}
}

func TestBuildTurnRequest_StateStoreConfig(t *testing.T) {
	executor := NewDefaultConversationExecutor(
		nil,
		nil,
		nil,
		createTestPromptRegistry(t),
	).(*DefaultConversationExecutor)

	store := statestore.NewMemoryStore()

	req := ConversationRequest{
		Config: &config.Config{
			LoadedPromptConfigs: map[string]*config.PromptConfigData{},
			ConfigDir:           "/test",
			Defaults: config.Defaults{
				Temperature: 0.7,
			},
		},
		Scenario: &config.Scenario{
			TaskType: "test",
		},
		Provider: &MockProvider{id: "test"},
		StateStoreConfig: &StateStoreConfig{
			Store:  store,
			UserID: "test-user-123",
		},
		ConversationID: "conv-456",
	}

	scenarioTurn := config.TurnDefinition{
		Role:    "user",
		Content: "Test",
	}

	turnReq := executor.buildTurnRequest(req, scenarioTurn)

	// Verify StateStoreConfig is correctly converted
	if turnReq.StateStoreConfig == nil {
		t.Fatal("Expected StateStoreConfig to be set")
	}

	if turnReq.StateStoreConfig.UserID != "test-user-123" {
		t.Errorf("Expected UserID test-user-123, got %s", turnReq.StateStoreConfig.UserID)
	}

	if turnReq.ConversationID != "conv-456" {
		t.Errorf("Expected ConversationID conv-456, got %s", turnReq.ConversationID)
	}
}

// Helper functions
func float64Ptr(f float64) *float64 {
	return &f
}

func intPtr(i int) *int {
	return &i
}

// MockProvider for testing (reuse from conversation_executor_test.go)
var _ providers.Provider = (*MockTestProvider)(nil)

type MockTestProvider struct {
	id string
}

func (m *MockTestProvider) ID() string {
	return m.id
}

func (m *MockTestProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{
		Content: "mock response",
	}, nil
}

func (m *MockTestProvider) ShouldIncludeRawOutput() bool {
	return false
}

func (m *MockTestProvider) Close() error {
	return nil
}

func (m *MockTestProvider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
}

func (m *MockTestProvider) SupportsStreaming() bool {
	return false
}

func (m *MockTestProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}
