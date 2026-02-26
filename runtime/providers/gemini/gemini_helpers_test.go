package gemini

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestConvertMessagesToGeminiContents(t *testing.T) {
	tests := []struct {
		name      string
		messages  []types.Message
		expected  int
		checkRole string
	}{
		{
			name: "Converts user messages correctly",
			messages: []types.Message{
				{Role: "user", Content: "Hello"},
			},
			expected:  1,
			checkRole: "user",
		},
		{
			name: "Converts assistant to model role",
			messages: []types.Message{
				{Role: "assistant", Content: "Hi there"},
			},
			expected:  1,
			checkRole: "model",
		},
		{
			name: "Handles multiple messages",
			messages: []types.Message{
				{Role: "user", Content: "Question 1"},
				{Role: "assistant", Content: "Answer 1"},
				{Role: "user", Content: "Question 2"},
			},
			expected: 3,
		},
		{
			name:     "Handles empty message list",
			messages: []types.Message{},
			expected: 0,
		},
		{
			name: "Preserves message content",
			messages: []types.Message{
				{Role: "user", Content: "Specific content to check"},
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contents := convertMessagesToGeminiContents(tt.messages)

			if len(contents) != tt.expected {
				t.Errorf("Expected %d contents, got %d", tt.expected, len(contents))
			}

			if tt.checkRole != "" && len(contents) > 0 {
				if contents[0].Role != tt.checkRole {
					t.Errorf("Expected role %q, got %q", tt.checkRole, contents[0].Role)
				}
			}

			// Verify content preservation
			for i, msg := range tt.messages {
				if i < len(contents) && len(contents[i].Parts) > 0 {
					if contents[i].Parts[0].Text != msg.Content {
						t.Errorf("Message %d: expected content %q, got %q",
							i, msg.Content, contents[i].Parts[0].Text)
					}
				}
			}
		})
	}
}

func TestPrepareGeminiRequest(t *testing.T) {
	tests := []struct {
		name              string
		req               providers.PredictionRequest
		defaults          providers.ProviderDefaults
		expectSystem      bool
		expectedTemp      float32
		expectedTopP      float32
		expectedMaxTokens int
	}{
		{
			name: "Uses request values when provided",
			req: providers.PredictionRequest{
				Messages:    []types.Message{{Role: "user", Content: "test"}},
				Temperature: 0.7,
				TopP:        0.9,
				MaxTokens:   100,
			},
			defaults: providers.ProviderDefaults{
				Temperature: 0.5,
				TopP:        0.95,
				MaxTokens:   2000,
			},
			expectSystem:      false,
			expectedTemp:      0.7,
			expectedTopP:      0.9,
			expectedMaxTokens: 100,
		},
		{
			name: "Falls back to defaults when zero values",
			req: providers.PredictionRequest{
				Messages:    []types.Message{{Role: "user", Content: "test"}},
				Temperature: 0,
				TopP:        0,
				MaxTokens:   0,
			},
			defaults: providers.ProviderDefaults{
				Temperature: 0.8,
				TopP:        0.92,
				MaxTokens:   1500,
			},
			expectSystem:      false,
			expectedTemp:      0.8,
			expectedTopP:      0.92,
			expectedMaxTokens: 1500,
		},
		{
			name: "Handles system message",
			req: providers.PredictionRequest{
				System:      "You are a helpful assistant",
				Messages:    []types.Message{{Role: "user", Content: "test"}},
				Temperature: 0.5,
				TopP:        0.9,
				MaxTokens:   500,
			},
			defaults: providers.ProviderDefaults{
				Temperature: 0.5,
				TopP:        0.95,
				MaxTokens:   2000,
			},
			expectSystem:      true,
			expectedTemp:      0.5,
			expectedTopP:      0.9,
			expectedMaxTokens: 500,
		},
		{
			name: "Mixed: some request values, some defaults",
			req: providers.PredictionRequest{
				Messages:    []types.Message{{Role: "user", Content: "test"}},
				Temperature: 0.6,
				TopP:        0, // Will use default
				MaxTokens:   200,
			},
			defaults: providers.ProviderDefaults{
				Temperature: 0.5,
				TopP:        0.95,
				MaxTokens:   2000,
			},
			expectSystem:      false,
			expectedTemp:      0.6,
			expectedTopP:      0.95, // From default
			expectedMaxTokens: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &Provider{
				defaults: tt.defaults,
			}

			contents, systemInstruction, temp, topP, maxTokens := provider.prepareGeminiRequest(tt.req)

			// Check contents
			if len(contents) != len(tt.req.Messages) {
				t.Errorf("Expected %d contents, got %d", len(tt.req.Messages), len(contents))
			}

			// Check system instruction
			if tt.expectSystem {
				if systemInstruction == nil {
					t.Error("Expected system instruction but got nil")
				} else if len(systemInstruction.Parts) == 0 || systemInstruction.Parts[0].Text != tt.req.System {
					t.Errorf("Expected system instruction %q, got %v",
						tt.req.System, systemInstruction)
				}
			} else {
				if systemInstruction != nil {
					t.Error("Expected no system instruction but got one")
				}
			}

			// Check parameters
			if temp != tt.expectedTemp {
				t.Errorf("Expected temperature %.2f, got %.2f", tt.expectedTemp, temp)
			}
			if topP != tt.expectedTopP {
				t.Errorf("Expected topP %.2f, got %.2f", tt.expectedTopP, topP)
			}
			if maxTokens != tt.expectedMaxTokens {
				t.Errorf("Expected maxTokens %d, got %d", tt.expectedMaxTokens, maxTokens)
			}
		})
	}
}

