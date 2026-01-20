package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/adapters"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestEngine_ExecuteEvalRun_Success(t *testing.T) {
	ctx := context.Background()

	// Create adapter registry with mock adapter
	adapterRegistry := adapters.NewRegistry()
	mockAdapter := &mockRecordingAdapter{
		messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
		metadata: &adapters.RecordingMetadata{
			SessionID: "test-session",
		},
	}
	adapterRegistry.Register(mockAdapter)

	// Create eval conversation executor
	evalExecutor := NewEvalConversationExecutor(
		adapterRegistry,
		nil, // assertion registry not needed for this test
		assertions.NewConversationAssertionRegistry(),
		nil, // prompt registry not needed
		providers.NewRegistry(),
	)

	e := &Engine{
		config: &config.Config{},
		evals: map[string]*config.Eval{
			"test-eval": {
				ID: "test-eval",
				Recording: config.RecordingSource{
					Path: "test.json",
					Type: "mock",
				},
			},
		},
		conversationExecutor: evalExecutor,
		stateStore:           statestore.NewArenaStateStore(),
		eventBus:             events.NewEventBus(),
	}

	combo := RunCombination{
		EvalID: "test-eval",
	}

	runID, err := e.executeRun(ctx, combo)
	require.NoError(t, err)
	assert.NotEmpty(t, runID)
	assert.Contains(t, runID, "eval")

	// Verify metadata was saved
	result, err := e.stateStore.(*statestore.ArenaStateStore).GetRunResult(ctx, runID)
	require.NoError(t, err)
	assert.Equal(t, "test-eval", result.ScenarioID) // EvalID stored in ScenarioID field
	assert.Equal(t, "eval", result.ProviderID)
	assert.Empty(t, result.Error)
}

func TestEvalConversationExecutor_ApplyAllTurnAssertions(t *testing.T) {
	executor := &EvalConversationExecutor{}

	turns := []config.EvalTurnConfig{
		{
			AllTurns: &config.EvalAllTurnsConfig{
				Assertions: []assertions.AssertionConfig{
					{Type: "test_assertion"},
				},
			},
		},
	}

	messages := []types.Message{
		{Role: "user", Content: "Test"},
		{Role: "assistant", Content: "Hello"},
		{Role: "assistant", Content: "Goodbye"},
	}

	convCtx := &assertions.ConversationContext{
		AllTurns: messages,
		Metadata: assertions.ConversationMetadata{
			Extras: make(map[string]interface{}),
		},
	}

	// This tests that the function runs without error
	executor.applyAllTurnAssertions(turns, messages, convCtx)
}

func TestEvalConversationExecutor_ExtractTurnAssertions(t *testing.T) {
	executor := &EvalConversationExecutor{}

	t.Run("extracts assertions from all turns", func(t *testing.T) {
		turns := []config.EvalTurnConfig{
			{
				AllTurns: &config.EvalAllTurnsConfig{
					Assertions: []assertions.AssertionConfig{
						{Type: "assertion1"},
						{Type: "assertion2"},
					},
				},
			},
			{
				AllTurns: &config.EvalAllTurnsConfig{
					Assertions: []assertions.AssertionConfig{
						{Type: "assertion3"},
					},
				},
			},
		}

		result := executor.extractTurnAssertions(turns)
		assert.Len(t, result, 3)
		assert.Equal(t, "assertion1", result[0].Type)
		assert.Equal(t, "assertion2", result[1].Type)
		assert.Equal(t, "assertion3", result[2].Type)
	})

	t.Run("handles turns without all_turns", func(t *testing.T) {
		turns := []config.EvalTurnConfig{
			{
				AllTurns: nil,
			},
		}

		result := executor.extractTurnAssertions(turns)
		assert.Empty(t, result)
	})

	t.Run("handles empty assertions", func(t *testing.T) {
		turns := []config.EvalTurnConfig{
			{
				AllTurns: &config.EvalAllTurnsConfig{
					Assertions: []assertions.AssertionConfig{},
				},
			},
		}

		result := executor.extractTurnAssertions(turns)
		assert.Empty(t, result)
	})
}

