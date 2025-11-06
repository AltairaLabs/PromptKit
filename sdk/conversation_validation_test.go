package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
)

// TestConversationManager_CreateConversation_ValidationErrors tests that validation errors
// are properly wrapped and meaningful to callers
func TestConversationManager_CreateConversation_ValidationErrors(t *testing.T) {
	// Setup
	mockProvider := mock.NewMockProvider("test-provider", "test-model", false)
	manager, err := NewConversationManager(WithProvider(mockProvider))
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	pack := &Pack{
		ID:      "test-pack",
		Name:    "Test Pack",
		Version: "1.0.0",
		Prompts: map[string]*Prompt{
			"test": {
				ID:             "test",
				SystemTemplate: "Test template",
			},
		},
	}

	tests := []struct {
		name          string
		config        ConversationConfig
		expectedError string
	}{
		{
			name:          "missing user ID",
			config:        ConversationConfig{PromptName: "test"},
			expectedError: "user_id is required",
		},
		{
			name:          "missing prompt name",
			config:        ConversationConfig{UserID: "user123"},
			expectedError: "prompt_name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.CreateConversation(context.Background(), pack, tt.config)
			if err == nil {
				t.Fatal("expected error but got none")
			}

			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("expected error to contain '%s', got: %v", tt.expectedError, err)
			}
		})
	}
}
