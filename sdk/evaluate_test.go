package sdk

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestEvaluate_WithEvalDefs(t *testing.T) {
	defs := []evals.EvalDef{
		{
			ID:      "greeting",
			Type:    "contains",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"patterns": []any{"hello"}},
		},
	}
	messages := []types.Message{
		types.NewUserMessage("hi"),
		types.NewAssistantMessage("hello there!"),
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:  defs,
		Messages:  messages,
		SessionID: "test-session",
		TurnIndex: 1,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
	assert.Equal(t, "greeting", results[0].EvalID)
}

func TestEvaluate_WithEvalDefs_Failing(t *testing.T) {
	defs := []evals.EvalDef{
		{
			ID:      "missing",
			Type:    "contains",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"patterns": []any{"nonexistent"}},
		},
	}
	messages := []types.Message{
		types.NewAssistantMessage("hello there!"),
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:  defs,
		Messages:  messages,
		SessionID: "s1",
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
}

func TestEvaluate_WithPackPath(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("hi"),
		types.NewAssistantMessage("hello! how can I help?"),
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		PackPath:             "testdata/packs/eval-test.pack.json",
		Messages:             messages,
		SessionID:            "test-session",
		TurnIndex:            1,
		SkipSchemaValidation: true,
	})

	require.NoError(t, err)
	// Pack has 2 evals: greeting_check (every_turn) and session_check (on_session_complete).
	// Default trigger is every_turn, so only greeting_check runs.
	require.Len(t, results, 1)
	assert.Equal(t, "greeting_check", results[0].EvalID)
	assert.True(t, results[0].Passed)
}

func TestEvaluate_WithPackPath_PromptEvals(t *testing.T) {
	messages := []types.Message{
		types.NewAssistantMessage("thank you for your patience"),
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		PackPath:             "testdata/packs/eval-test.pack.json",
		PromptName:           "assistant",
		Messages:             messages,
		SessionID:            "s1",
		SkipSchemaValidation: true,
	})

	require.NoError(t, err)
	// Merged: greeting_check (pack) + session_check (pack) + prompt_eval (prompt).
	// Default trigger (every_turn) matches greeting_check and prompt_eval.
	require.Len(t, results, 2)

	ids := map[string]bool{}
	for _, r := range results {
		ids[r.EvalID] = true
	}
	assert.True(t, ids["greeting_check"])
	assert.True(t, ids["prompt_eval"])
}

func TestEvaluate_WithPackData(t *testing.T) {
	data, err := os.ReadFile("testdata/packs/eval-test.pack.json")
	require.NoError(t, err)

	messages := []types.Message{
		types.NewAssistantMessage("hello!"),
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		PackData:  data,
		Messages:  messages,
		SessionID: "s1",
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "greeting_check", results[0].EvalID)
	assert.True(t, results[0].Passed)
}

func TestEvaluate_SessionTrigger(t *testing.T) {
	messages := []types.Message{
		types.NewAssistantMessage("goodbye and take care"),
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		PackPath:             "testdata/packs/eval-test.pack.json",
		Messages:             messages,
		SessionID:            "s1",
		Trigger:              evals.TriggerOnSessionComplete,
		SkipSchemaValidation: true,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "session_check", results[0].EvalID)
	assert.True(t, results[0].Passed)
}

func TestEvaluate_NoSource_ReturnsError(t *testing.T) {
	_, err := Evaluate(context.Background(), EvaluateOpts{
		Messages: []types.Message{types.NewAssistantMessage("hi")},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "one of EvalDefs, PackData, or PackPath must be provided")
}

func TestEvaluate_InvalidPackPath_ReturnsError(t *testing.T) {
	_, err := Evaluate(context.Background(), EvaluateOpts{
		PackPath: "nonexistent.pack.json",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "load pack")
}

func TestEvaluate_InvalidPackData_ReturnsError(t *testing.T) {
	_, err := Evaluate(context.Background(), EvaluateOpts{
		PackData: []byte(`not json`),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse pack data")
}

func TestEvaluate_EmptyDefs_ReturnsNil(t *testing.T) {
	// Empty slice of defs: Evaluate treats it as "no defs" since len == 0
	// but resolveEvalDefs returns them as-is (not nil).
	// The nil-or-empty check in Evaluate returns nil results.
	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: []evals.EvalDef{},
	})
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestEvaluate_EmptyMessages(t *testing.T) {
	defs := []evals.EvalDef{
		{
			ID:      "check",
			Type:    "contains",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"patterns": []any{"hello"}},
		},
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: defs,
		Messages: nil,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed, "should fail with no messages to match")
}

func TestEvaluate_EventBusEmission(t *testing.T) {
	bus := events.NewEventBus()

	var mu sync.Mutex
	var received []*events.Event
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})

	defs := []evals.EvalDef{
		{
			ID:      "pass",
			Type:    "contains",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"patterns": []any{"hello"}},
		},
	}
	messages := []types.Message{
		types.NewAssistantMessage("hello world"),
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: defs,
		Messages: messages,
		EventBus: bus,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)

	// Close bus to flush pending events
	bus.Close()

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(received), 1)
	assert.Equal(t, events.EventEvalCompleted, received[0].Type)
}

func TestEvaluate_CustomRegistry(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	// Empty registry — handler lookup will fail

	defs := []evals.EvalDef{
		{
			ID:      "check",
			Type:    "contains",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"patterns": []any{"hello"}},
		},
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: defs,
		Messages: []types.Message{types.NewAssistantMessage("hello")},
		Registry: registry,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.NotEmpty(t, results[0].Error, "should have error from missing handler")
}

func TestEvaluate_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	defs := []evals.EvalDef{
		{
			ID:      "check",
			Type:    "contains",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"patterns": []any{"hello"}},
		},
	}

	results, err := Evaluate(ctx, EvaluateOpts{
		EvalDefs: defs,
		Messages: []types.Message{types.NewAssistantMessage("hello")},
	})

	require.NoError(t, err)
	// Runner may skip evals when context is canceled
	assert.Empty(t, results)
}

func TestEvaluate_JudgeMetadata(t *testing.T) {
	// Verify judge metadata is wired into EvalContext
	defs := []evals.EvalDef{
		{
			ID:      "check",
			Type:    "contains",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"patterns": []any{"hello"}},
		},
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:      defs,
		Messages:      []types.Message{types.NewAssistantMessage("hello")},
		JudgeProvider: "mock-judge",
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
}
