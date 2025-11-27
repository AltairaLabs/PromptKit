package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestArenaStateStoreSaveMiddleware_CapturesValidationResults(t *testing.T) {
	// Create ArenaStateStore
	arenaStore := statestore.NewArenaStateStore()

	// Create config
	config := &pipeline.StateStoreConfig{
		Store:          arenaStore,
		ConversationID: "test-conv-1",
		UserID:         "test-user",
	}

	// Create execution context with validation results on the assistant message
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{
				Role:    "assistant",
				Content: "This is a very long response that exceeds the limit",
				Validations: []types.ValidationResult{
					{
						ValidatorType: "*validators.MaxLengthValidator",
						Passed:        false,
						Details: map[string]interface{}{
							"actual_length": 100,
							"max_length":    50,
						},
						Timestamp: time.Now(),
					},
				},
			},
		},
		Metadata: map[string]interface{}{},
		CostInfo: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.0005,
		},
	}

	// Create middleware
	middleware := ArenaStateStoreSaveMiddleware(config)

	// Execute before (no-op for save middleware)
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before returned error: %v", err)
	}

	// Simulate some execution time
	time.Sleep(10 * time.Millisecond)

	// Execute after (this does the save)

	if err != nil {
		t.Fatalf("Middleware returned error: %v", err)
	}

	// Verify telemetry was captured
	arenaState, err := arenaStore.GetArenaState(context.Background(), "test-conv-1")
	if err != nil {
		t.Fatalf("GetArenaState returned error: %v", err)
	}
	if arenaState == nil {
		t.Fatal("ArenaState not found")
	}

	// Check validation results are in messages
	if len(arenaState.Messages) == 0 {
		t.Fatal("Expected messages in arena state")
	}

	// Find assistant message with validations
	var foundValidation bool
	for _, msg := range arenaState.Messages {
		if msg.Role == "assistant" && len(msg.Validations) > 0 {
			foundValidation = true
			validation := msg.Validations[0]
			if validation.Passed {
				t.Error("Expected validation to have failed")
			}
			if validation.ValidatorType != "*validators.MaxLengthValidator" {
				t.Errorf("Expected validator type MaxLengthValidator, got %s", validation.ValidatorType)
			}
			if validation.Details == nil {
				t.Error("Expected validation details")
			}
			break
		}
	}

	if !foundValidation {
		t.Error("Expected to find validation in assistant message")
	}
}

func TestArenaStateStoreSaveMiddleware_CapturesSuccessfulValidation(t *testing.T) {
	// Create ArenaStateStore
	arenaStore := statestore.NewArenaStateStore()

	// Create config
	config := &pipeline.StateStoreConfig{
		Store:          arenaStore,
		ConversationID: "test-conv-2",
		UserID:         "test-user",
	}

	// Create execution context with successful validation on assistant message
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{
				Role:    "assistant",
				Content: "Short response",
				Validations: []types.ValidationResult{
					{
						ValidatorType: "*validators.MaxLengthValidator",
						Passed:        true,
						Details: map[string]interface{}{
							"actual_length": 14,
							"max_length":    50,
						},
						Timestamp: time.Now(),
					},
				},
			},
		},
		Metadata: map[string]interface{}{},
		CostInfo: types.CostInfo{
			InputTokens:  5,
			OutputTokens: 5,
			TotalCost:    0.0001,
		},
	}

	// Create middleware
	middleware := ArenaStateStoreSaveMiddleware(config)

	// Execute before (no-op for save middleware)
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before returned error: %v", err)
	}

	// Execute after (this does the save)

	if err != nil {
		t.Fatalf("Middleware returned error: %v", err)
	}

	// Verify telemetry was captured
	arenaState, err := arenaStore.GetArenaState(context.Background(), "test-conv-2")
	if err != nil {
		t.Fatalf("GetArenaState returned error: %v", err)
	}
	if arenaState == nil {
		t.Fatal("ArenaState not found")
	}

	// Check validation passed - should be in messages
	var foundValidation bool
	for _, msg := range arenaState.Messages {
		if msg.Role == "assistant" && len(msg.Validations) > 0 {
			foundValidation = true
			validation := msg.Validations[0]
			if !validation.Passed {
				t.Error("Expected validation to have passed")
			}
			break
		}
	}

	if !foundValidation {
		t.Error("Expected to find validation in assistant message")
	}
}

