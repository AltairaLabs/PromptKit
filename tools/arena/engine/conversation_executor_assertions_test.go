package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/stretchr/testify/require"
)

// TestConversationExecutor_DebugOnUserTurnAssertions verifies that a debug message is logged
// when assertions are specified on user turns (which will validate assistant responses)
func TestConversationExecutor_DebugOnUserTurnAssertions(t *testing.T) {
	// Capture log output using a buffer
	var output bytes.Buffer
	logger.SetOutput(&output)
	logger.SetVerbose(true) // Enable debug logging
	defer func() {
		logger.SetOutput(nil) // Reset to stderr
		logger.SetVerbose(false)
	}()

	// Create mock turn executor
	mockTurnExec := &MockTurnExecutor{}

	// Get test prompt registry (using memory repository)
	memRepo := memory.NewPromptRepository()
	promptReg := prompt.NewRegistryWithRepository(memRepo)

	// Create executor
	executor := NewDefaultConversationExecutor(
		mockTurnExec,
		mockTurnExec,
		nil,
		promptReg,
		nil,
	)

	// Create scenario with assertions on user turn (should trigger warning)
	scenario := &config.Scenario{
		TaskType: "test",
		Turns: []config.TurnDefinition{
			{
				Role:    "user",
				Content: "Test question",
				Assertions: []assertions.AssertionConfig{
					{
						Type: "content_includes",
						Params: map[string]interface{}{
							"patterns": []string{"test"},
						},
					},
				},
			},
		},
	}

	// Create provider
	provider := &openai.Provider{}

	// Create config
	cfg := &config.Config{
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   100,
			Seed:        42,
		},
	}

	// Create state store
	store := statestore.NewMemoryStore()

	// Execute conversation
	req := ConversationRequest{
		Provider: provider,
		Scenario: scenario,
		Config:   cfg,
		Region:   "default",
		StateStoreConfig: &StateStoreConfig{
			Store:  store,
			UserID: "test-user",
		},
		ConversationID: "test-conv",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	// Should not fail
	require.False(t, result.Failed, "Conversation should not fail")

	// Should have logged a debug message about assertions on user turn
	outputStr := output.String()
	require.True(t,
		strings.Contains(outputStr, "Assertions on user turn will validate next assistant response"),
		"Expected debug message about assertions on user turn in output: %s", outputStr)
}