func TestBuildGeminiRequest(t *testing.T) {
	provider := &Provider{}

	tests := []struct {
		name              string
		contents          []geminiContent
		systemInstruction *geminiContent
		temperature       float32
		topP              float32
		maxTokens         int
		checkSafety       bool
	}{
		{
			name: "Creates request with all parameters",
			contents: []geminiContent{
				{Role: "user", Parts: []geminiPart{{Text: "Hello"}}},
			},
			systemInstruction: &geminiContent{
				Parts: []geminiPart{{Text: "You are helpful"}},
			},
			temperature: 0.7,
			topP:        0.9,
			maxTokens:   500,
			checkSafety: true,
		},
		{
			name: "Creates request without system instruction",
			contents: []geminiContent{
				{Role: "user", Parts: []geminiPart{{Text: "Test"}}},
			},
			systemInstruction: nil,
			temperature:       0.5,
			topP:              0.95,
			maxTokens:         1000,
			checkSafety:       true,
		},
		{
			name:              "Handles empty contents",
			contents:          []geminiContent{},
			systemInstruction: nil,
			temperature:       0.8,
			topP:              0.9,
			maxTokens:         100,
			checkSafety:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := provider.buildGeminiRequest(tt.contents, tt.systemInstruction, tt.temperature, tt.topP, tt.maxTokens)

			// Check contents
			if len(req.Contents) != len(tt.contents) {
				t.Errorf("Expected %d contents, got %d", len(tt.contents), len(req.Contents))
			}

			// Check system instruction
			if tt.systemInstruction != nil {
				if req.SystemInstruction == nil {
					t.Error("Expected system instruction in request but got nil")
				}
			}

			// Check generation config
			if req.GenerationConfig.Temperature != tt.temperature {
				t.Errorf("Expected temperature %.2f, got %.2f",
					tt.temperature, req.GenerationConfig.Temperature)
			}
			if req.GenerationConfig.TopP != tt.topP {
				t.Errorf("Expected topP %.2f, got %.2f",
					tt.topP, req.GenerationConfig.TopP)
			}
			if req.GenerationConfig.MaxOutputTokens != tt.maxTokens {
				t.Errorf("Expected maxTokens %d, got %d",
					tt.maxTokens, req.GenerationConfig.MaxOutputTokens)
			}

			// Check safety settings
			if tt.checkSafety {
				if len(req.SafetySettings) != 4 {
					t.Errorf("Expected 4 safety settings, got %d", len(req.SafetySettings))
				}
				for _, setting := range req.SafetySettings {
					if setting.Threshold != "BLOCK_NONE" {
						t.Errorf("Expected BLOCK_NONE threshold, got %s", setting.Threshold)
					}
				}
			}
		})
	}
}

func TestGeminiHelpers_Integration(t *testing.T) {
	t.Run("Full request preparation flow", func(t *testing.T) {
		provider := &Provider{
			defaults: providers.ProviderDefaults{
				Temperature: 0.7,
				TopP:        0.95,
				MaxTokens:   2000,
			},
		}

		req := providers.PredictionRequest{
			System: "You are a test assistant",
			Messages: []types.Message{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
				{Role: "user", Content: "How are you?"},
			},
			Temperature: 0,   // Should use default
			TopP:        0.9, // Should use this value
			MaxTokens:   0,   // Should use default
		}

		// Step 1: Prepare request
		contents, systemInstruction, temp, topP, maxTokens := provider.prepareGeminiRequest(req)

		if len(contents) != 3 {
			t.Errorf("Expected 3 contents, got %d", len(contents))
		}

		// Verify role conversion
		if contents[0].Role != "user" {
			t.Errorf("Expected first role 'user', got %q", contents[0].Role)
		}
		if contents[1].Role != "model" {
			t.Errorf("Expected second role 'model', got %q", contents[1].Role)
		}

		if systemInstruction == nil {
			t.Fatal("Expected system instruction")
		}

		if temp != 0.7 {
			t.Errorf("Expected temperature 0.7 (default), got %.2f", temp)
		}
		if topP != 0.9 {
			t.Errorf("Expected topP 0.9 (from request), got %.2f", topP)
		}
		if maxTokens != 2000 {
			t.Errorf("Expected maxTokens 2000 (default), got %d", maxTokens)
		}

		// Step 2: Build request
		geminiReq := provider.buildGeminiRequest(contents, systemInstruction, temp, topP, maxTokens)

		if geminiReq.SystemInstruction == nil {
			t.Error("Expected system instruction in built request")
		}
		if len(geminiReq.Contents) != 3 {
			t.Errorf("Expected 3 contents in built request, got %d", len(geminiReq.Contents))
		}
		if len(geminiReq.SafetySettings) != 4 {
			t.Errorf("Expected 4 safety settings, got %d", len(geminiReq.SafetySettings))
		}
	})
}