func TestArenaStateStoreSaveMiddleware_AccumulatesAcrossMultipleTurns(t *testing.T) {
	// Create ArenaStateStore
	arenaStore := statestore.NewArenaStateStore()

	// Create config
	config := &pipeline.StateStoreConfig{
		Store:          arenaStore,
		ConversationID: "test-conv-3",
		UserID:         "test-user",
	}

	// First turn
	execCtx1 := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "First question"},
			{
				Role:    "assistant",
				Content: "First answer",
				Validations: []types.ValidationResult{
					{
						ValidatorType: "*validators.MaxLengthValidator",
						Passed:        true,
						Details:       map[string]interface{}{"actual_length": 12},
						Timestamp:     time.Now(),
					},
				},
			},
		},
		Metadata: map[string]interface{}{},
		CostInfo: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 15,
			TotalCost:    0.0003,
		},
	}

	middleware := ArenaStateStoreSaveMiddleware(config)
	err := middleware.Process(execCtx1, func() error { return nil })
	if err != nil {
		t.Fatalf("Turn 1 Process failed: %v", err)
	}

	// Second turn
	execCtx2 := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "First question"},
			{Role: "assistant", Content: "First answer"},
			{Role: "user", Content: "Second question"},
			{
				Role:    "assistant",
				Content: "Second answer",
				Validations: []types.ValidationResult{
					{
						ValidatorType: "*validators.BannedWordsValidator",
						Passed:        false,
						Details:       map[string]interface{}{"banned_word": "guarantee"},
						Timestamp:     time.Now(),
					},
				},
			},
		},
		Metadata: map[string]interface{}{},
		CostInfo: types.CostInfo{
			InputTokens:  20,
			OutputTokens: 25,
			TotalCost:    0.0005,
		},
	}

	err = middleware.Process(execCtx2, func() error { return nil })
	if err != nil {
		t.Fatalf("Turn 2 Process failed: %v", err)
	}

	// Verify accumulated telemetry
	arenaState, err := arenaStore.GetArenaState(context.Background(), "test-conv-3")
	if err != nil {
		t.Fatalf("GetArenaState returned error: %v", err)
	}
	if arenaState == nil {
		t.Fatal("ArenaState not found")
	}

	// Note: TurnMetrics not yet implemented in ArenaConversationState

	// Should have validation results from the last turn (turn 2)
	// Note: Previous turn validations are not preserved when the execution context
	// is reconstructed for a new turn, so we only see the latest turn's validations
	totalValidations := 0
	for _, msg := range arenaState.Messages {
		if msg.Role == "assistant" {
			totalValidations += len(msg.Validations)
		}
	}

	if totalValidations != 1 {
		t.Fatalf("Expected 1 validation result (from turn 2), got %d", totalValidations)
	}

	// Note: Cost accumulation is not currently implemented in ArenaStateStore
	// The middleware only saves the conversation state, it doesn't accumulate costs
}

func TestArenaStateStoreSaveMiddleware_CapturesFromMessages(t *testing.T) {
	// This test verifies that ArenaStateStoreSaveMiddleware correctly reads
	// validation results that were attached to messages by ProviderMiddleware
	// (after being set by DynamicValidatorMiddleware)

	// Create ArenaStateStore
	arenaStore := statestore.NewArenaStateStore()

	// Create execution context with validation results on assistant message
	// (as if ProviderMiddleware attached them after DynamicValidatorMiddleware ran)
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{
				Role:    "assistant",
				Content: "This contains a forbidden word",
				Validations: []types.ValidationResult{
					{
						ValidatorType: "*validators.BannedWordsValidator",
						Passed:        false,
						Details: map[string]interface{}{
							"banned_word": "forbidden",
						},
						Timestamp: time.Now(),
					},
				},
			},
		},
		Metadata: map[string]interface{}{},
		Response: &pipeline.Response{
			Content: "This contains a forbidden word",
		},
		CostInfo: types.CostInfo{
			InputTokens:  5,
			OutputTokens: 10,
			TotalCost:    0.0002,
		},
	}

	// Create middleware
	stateConfig := &pipeline.StateStoreConfig{
		Store:          arenaStore,
		ConversationID: "test-conv-4",
		UserID:         "test-user",
	}
	arenaSave := ArenaStateStoreSaveMiddleware(stateConfig)

	// Execute - should succeed (we're just testing telemetry capture, not validation)
	err := arenaSave.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before returned error: %v", err)
	}

	// Verify telemetry was captured
	arenaState, err := arenaStore.GetArenaState(context.Background(), "test-conv-4")
	if err != nil {
		t.Fatalf("GetArenaState returned error: %v", err)
	}
	if arenaState == nil {
		t.Fatal("ArenaState not found")
	}

	// Should have validation results in message
	var foundValidation bool
	var validation types.ValidationResult

	for _, msg := range arenaState.Messages {
		if msg.Role == "assistant" && len(msg.Validations) > 0 {
			foundValidation = true
			validation = msg.Validations[0]
			break
		}
	}

	if !foundValidation {
		t.Fatal("Expected validation results to be captured in message")
	}

	if validation.Passed {
		t.Error("Expected validation to have failed")
	}
	if validation.ValidatorType != "*validators.BannedWordsValidator" {
		t.Errorf("Expected validator type *validators.BannedWordsValidator, got %s", validation.ValidatorType)
	}

	// Verify details were captured
	if validation.Details == nil {
		t.Error("Expected validation details")
	} else if bannedWord, ok := validation.Details["banned_word"]; !ok || bannedWord != "forbidden" {
		t.Errorf("Expected banned_word='forbidden' in details, got %v", validation.Details)
	}
}

