package sdk

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// containsDef returns a single "contains" eval def for use in tests.
func containsDef(id string, patterns ...string) []evals.EvalDef {
	anyPatterns := make([]any, len(patterns))
	for i, p := range patterns {
		anyPatterns[i] = p
	}
	return []evals.EvalDef{{
		ID:      id,
		Type:    "contains",
		Trigger: evals.TriggerEveryTurn,
		Params:  map[string]any{"patterns": anyPatterns},
	}}
}

func TestEvaluate_WithEvalDefs(t *testing.T) {
	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:  containsDef("greeting", "hello"),
		Messages:  []types.Message{types.NewUserMessage("hi"), types.NewAssistantMessage("hello there!")},
		SessionID: "test-session",
		TurnIndex: 1,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
	assert.Equal(t, "greeting", results[0].EvalID)
}

func TestEvaluate_WithEvalDefs_Failing(t *testing.T) {
	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:  containsDef("missing", "nonexistent"),
		Messages:  []types.Message{types.NewAssistantMessage("hello there!")},
		SessionID: "s1",
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
}

const evalTestPack = "testdata/packs/eval-test.pack.json"

func TestEvaluate_WithPackPath(t *testing.T) {
	results, err := Evaluate(context.Background(), EvaluateOpts{
		PackPath:             evalTestPack,
		Messages:             []types.Message{types.NewUserMessage("hi"), types.NewAssistantMessage("hello! how can I help?")},
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
	results, err := Evaluate(context.Background(), EvaluateOpts{
		PackPath:             evalTestPack,
		PromptName:           "assistant",
		Messages:             []types.Message{types.NewAssistantMessage("thank you for your patience")},
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
	data, err := os.ReadFile(evalTestPack)
	require.NoError(t, err)

	results, err := Evaluate(context.Background(), EvaluateOpts{
		PackData:  data,
		Messages:  []types.Message{types.NewAssistantMessage("hello!")},
		SessionID: "s1",
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "greeting_check", results[0].EvalID)
	assert.True(t, results[0].Passed)
}

func TestEvaluate_SessionTrigger(t *testing.T) {
	results, err := Evaluate(context.Background(), EvaluateOpts{
		PackPath:             evalTestPack,
		Messages:             []types.Message{types.NewAssistantMessage("goodbye and take care")},
		SessionID:            "s1",
		Trigger:              evals.TriggerOnSessionComplete,
		SkipSchemaValidation: true,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "session_check", results[0].EvalID)
	assert.True(t, results[0].Passed)
}

func TestEvaluate_ErrorCases(t *testing.T) {
	tests := []struct {
		name    string
		opts    EvaluateOpts
		wantErr string
	}{
		{
			name:    "no source",
			opts:    EvaluateOpts{Messages: []types.Message{types.NewAssistantMessage("hi")}},
			wantErr: "one of EvalDefs, PackData, or PackPath must be provided",
		},
		{
			name:    "invalid pack path",
			opts:    EvaluateOpts{PackPath: "nonexistent.pack.json"},
			wantErr: "load pack",
		},
		{
			name:    "invalid pack data",
			opts:    EvaluateOpts{PackData: []byte(`not json`)},
			wantErr: "parse pack data",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Evaluate(context.Background(), tt.opts)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestEvaluate_EmptyDefs_ReturnsNil(t *testing.T) {
	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: []evals.EvalDef{},
	})
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestEvaluate_EmptyMessages(t *testing.T) {
	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: containsDef("check", "hello"),
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

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: containsDef("pass", "hello"),
		Messages: []types.Message{types.NewAssistantMessage("hello world")},
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

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: containsDef("check", "hello"),
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

	results, err := Evaluate(ctx, EvaluateOpts{
		EvalDefs: containsDef("check", "hello"),
		Messages: []types.Message{types.NewAssistantMessage("hello")},
	})

	require.NoError(t, err)
	// Runner may skip evals when context is canceled
	assert.Empty(t, results)
}

func TestEvaluate_TracerProvider(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:       containsDef("greeting", "hello"),
		Messages:       []types.Message{types.NewAssistantMessage("hello world")},
		TracerProvider: tp,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)

	// Force flush to ensure spans are exported
	require.NoError(t, tp.ForceFlush(context.Background()))

	spans := exp.GetSpans()
	var evalSpans []tracetest.SpanStub
	for _, s := range spans {
		if s.Name == "promptkit.eval.greeting" {
			evalSpans = append(evalSpans, s)
		}
	}
	require.Len(t, evalSpans, 1, "expected one OTel span for eval 'greeting'")
}

func TestEvaluate_TracerProvider_CreatesEventBus(t *testing.T) {
	// When TracerProvider is set but EventBus is nil, Evaluate should create one automatically
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// No EventBus provided — should still work
	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:       containsDef("check", "hello"),
		Messages:       []types.Message{types.NewAssistantMessage("hello")},
		TracerProvider: tp,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
}

func TestEvaluate_JudgeMetadata(t *testing.T) {
	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:      containsDef("check", "hello"),
		Messages:      []types.Message{types.NewAssistantMessage("hello")},
		JudgeProvider: "mock-judge",
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
}
