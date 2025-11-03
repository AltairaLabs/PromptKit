package engine

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/stretchr/testify/require"
)

// TestConversationExecutor_WarnOnUserTurnAssertions verifies that a warning is logged
// when assertions are specified on user turns (which are meaningless)
func TestConversationExecutor_WarnOnUserTurnAssertions(t *testing.T) {
	// Capture stderr output where logger writes
	originalStderr := os.Stderr
	defer func() {
		os.Stderr = originalStderr
	}()

	r, w, _ := os.Pipe()
	os.Stderr = w
	logger.SetVerbose(false)

	// Create mock turn executor
	mockTurnExec := &MockTurnExecutor{}

	// Get test prompt registry (using memory repository)
	memRepo := memory.NewMemoryPromptRepository()
	promptReg := prompt.NewRegistryWithRepository(memRepo)

	// Create executor
	executor := NewDefaultConversationExecutor(
		mockTurnExec,
		mockTurnExec,
		nil,
		promptReg,
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
	provider := &providers.OpenAIProvider{}

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

	// Close writer and read output
	w.Close()
	var output bytes.Buffer
	_, _ = output.ReadFrom(r) // Ignore error and bytes written in test

	// Should not fail
	require.False(t, result.Failed, "Conversation should not fail")

	// Should have logged a warning about assertions on user turn
	outputStr := output.String()
	require.True(t,
		strings.Contains(outputStr, "Ignoring assertions on user turn") ||
			strings.Contains(outputStr, "assertions only validate assistant responses"),
		"Expected warning about assertions on user turn in output: %s", outputStr)
}
