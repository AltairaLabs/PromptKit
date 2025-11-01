package sdk

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestConvertMessagesToProvider(t *testing.T) {
	tests := []struct {
		name     string
		messages []types.Message
		expected int
	}{
		{
			name:     "empty messages",
			messages: []types.Message{},
			expected: 0,
		},
		{
			name: "single message",
			messages: []types.Message{
				{Role: "user", Content: "Hello"},
			},
			expected: 1,
		},
		{
			name: "multiple messages",
			messages: []types.Message{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there!"},
				{Role: "user", Content: "How are you?"},
			},
			expected: 3,
		},
		{
			name: "messages with tool calls",
			messages: []types.Message{
				{Role: "user", Content: "Check the weather"},
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_1", Name: "get_weather", Args: []byte(`{"location":"SF"}`)},
					},
				},
				{
					Role: "tool",
					ToolResult: &types.MessageToolResult{
						ID:      "call_1",
						Name:    "get_weather",
						Content: "Sunny, 72°F",
					},
				},
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMessagesToProvider(tt.messages)

			if len(result) != tt.expected {
				t.Errorf("expected %d messages, got %d", tt.expected, len(result))
			}

			// Verify it's a copy, not the same slice
			if len(tt.messages) > 0 && &tt.messages[0] == &result[0] {
				t.Error("expected a copy of messages, got same reference")
			}

			// Verify content is the same
			for i, msg := range tt.messages {
				if msg.Role != result[i].Role {
					t.Errorf("message %d: expected role %s, got %s", i, msg.Role, result[i].Role)
				}
				if msg.Content != result[i].Content {
					t.Errorf("message %d: expected content %s, got %s", i, msg.Content, result[i].Content)
				}
				if len(msg.ToolCalls) != len(result[i].ToolCalls) {
					t.Errorf("message %d: expected %d tool calls, got %d", i, len(msg.ToolCalls), len(result[i].ToolCalls))
				}
			}
		})
	}
}

func TestGenerateConversationID(t *testing.T) {
	// Test that it generates unique IDs
	id1 := generateConversationID()
	id2 := generateConversationID()

	// Should start with "conv_"
	if !strings.HasPrefix(id1, "conv_") {
		t.Errorf("expected ID to start with 'conv_', got: %s", id1)
	}
	if !strings.HasPrefix(id2, "conv_") {
		t.Errorf("expected ID to start with 'conv_', got: %s", id2)
	}

	// Should not be empty
	if id1 == "" {
		t.Error("expected non-empty ID")
	}
	if id2 == "" {
		t.Error("expected non-empty ID")
	}

	// Check format: conv_<numbers>
	if !strings.Contains(id1, "_") {
		t.Errorf("expected ID to contain underscore: %s", id1)
	}
	if !strings.Contains(id2, "_") {
		t.Errorf("expected ID to contain underscore: %s", id2)
	}
}