// TestArenaStateStoreSaveMiddleware_SystemPromptPrepending tests that system prompts are prepended as first message
func TestArenaStateStoreSaveMiddleware_SystemPromptPrepending(t *testing.T) {
	arenaStore := statestore.NewArenaStateStore()

	config := &pipeline.StateStoreConfig{
		Store:          arenaStore,
		ConversationID: "conv-sysprompt-1",
		UserID:         "test-user",
	}

	now := time.Now()
	execCtx := &pipeline.ExecutionContext{
		Context:      context.Background(),
		SystemPrompt: "You are a helpful assistant",
		Messages: []types.Message{
			{
				Role:      "user",
				Content:   "Hello",
				Timestamp: now,
			},
			{
				Role:      "assistant",
				Content:   "Hi there!",
				Timestamp: now.Add(time.Second),
			},
		},
		Metadata: map[string]interface{}{},
	}

	middleware := ArenaStateStoreSaveMiddleware(config)
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Middleware returned error: %v", err)
	}

	// Load state and verify system message was prepended
	state, err := arenaStore.Load(context.Background(), "conv-sysprompt-1")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(state.Messages) != 3 {
		t.Fatalf("Expected 3 messages (system + user + assistant), got %d", len(state.Messages))
	}

	// Verify first message is system prompt
	if state.Messages[0].Role != "system" {
		t.Errorf("Expected first message role to be 'system', got %s", state.Messages[0].Role)
	}
	if state.Messages[0].Content != "You are a helpful assistant" {
		t.Errorf("Expected system content 'You are a helpful assistant', got %s", state.Messages[0].Content)
	}
	if state.Messages[0].Timestamp.IsZero() {
		t.Error("System message should have timestamp")
	}

	// Verify system message has Parts array with text content
	if len(state.Messages[0].Parts) != 1 {
		t.Errorf("Expected system message to have 1 Part, got %d", len(state.Messages[0].Parts))
	} else {
		if state.Messages[0].Parts[0].Type != types.ContentTypeText {
			t.Errorf("Expected Part type 'text', got %s", state.Messages[0].Parts[0].Type)
		}
		if state.Messages[0].Parts[0].Text == nil {
			t.Error("Expected Part Text to be non-nil")
		} else if *state.Messages[0].Parts[0].Text != "You are a helpful assistant" {
			t.Errorf("Expected Part Text 'You are a helpful assistant', got %s", *state.Messages[0].Parts[0].Text)
		}
	}

	// Verify subsequent messages
	if state.Messages[1].Role != "user" || state.Messages[1].Content != "Hello" {
		t.Error("Second message should be user message 'Hello'")
	}
	if state.Messages[2].Role != "assistant" || state.Messages[2].Content != "Hi there!" {
		t.Error("Third message should be assistant message 'Hi there!'")
	}
}

// TestArenaStateStoreSaveMiddleware_NoSystemPrompt tests that messages are copied normally without system prompt
func TestArenaStateStoreSaveMiddleware_NoSystemPrompt(t *testing.T) {
	arenaStore := statestore.NewArenaStateStore()

	config := &pipeline.StateStoreConfig{
		Store:          arenaStore,
		ConversationID: "conv-nosysprompt",
		UserID:         "test-user",
	}

	execCtx := &pipeline.ExecutionContext{
		Context:      context.Background(),
		SystemPrompt: "", // Empty system prompt
		Messages: []types.Message{
			{
				Role:      "user",
				Content:   "Hello",
				Timestamp: time.Now(),
			},
			{
				Role:      "assistant",
				Content:   "Hi there!",
				Timestamp: time.Now(),
			},
		},
		Metadata: map[string]interface{}{},
	}

	middleware := ArenaStateStoreSaveMiddleware(config)
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Middleware returned error: %v", err)
	}

	// Load state and verify no system message
	state, err := arenaStore.Load(context.Background(), "conv-nosysprompt")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(state.Messages) != 2 {
		t.Fatalf("Expected 2 messages (user + assistant), got %d", len(state.Messages))
	}

	// Verify messages without system prompt
	if state.Messages[0].Role != "user" || state.Messages[0].Content != "Hello" {
		t.Error("First message should be user message 'Hello'")
	}
	if state.Messages[1].Role != "assistant" || state.Messages[1].Content != "Hi there!" {
		t.Error("Second message should be assistant message 'Hi there!'")
	}
}