func TestEvalConversationExecutor_EvaluateConversationAssertions(t *testing.T) {
	ctx := context.Background()

	convAssertionReg := assertions.NewConversationAssertionRegistry()
	convAssertionReg.Register("test_assertion", func() assertions.ConversationValidator {
		return &mockConversationAssertion{
			evaluateFunc: func(ctx context.Context, convCtx *assertions.ConversationContext, params map[string]interface{}) assertions.ConversationValidationResult {
				return assertions.ConversationValidationResult{
					Type:   "test_assertion",
					Passed: true,
				}
			},
		}
	})

	executor := &EvalConversationExecutor{
		convAssertionReg: convAssertionReg,
	}

	t.Run("evaluates conversation assertions", func(t *testing.T) {
		assertionConfigs := []assertions.AssertionConfig{
			{
				Type:   "test_assertion",
				Params: map[string]interface{}{},
			},
		}

		convCtx := &assertions.ConversationContext{
			AllTurns: []types.Message{{Role: "user", Content: "test"}},
			Metadata: assertions.ConversationMetadata{
				Extras: make(map[string]interface{}),
			},
		}

		results := executor.evaluateConversationAssertions(ctx, assertionConfigs, convCtx)
		assert.Len(t, results, 1)
		assert.True(t, results[0].Passed)
		assert.Equal(t, "test_assertion", results[0].Type)
	})

	t.Run("returns nil when no registry", func(t *testing.T) {
		executor := &EvalConversationExecutor{
			convAssertionReg: nil,
		}

		results := executor.evaluateConversationAssertions(ctx, []assertions.AssertionConfig{
			{Type: "test"},
		}, &assertions.ConversationContext{})

		assert.Nil(t, results)
	})

	t.Run("returns nil when no assertions", func(t *testing.T) {
		results := executor.evaluateConversationAssertions(ctx, []assertions.AssertionConfig{}, &assertions.ConversationContext{})
		assert.Nil(t, results)
	})
}

func TestEvalConversationExecutor_BuildConversationContext_WithMetadata(t *testing.T) {
	executor := &EvalConversationExecutor{}

	req := &ConversationRequest{
		Eval: &config.Eval{
			ID:   "test-eval",
			Tags: []string{"eval-tag"},
		},
		Config: &config.Config{
			ProviderGroups: map[string]string{},
		},
	}

	messages := []types.Message{
		{Role: "user", Content: "Hello"},
	}

	metadata := &adapters.RecordingMetadata{
		SessionID: "session-123",
		ProviderInfo: map[string]interface{}{
			"model": "gpt-4",
		},
		Tags: []string{"recording-tag"},
		Extras: map[string]interface{}{
			"custom_field": "value",
		},
	}

	ctx := executor.buildConversationContext(req, messages, metadata)

	assert.Equal(t, messages, ctx.AllTurns)
	assert.Equal(t, "test-eval", ctx.Metadata.Extras["eval_id"])
	assert.Equal(t, "session-123", ctx.Metadata.Extras["session_id"])
	assert.Equal(t, metadata.ProviderInfo, ctx.Metadata.Extras["provider_info"])
	assert.Equal(t, "value", ctx.Metadata.Extras["custom_field"])

	// Check tags are merged
	tags := ctx.Metadata.Extras["tags"].([]string)
	assert.Contains(t, tags, "eval-tag")
	assert.Contains(t, tags, "recording-tag")
}

func TestEvalConversationExecutor_MessageHasFailedAssertions(t *testing.T) {
	executor := &EvalConversationExecutor{}

	t.Run("returns true when assertion failed", func(t *testing.T) {
		msg := &types.Message{
			Meta: map[string]interface{}{
				"assertions": []assertions.AssertionResult{
					{Passed: true},
					{Passed: false},
				},
			},
		}
		assert.True(t, executor.messageHasFailedAssertions(msg))
	})

	t.Run("returns false when all pass", func(t *testing.T) {
		msg := &types.Message{
			Meta: map[string]interface{}{
				"assertions": []assertions.AssertionResult{
					{Passed: true},
					{Passed: true},
				},
			},
		}
		assert.False(t, executor.messageHasFailedAssertions(msg))
	})

	t.Run("returns false when no assertions", func(t *testing.T) {
		msg := &types.Message{
			Meta: map[string]interface{}{},
		}
		assert.False(t, executor.messageHasFailedAssertions(msg))
	})
}

// Mock types for testing

type mockRecordingAdapter struct {
	messages []types.Message
	metadata *adapters.RecordingMetadata
}

func (m *mockRecordingAdapter) CanHandle(source, recordingType string) bool {
	return recordingType == "mock"
}

func (m *mockRecordingAdapter) Enumerate(source string) ([]adapters.RecordingReference, error) {
	return []adapters.RecordingReference{
		{ID: source, Source: source, TypeHint: "mock"},
	}, nil
}

func (m *mockRecordingAdapter) Load(ref adapters.RecordingReference) ([]types.Message, *adapters.RecordingMetadata, error) {
	return m.messages, m.metadata, nil
}

type mockConversationAssertion struct {
	evaluateFunc func(ctx context.Context, convCtx *assertions.ConversationContext, params map[string]interface{}) assertions.ConversationValidationResult
}

func (m *mockConversationAssertion) Type() string {
	return "test_assertion"
}

func (m *mockConversationAssertion) ValidateConversation(ctx context.Context, convCtx *assertions.ConversationContext, params map[string]interface{}) assertions.ConversationValidationResult {
	return m.evaluateFunc(ctx, convCtx, params)
}
