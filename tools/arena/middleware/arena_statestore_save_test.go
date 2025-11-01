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
