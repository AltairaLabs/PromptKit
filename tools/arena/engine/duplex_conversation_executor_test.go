package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestDuplexConversationExecutor_RequiresDuplexConfig(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	// Scenario without duplex config should fail
	req := ConversationRequest{
		Scenario: &config.Scenario{
			ID:       "test",
			TaskType: "test",
			Turns:    []config.TurnDefinition{},
		},
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if !result.Failed {
		t.Error("Expected failure when duplex config is missing")
	}
	if result.Error == "" {
		t.Error("Expected error message")
	}
}

func TestDuplexConversationExecutor_ValidatesDuplexConfig(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	// Scenario with invalid duplex config should fail
	req := ConversationRequest{
		Scenario: &config.Scenario{
			ID:       "test",
			TaskType: "test",
			Duplex: &config.DuplexConfig{
				Timeout: "invalid-duration",
			},
			Turns: []config.TurnDefinition{},
		},
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if !result.Failed {
		t.Error("Expected failure when duplex config is invalid")
	}
	if result.Error == "" {
		t.Error("Expected error message")
	}
}

func TestDuplexConversationExecutor_RequiresStreamingProvider(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	// Create a mock provider that doesn't support streaming
	mockProvider := &mockNonStreamingProvider{}

	req := ConversationRequest{
		Provider: mockProvider,
		Scenario: &config.Scenario{
			ID:       "test",
			TaskType: "test",
			Duplex: &config.DuplexConfig{
				Timeout: "10m",
			},
			Turns: []config.TurnDefinition{},
		},
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if !result.Failed {
		t.Error("Expected failure when provider doesn't support streaming")
	}
	if result.Error == "" {
		t.Error("Expected error message about streaming support")
	}
}

func TestDuplexConversationExecutor_ShouldUseClientVAD(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	tests := []struct {
		name     string
		duplex   *config.DuplexConfig
		expected bool
	}{
		{
			name: "nil turn detection defaults to VAD",
			duplex: &config.DuplexConfig{
				TurnDetection: nil,
			},
			expected: true,
		},
		{
			name: "explicit VAD mode",
			duplex: &config.DuplexConfig{
				TurnDetection: &config.TurnDetectionConfig{
					Mode: config.TurnDetectionModeVAD,
				},
			},
			expected: true,
		},
		{
			name: "ASM mode disables client VAD",
			duplex: &config.DuplexConfig{
				TurnDetection: &config.TurnDetectionConfig{
					Mode: config.TurnDetectionModeASM,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ConversationRequest{
				Scenario: &config.Scenario{
					Duplex: tt.duplex,
				},
			}
			result := executor.shouldUseClientVAD(req)
			if result != tt.expected {
				t.Errorf("shouldUseClientVAD() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDuplexConversationExecutor_ImplementsInterface(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	// Verify executor implements ConversationExecutor interface
	var _ ConversationExecutor = executor
}

// mockNonStreamingProvider is a mock provider that doesn't support streaming
type mockNonStreamingProvider struct{}

func (m *mockNonStreamingProvider) ID() string              { return "mock" }
func (m *mockNonStreamingProvider) SupportsStreaming() bool { return false }
func (m *mockNonStreamingProvider) ShouldIncludeRawOutput() bool { return false }
func (m *mockNonStreamingProvider) Close() error            { return nil }

func (m *mockNonStreamingProvider) Predict(
	_ context.Context,
	_ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, nil
}

func (m *mockNonStreamingProvider) PredictStream(
	_ context.Context,
	_ providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	return nil, nil
}

func (m *mockNonStreamingProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

func TestDuplexConversationExecutor_BuildVADConfig(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	tests := []struct {
		name            string
		duplex          *config.DuplexConfig
		expectDefaults  bool
		silenceMs       int
		minSpeechMs     int
		maxTurnDurationS int
	}{
		{
			name: "nil turn detection uses defaults",
			duplex: &config.DuplexConfig{
				TurnDetection: nil,
			},
			expectDefaults: true,
		},
		{
			name: "nil VAD config uses defaults",
			duplex: &config.DuplexConfig{
				TurnDetection: &config.TurnDetectionConfig{
					Mode: config.TurnDetectionModeVAD,
					VAD:  nil,
				},
			},
			expectDefaults: true,
		},
		{
			name: "custom VAD settings",
			duplex: &config.DuplexConfig{
				TurnDetection: &config.TurnDetectionConfig{
					Mode: config.TurnDetectionModeVAD,
					VAD: &config.VADConfig{
						SilenceThresholdMs: 500,
						MinSpeechMs:        200,
						MaxTurnDurationS:   30,
					},
				},
			},
			expectDefaults:   false,
			silenceMs:        500,
			minSpeechMs:      200,
			maxTurnDurationS: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ConversationRequest{
				Scenario: &config.Scenario{
					Duplex: tt.duplex,
				},
			}
			cfg := executor.buildVADConfig(req)

			if tt.expectDefaults {
				// Just verify we got a config without error
				if cfg.SilenceDuration == 0 {
					t.Error("Expected non-zero default silence duration")
				}
			} else {
				// Check custom values were applied
				if cfg.SilenceDuration.Milliseconds() != int64(tt.silenceMs) {
					t.Errorf("Expected silence %dms, got %v", tt.silenceMs, cfg.SilenceDuration)
				}
			}
		})
	}
}

func TestDuplexConversationExecutor_ContainsSelfPlay(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	tests := []struct {
		name     string
		scenario *config.Scenario
		expected bool
	}{
		{
			name: "no turns",
			scenario: &config.Scenario{
				Turns: []config.TurnDefinition{},
			},
			expected: false,
		},
		{
			name: "only user/assistant roles",
			scenario: &config.Scenario{
				Turns: []config.TurnDefinition{
					{Role: "user"},
					{Role: "assistant"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.containsSelfPlay(tt.scenario)
			if result != tt.expected {
				t.Errorf("containsSelfPlay() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDuplexConversationExecutor_IsSelfPlayRole(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	// With nil registry, should always return false
	if executor.isSelfPlayRole("customer") {
		t.Error("Expected false with nil registry")
	}
	if executor.isSelfPlayRole("agent") {
		t.Error("Expected false with nil registry")
	}
}


func TestDuplexConversationExecutor_BuildSessionConfig(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	req := &ConversationRequest{
		Scenario: &config.Scenario{
			ID:       "test",
			TaskType: "test-task",
		},
	}

	cfg := executor.buildSessionConfig(req)

	if cfg == nil {
		t.Fatal("Expected non-nil config")
	}
	if cfg.Config.Type != types.ContentTypeAudio {
		t.Errorf("Expected audio content type, got %s", cfg.Config.Type)
	}
	if cfg.Config.Encoding != "pcm" {
		t.Errorf("Expected pcm encoding, got %s", cfg.Config.Encoding)
	}
	if cfg.Config.Channels != 1 {
		t.Errorf("Expected 1 channel, got %d", cfg.Config.Channels)
	}
}

func TestDuplexConversationExecutor_CalculateTotalCost(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	tests := []struct {
		name     string
		messages []types.Message
		expected types.CostInfo
	}{
		{
			name:     "empty messages",
			messages: []types.Message{},
			expected: types.CostInfo{},
		},
		{
			name: "messages without cost info",
			messages: []types.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			expected: types.CostInfo{},
		},
		{
			name: "messages with cost info",
			messages: []types.Message{
				{
					Role:    "assistant",
					Content: "response 1",
					CostInfo: &types.CostInfo{
						InputTokens:  100,
						OutputTokens: 50,
						TotalCost:    0.001,
					},
				},
				{
					Role:    "assistant",
					Content: "response 2",
					CostInfo: &types.CostInfo{
						InputTokens:  200,
						OutputTokens: 100,
						TotalCost:    0.002,
					},
				},
			},
			expected: types.CostInfo{
				InputTokens:  300,
				OutputTokens: 150,
				TotalCost:    0.003,
			},
		},
		{
			name: "mixed messages with and without cost info",
			messages: []types.Message{
				{Role: "user", Content: "hello"},
				{
					Role:    "assistant",
					Content: "response",
					CostInfo: &types.CostInfo{
						InputTokens:  50,
						OutputTokens: 25,
						TotalCost:    0.0005,
					},
				},
				{Role: "user", Content: "thanks"},
			},
			expected: types.CostInfo{
				InputTokens:  50,
				OutputTokens: 25,
				TotalCost:    0.0005,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.calculateTotalCost(tt.messages)
			if result.InputTokens != tt.expected.InputTokens {
				t.Errorf("InputTokens = %d, want %d", result.InputTokens, tt.expected.InputTokens)
			}
			if result.OutputTokens != tt.expected.OutputTokens {
				t.Errorf("OutputTokens = %d, want %d", result.OutputTokens, tt.expected.OutputTokens)
			}
			if result.TotalCost != tt.expected.TotalCost {
				t.Errorf("TotalCost = %f, want %f", result.TotalCost, tt.expected.TotalCost)
			}
		})
	}
}

func TestDuplexConversationExecutor_BuildSessionConfigWithPromptRegistry(t *testing.T) {
	executor := NewDuplexConversationExecutor(nil, nil)

	// Test with nil scenario
	req := &ConversationRequest{
		Scenario: nil,
	}
	cfg := executor.buildSessionConfig(req)
	if cfg == nil {
		t.Fatal("Expected non-nil config even with nil scenario")
	}

	// Test with empty task type
	req = &ConversationRequest{
		Scenario: &config.Scenario{
			ID:       "test",
			TaskType: "",
		},
	}
	cfg = executor.buildSessionConfig(req)
	if cfg == nil {
		t.Fatal("Expected non-nil config with empty task type")
	}
}
