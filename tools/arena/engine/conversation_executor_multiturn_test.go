package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// TestExecuteConversation_TenScriptedTurns tests that all 10 turns execute
// This reproduces the MCP chatbot scenario which has 10 turns
func TestExecuteConversation_TenScriptedTurns(t *testing.T) {
	// Track which turns were executed
	executedTurns := []int{}

	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			// Track turn number based on content
			turnNum := len(executedTurns)
			executedTurns = append(executedTurns, turnNum)

			// Create and save messages to StateStore
			messages := []types.Message{
				{
					Role:    "user",
					Content: req.ScriptedContent,
				},
				{
					Role:    "assistant",
					Content: "Response to turn " + req.ScriptedContent,
					CostInfo: &types.CostInfo{
						InputTokens:   10,
						OutputTokens:  20,
						InputCostUSD:  0.0001,
						OutputCostUSD: 0.0002,
						TotalCost:     0.0003,
					},
				},
			}

			if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil && req.ConversationID != "" {
				store, ok := req.StateStoreConfig.Store.(statestore.Store)
				if !ok {
					return nil
				}

				// Load existing conversation
				state, loadErr := store.Load(ctx, req.ConversationID)
				if loadErr != nil && !errors.Is(loadErr, statestore.ErrNotFound) {
					return loadErr
				}

				if state == nil {
					state = &statestore.ConversationState{
						ID:       req.ConversationID,
						UserID:   req.StateStoreConfig.UserID,
						Messages: []types.Message{},
					}
				}

				// Append new messages
				state.Messages = append(state.Messages, messages...)

				// Save back
				if saveErr := store.Save(ctx, state); saveErr != nil {
					return saveErr
				}
			}

			return nil
		},
	}

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	// Create scenario with 10 turns (like MCP chatbot memory-conversations scenario)
	scenario := &config.Scenario{
		ID:       "memory-conversations",
		TaskType: "memory-assistant",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Hi! My name is Alice and I'm a software engineer."},
			{Role: "user", Content: "What's my name and what do I do?"},
			{Role: "user", Content: "I love Python programming and I'm currently working on a web scraper project."},
			{Role: "user", Content: "What programming language do I like?"},
			{Role: "user", Content: "I work with my colleague Dana on an AI project called PromptKit."},
			{Role: "user", Content: "Who do I work with and on what project?"},
			{Role: "user", Content: "I also specialize in data processing and API integrations."},
			{Role: "user", Content: "Tell me everything you know about my technical skills."},
			{Role: "user", Content: "I enjoy hiking, reading sci-fi novels, and drinking good coffee in my free time."},
			{Role: "user", Content: "What are my hobbies?"},
		},
	}

	req := ConversationRequest{
		Region:   "default",
		Scenario: scenario,
		Provider: &MockProvider{id: "openai-gpt4o-mini"},
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose:     false,
				Temperature: 0.7,
				MaxTokens:   1000,
			},
		},
		StateStoreConfig: &StateStoreConfig{
			Store:  createTestStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-conv-10-turns",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Failed {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// CRITICAL: Should have executed ALL 10 turns
	if len(executedTurns) != 10 {
		t.Errorf("CRITICAL BUG: Expected 10 turns to execute, only %d executed", len(executedTurns))
		t.Errorf("Executed turns: %v", executedTurns)
	}

	// Should have 10 * (user + assistant) = 20 messages
	expectedMessages := 10 * 2
	if len(result.Messages) != expectedMessages {
		t.Errorf("Expected %d messages, got %d", expectedMessages, len(result.Messages))
		for i, msg := range result.Messages {
			t.Logf("Message %d: role=%s, content=%q", i, msg.Role, msg.Content)
		}
	}

	if scriptedExecutor.callCount != 10 {
		t.Errorf("Expected 10 executor calls, got %d", scriptedExecutor.callCount)
	}

	// Verify conversation state was saved correctly
	if req.StateStoreConfig != nil {
		store, ok := req.StateStoreConfig.Store.(statestore.Store)
		if !ok {
			t.Fatal("StateStore is not of correct type")
		}
		state, err := store.Load(context.Background(), req.ConversationID)
		if err != nil {
			t.Fatalf("Failed to retrieve conversation: %v", err)
		}
		if len(state.Messages) != 20 {
			t.Errorf("StateStore should have 20 messages, got %d", len(state.Messages))
		}
	}
}
