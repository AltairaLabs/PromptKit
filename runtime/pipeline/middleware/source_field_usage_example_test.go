package middleware

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

// TestSourceField_UsageExample demonstrates how to use the Source field
// to distinguish between loaded and newly created messages.
func TestSourceField_UsageExample(t *testing.T) {
	// Setup: Create store with existing conversation
	store := statestore.NewMemoryStore()
	existingState := &statestore.ConversationState{
		ID:     "conv-123",
		UserID: "user-456",
		Messages: []types.Message{
			{Role: "user", Content: "What's 2+2?"},
			{Role: "assistant", Content: "2+2 equals 4"},
			{Role: "user", Content: "What's 3+3?"},
			{Role: "assistant", Content: "3+3 equals 6"},
		},
	}
	err := store.Save(context.Background(), existingState)
	if err != nil {
		t.Fatalf("failed to setup test state: %v", err)
	}

	// Create mock provider for new turn
	mockProvider := &mockProviderForSourceTest{
		response: providers.ChatResponse{
			Content: "5+5 equals 10",
		},
	}

	// Create execution context with new user message
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "What's 5+5?"},
		},
	}

	// Execute StateStore load and provider middleware
	stateConfig := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "conv-123",
		UserID:         "user-456",
	}
	loadMiddleware := StateStoreLoadMiddleware(stateConfig)
	providerMiddleware := ProviderMiddleware(mockProvider, nil, nil, nil)

	err = loadMiddleware.Process(execCtx, func() error {
		return providerMiddleware.Process(execCtx, func() error {
			return nil
		})
	})
	if err != nil {
		t.Fatalf("middleware chain failed: %v", err)
	}

	// Demonstrate: Filter messages by Source field
	t.Run("count messages by source", func(t *testing.T) {
		var loadedCount, pipelineCount, userInputCount int
		for _, msg := range execCtx.Messages {
			switch msg.Source {
			case "statestore":
				loadedCount++
			case "pipeline":
				pipelineCount++
			case "":
				userInputCount++
			}
		}

		assert.Equal(t, 4, loadedCount, "should have 4 loaded messages")
		assert.Equal(t, 1, pipelineCount, "should have 1 pipeline-generated message")
		assert.Equal(t, 1, userInputCount, "should have 1 user input message")
	})

	t.Run("get only current turn messages", func(t *testing.T) {
		// Filter to get only messages created in this turn (pipeline + user input)
		var currentTurnMessages []types.Message
		for _, msg := range execCtx.Messages {
			if msg.Source != "statestore" {
				currentTurnMessages = append(currentTurnMessages, msg)
			}
		}

		assert.Equal(t, 2, len(currentTurnMessages), "current turn should have user + assistant")
		assert.Equal(t, "user", currentTurnMessages[0].Role)
		assert.Equal(t, "What's 5+5?", currentTurnMessages[0].Content)
		assert.Equal(t, "assistant", currentTurnMessages[1].Role)
		assert.Equal(t, "5+5 equals 10", currentTurnMessages[1].Content)
	})

	t.Run("get only loaded history", func(t *testing.T) {
		// Filter to get only messages loaded from StateStore
		var historyMessages []types.Message
		for _, msg := range execCtx.Messages {
			if msg.Source == "statestore" {
				historyMessages = append(historyMessages, msg)
			}
		}

		assert.Equal(t, 4, len(historyMessages), "should have 4 history messages")
		assert.Equal(t, "What's 2+2?", historyMessages[0].Content)
		assert.Equal(t, "2+2 equals 4", historyMessages[1].Content)
	})

	t.Run("count assistant responses by source", func(t *testing.T) {
		// Count assistant responses from different sources
		var historicalResponses, newResponses int
		for _, msg := range execCtx.Messages {
			if msg.Role == "assistant" {
				if msg.Source == "statestore" {
					historicalResponses++
				} else if msg.Source == "pipeline" {
					newResponses++
				}
			}
		}

		assert.Equal(t, 2, historicalResponses, "should have 2 historical assistant messages")
		assert.Equal(t, 1, newResponses, "should have 1 new assistant message")
	})
}

// TestSourceField_SimplifiesTurnBoundaryDetection shows how Source field
// can simplify logic that previously relied on walking backward to find user messages.
func TestSourceField_SimplifiesTurnBoundaryDetection(t *testing.T) {
	// Create a complex message history with multiple turns
	messages := []types.Message{
		// Turn 1 (from StateStore)
		{Role: "user", Content: "turn 1 user", Source: "statestore"},
		{Role: "assistant", Content: "turn 1 assistant", Source: "statestore"},
		// Turn 2 (from StateStore)
		{Role: "user", Content: "turn 2 user", Source: "statestore"},
		{Role: "assistant", Content: "turn 2 tool call", Source: "statestore"},
		{Role: "tool", Content: "turn 2 tool result", Source: "statestore"},
		{Role: "assistant", Content: "turn 2 final", Source: "statestore"},
		// Turn 3 (current turn - new)
		{Role: "user", Content: "turn 3 user", Source: ""},
		{Role: "assistant", Content: "turn 3 assistant", Source: "pipeline"},
	}

	// Old way: Walk backward to find turn boundary
	t.Run("old way - backward walk", func(t *testing.T) {
		lastAssistantIdx := -1
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" {
				lastAssistantIdx = i
				break
			}
		}

		var turnMessages []types.Message
		for i := lastAssistantIdx; i >= 0; i-- {
			turnMessages = append([]types.Message{messages[i]}, turnMessages...)
			if messages[i].Role == "user" {
				break
			}
		}

		assert.Equal(t, 2, len(turnMessages))
		assert.Equal(t, "turn 3 user", turnMessages[0].Content)
	})

	// New way: Simple filter by Source field
	t.Run("new way - filter by Source", func(t *testing.T) {
		var currentTurnMessages []types.Message
		for _, msg := range messages {
			// Current turn = messages not from statestore
			if msg.Source != "statestore" {
				currentTurnMessages = append(currentTurnMessages, msg)
			}
		}

		assert.Equal(t, 2, len(currentTurnMessages))
		assert.Equal(t, "turn 3 user", currentTurnMessages[0].Content)
		assert.Equal(t, "turn 3 assistant", currentTurnMessages[1].Content)
	})

	// New way is clearer, more maintainable, and less error-prone
}