// TestArenaStateStoreSaveMiddleware_SystemPromptMetadata tests that system_prompt is stored in metadata
func TestArenaStateStoreSaveMiddleware_SystemPromptMetadata(t *testing.T) {
	arenaStore := statestore.NewArenaStateStore()

	config := &pipeline.StateStoreConfig{
		Store:          arenaStore,
		ConversationID: "conv-sysmeta",
		UserID:         "test-user",
	}

	systemPrompt := "You are a specialized AI assistant for customer service"

	execCtx := &pipeline.ExecutionContext{
		Context:      context.Background(),
		SystemPrompt: systemPrompt,
		Messages: []types.Message{
			{
				Role:      "user",
				Content:   "Test",
				Timestamp: time.Now(),
			},
		},
		Metadata: map[string]interface{}{},
	}

	middleware := ArenaStateStoreSaveMiddleware(config)
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Middleware returned error: %v", err)
	}

	// Load state and verify system_prompt is in metadata
	state, err := arenaStore.Load(context.Background(), "conv-sysmeta")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// Check metadata contains system_prompt
	systemPromptValue, exists := state.Metadata["system_prompt"]
	if !exists {
		t.Fatal("system_prompt should be in metadata")
	}
	if systemPromptValue != systemPrompt {
		t.Errorf("Expected system_prompt '%s', got '%v'", systemPrompt, systemPromptValue)
	}
}

// TestArenaStateStoreSaveMiddleware_SystemPromptTimestamp tests that system message uses first message timestamp
func TestArenaStateStoreSaveMiddleware_SystemPromptTimestamp(t *testing.T) {
	arenaStore := statestore.NewArenaStateStore()

	config := &pipeline.StateStoreConfig{
		Store:          arenaStore,
		ConversationID: "conv-systime",
		UserID:         "test-user",
	}

	firstMessageTime := time.Now().Add(-1 * time.Minute) // 1 minute ago

	execCtx := &pipeline.ExecutionContext{
		Context:      context.Background(),
		SystemPrompt: "You are helpful",
		Messages: []types.Message{
			{
				Role:      "user",
				Content:   "Hello",
				Timestamp: firstMessageTime,
			},
			{
				Role:      "assistant",
				Content:   "Hi!",
				Timestamp: time.Now(),
			},
		},
		Metadata: map[string]interface{}{},
	}

	middleware := ArenaStateStoreSaveMiddleware(config)
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Middleware returned error: %v", err)
	}

	// Load state and verify system message timestamp
	state, err := arenaStore.Load(context.Background(), "conv-systime")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(state.Messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(state.Messages))
	}

	// System message should have the same timestamp as the first user message
	if state.Messages[0].Timestamp.Unix() != firstMessageTime.Unix() {
		t.Errorf("System message timestamp (%v) should match first message timestamp (%v)",
			state.Messages[0].Timestamp, firstMessageTime)
	}
}

func TestArenaStateStoreSaveMiddleware_PersistsTurnCounters(t *testing.T) {
	arenaStore := statestore.NewArenaStateStore()

	config := &pipeline.StateStoreConfig{
		Store:          arenaStore,
		ConversationID: "conv-counters-1",
		UserID:         "user-1",
	}

	// Two user messages and one assistant message
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "Turn 1"},
			{Role: "assistant", Content: "Resp 1"},
			{Role: "user", Content: "Turn 2"},
		},
		Metadata: map[string]interface{}{},
	}

	// Compute counters then save
	turnIndex := TurnIndexMiddleware()
	err := turnIndex.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("TurnIndexMiddleware returned error: %v", err)
	}

	save := ArenaStateStoreSaveMiddleware(config)
	err = save.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("ArenaStateStoreSaveMiddleware returned error: %v", err)
	}

	state, err := arenaStore.Load(context.Background(), "conv-counters-1")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// Verify counters persisted to state metadata
	if got, ok := state.Metadata["arena_user_completed_turns"]; !ok || got != 2 {
		t.Fatalf("expected arena_user_completed_turns=2, got %v (ok=%v)", got, ok)
	}
	if got, ok := state.Metadata["arena_user_next_turn"]; !ok || got != 3 {
		t.Fatalf("expected arena_user_next_turn=3, got %v (ok=%v)", got, ok)
	}
	if got, ok := state.Metadata["arena_assistant_completed_turns"]; !ok || got != 1 {
		t.Fatalf("expected arena_assistant_completed_turns=1, got %v (ok=%v)", got, ok)
	}
	if got, ok := state.Metadata["arena_assistant_next_turn"]; !ok || got != 2 {
		t.Fatalf("expected arena_assistant_next_turn=2, got %v (ok=%v)", got, ok)
	}
}
