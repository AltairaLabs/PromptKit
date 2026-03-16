package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// TestNaturalTermination_CompletesAfterMinTurns verifies that ErrConversationComplete
// stops the loop when the completion happens at or above the minimum turn count.
func TestNaturalTermination_CompletesAfterMinTurns(t *testing.T) {
	selfPlayCallCount := 0

	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			messages := []types.Message{
				{Role: "user", Content: req.ScriptedContent},
				{Role: "assistant", Content: "Response"},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			selfPlayCallCount++
			messages := []types.Message{
				{Role: "user", Content: "Question"},
				{Role: "assistant", Content: "Answer"},
			}
			if err := saveMessagesToStateStore(ctx, req, messages); err != nil {
				return err
			}
			// Signal completion on turn 3 (>= min turns of 2)
			if selfPlayCallCount == 3 {
				return selfplay.ErrConversationComplete
			}
			return nil
		},
	}

	selfPlayRegistry := createTestSelfPlayRegistry(t)
	executor := NewDefaultConversationExecutor(
		scriptedExecutor, selfPlayExecutor, selfPlayRegistry,
		createTestPromptRegistry(t), nil,
	)

	scenario := &config.Scenario{
		ID:       "natural-term",
		TaskType: "assistant",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Start"},
			{Role: "attacker", Persona: "attacker", Turns: 2, MaxTurns: 10},
		},
	}

	req := ConversationRequest{
		Region:           "default",
		Scenario:         scenario,
		Provider:         &MockProvider{id: "test"},
		Config:           &config.Config{},
		StateStoreConfig: &StateStoreConfig{Store: createTestStateStore(), UserID: "test-user"},
		ConversationID:   "test-natural-term",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result.Failed {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Should stop at turn 3 (completion detected and >= minTurns=2)
	if selfPlayCallCount != 3 {
		t.Errorf("Expected 3 self-play turns (natural termination), got %d", selfPlayCallCount)
	}

	// 2 initial + 3*2 self-play = 8 messages
	if len(result.Messages) != 8 {
		t.Errorf("Expected 8 messages, got %d", len(result.Messages))
	}
}

// TestNaturalTermination_IgnoredBelowMinTurns verifies that ErrConversationComplete
// is ignored (marker stripped, loop continues) when below the minimum turn count.
func TestNaturalTermination_IgnoredBelowMinTurns(t *testing.T) {
	selfPlayCallCount := 0

	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			messages := []types.Message{
				{Role: "user", Content: req.ScriptedContent},
				{Role: "assistant", Content: "Response"},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			selfPlayCallCount++
			messages := []types.Message{
				{Role: "user", Content: "Question"},
				{Role: "assistant", Content: "Answer"},
			}
			if err := saveMessagesToStateStore(ctx, req, messages); err != nil {
				return err
			}
			// Signal completion on turn 1, but min is 3 — should be ignored
			if selfPlayCallCount == 1 {
				return selfplay.ErrConversationComplete
			}
			// Signal again on turn 3, which is at min — should be accepted
			if selfPlayCallCount == 3 {
				return selfplay.ErrConversationComplete
			}
			return nil
		},
	}

	selfPlayRegistry := createTestSelfPlayRegistry(t)
	executor := NewDefaultConversationExecutor(
		scriptedExecutor, selfPlayExecutor, selfPlayRegistry,
		createTestPromptRegistry(t), nil,
	)

	scenario := &config.Scenario{
		ID:       "min-enforcement",
		TaskType: "assistant",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Start"},
			{Role: "attacker", Persona: "attacker", Turns: 3, MaxTurns: 10},
		},
	}

	req := ConversationRequest{
		Region:           "default",
		Scenario:         scenario,
		Provider:         &MockProvider{id: "test"},
		Config:           &config.Config{},
		StateStoreConfig: &StateStoreConfig{Store: createTestStateStore(), UserID: "test-user"},
		ConversationID:   "test-min-enforcement",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result.Failed {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Turn 1: completion ignored (below min=3), turns 2-3 execute, turn 3 completion accepted
	if selfPlayCallCount != 3 {
		t.Errorf("Expected 3 self-play turns (min enforced), got %d", selfPlayCallCount)
	}
}

// TestNaturalTermination_ExactTurnsWithoutMaxTurns verifies backward compatibility:
// turns=5 without max_turns executes exactly 5 turns with no completion detection.
func TestNaturalTermination_ExactTurnsWithoutMaxTurns(t *testing.T) {
	selfPlayCallCount := 0
	metadataChecked := false

	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			messages := []types.Message{
				{Role: "user", Content: req.ScriptedContent},
				{Role: "assistant", Content: "Response"},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			selfPlayCallCount++
			// Verify natural_termination_enabled is NOT set
			if !metadataChecked {
				if nt, ok := req.Metadata["natural_termination_enabled"].(bool); ok && nt {
					t.Error("natural_termination_enabled should not be set when max_turns is absent")
				}
				metadataChecked = true
			}
			messages := []types.Message{
				{Role: "user", Content: "Question"},
				{Role: "assistant", Content: "Answer"},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayRegistry := createTestSelfPlayRegistry(t)
	executor := NewDefaultConversationExecutor(
		scriptedExecutor, selfPlayExecutor, selfPlayRegistry,
		createTestPromptRegistry(t), nil,
	)

	scenario := &config.Scenario{
		ID:       "exact-turns",
		TaskType: "assistant",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Start"},
			{Role: "attacker", Persona: "attacker", Turns: 5}, // No MaxTurns
		},
	}

	req := ConversationRequest{
		Region:           "default",
		Scenario:         scenario,
		Provider:         &MockProvider{id: "test"},
		Config:           &config.Config{},
		StateStoreConfig: &StateStoreConfig{Store: createTestStateStore(), UserID: "test-user"},
		ConversationID:   "test-exact-turns",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result.Failed {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Exactly 5 turns — backward compatible
	if selfPlayCallCount != 5 {
		t.Errorf("Expected exactly 5 self-play turns, got %d", selfPlayCallCount)
	}
}

// TestNaturalTermination_MaxTurnsReachedWithoutCompletion verifies that if the LLM
// never signals completion, the loop runs exactly max_turns times.
func TestNaturalTermination_MaxTurnsReachedWithoutCompletion(t *testing.T) {
	selfPlayCallCount := 0

	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			messages := []types.Message{
				{Role: "user", Content: req.ScriptedContent},
				{Role: "assistant", Content: "Response"},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			selfPlayCallCount++
			messages := []types.Message{
				{Role: "user", Content: "Question"},
				{Role: "assistant", Content: "Answer"},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayRegistry := createTestSelfPlayRegistry(t)
	executor := NewDefaultConversationExecutor(
		scriptedExecutor, selfPlayExecutor, selfPlayRegistry,
		createTestPromptRegistry(t), nil,
	)

	scenario := &config.Scenario{
		ID:       "max-turns-cap",
		TaskType: "assistant",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Start"},
			{Role: "attacker", Persona: "attacker", Turns: 2, MaxTurns: 5},
		},
	}

	req := ConversationRequest{
		Region:           "default",
		Scenario:         scenario,
		Provider:         &MockProvider{id: "test"},
		Config:           &config.Config{},
		StateStoreConfig: &StateStoreConfig{Store: createTestStateStore(), UserID: "test-user"},
		ConversationID:   "test-max-turns-cap",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result.Failed {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// No completion signal — should run all max_turns
	if selfPlayCallCount != 5 {
		t.Errorf("Expected 5 self-play turns (max_turns cap), got %d", selfPlayCallCount)
	}
}

// TestNaturalTermination_MetadataFlag verifies that the natural_termination_enabled
// metadata flag is set on the TurnRequest when max_turns > turns.
func TestNaturalTermination_MetadataFlag(t *testing.T) {
	natTermSeen := false

	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			messages := []types.Message{
				{Role: "user", Content: req.ScriptedContent},
				{Role: "assistant", Content: "Response"},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			if nt, ok := req.Metadata["natural_termination_enabled"].(bool); ok && nt {
				natTermSeen = true
			}
			messages := []types.Message{
				{Role: "user", Content: "Q"},
				{Role: "assistant", Content: "A"},
			}
			if err := saveMessagesToStateStore(ctx, req, messages); err != nil {
				return err
			}
			return selfplay.ErrConversationComplete
		},
	}

	selfPlayRegistry := createTestSelfPlayRegistry(t)
	executor := NewDefaultConversationExecutor(
		scriptedExecutor, selfPlayExecutor, selfPlayRegistry,
		createTestPromptRegistry(t), nil,
	)

	scenario := &config.Scenario{
		ID:       "metadata-flag",
		TaskType: "assistant",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Start"},
			{Role: "attacker", Persona: "attacker", Turns: 1, MaxTurns: 5},
		},
	}

	req := ConversationRequest{
		Region:           "default",
		Scenario:         scenario,
		Provider:         &MockProvider{id: "test"},
		Config:           &config.Config{},
		StateStoreConfig: &StateStoreConfig{Store: createTestStateStore(), UserID: "test-user"},
		ConversationID:   "test-metadata-flag",
	}

	executor.ExecuteConversation(context.Background(), req)

	if !natTermSeen {
		t.Error("Expected natural_termination_enabled metadata to be set on TurnRequest")
	}
}
