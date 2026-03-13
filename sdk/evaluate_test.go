package sdk

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/metrics"
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
		EvalDefs:  containsDef("pass", "hello"),
		Messages:  []types.Message{types.NewAssistantMessage("hello world")},
		EventBus:  bus,
		SessionID: "eval-session-456",
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
	assert.Equal(t, "eval-session-456", received[0].SessionID,
		"eval events from Evaluate() should include the SessionID from opts")
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

// newTestTracerProvider creates an in-memory OTel tracer provider for testing.
// Caller must defer tp.Shutdown(ctx).
func newTestTracerProvider() (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	return exp, tp
}

func TestEvaluate_TracerProvider(t *testing.T) {
	exp, tp := newTestTracerProvider()
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
	_, tp := newTestTracerProvider()
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

func TestEvaluate_RuntimeConfigPath(t *testing.T) {
	// Write a RuntimeConfig that binds an exec eval to a shell script
	script := writeTestScript(t, `#!/bin/sh
read input
echo '{"score": 0.95, "detail": "exec eval OK"}'
`)
	rcYAML := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: test
spec:
  evals:
    custom_check:
      command: ` + script + `
      timeout_ms: 5000
`)
	rcPath := t.TempDir() + "/runtime.yaml"
	require.NoError(t, os.WriteFile(rcPath, rcYAML, 0o644))

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: []evals.EvalDef{{
			ID:      "exec-eval",
			Type:    "custom_check",
			Trigger: evals.TriggerEveryTurn,
		}},
		RuntimeConfigPath: rcPath,
		Messages:          []types.Message{types.NewAssistantMessage("hello world")},
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "exec-eval", results[0].EvalID)
	require.NotNil(t, results[0].Score)
	assert.InDelta(t, 0.95, *results[0].Score, 0.001)
	assert.Equal(t, "exec eval OK", results[0].Explanation)
}

func TestEvaluate_RuntimeConfigPath_InvalidPath(t *testing.T) {
	_, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:          containsDef("check", "hello"),
		RuntimeConfigPath: "/nonexistent/runtime.yaml",
		Messages:          []types.Message{types.NewAssistantMessage("hello")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load runtime config evals")
}

func TestEvaluate_WithEvalGroups(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "safety", Type: "contains", Trigger: evals.TriggerEveryTurn,
			Params: map[string]any{"patterns": []any{"hello"}}, Groups: []string{"safety"}},
		{ID: "quality", Type: "contains", Trigger: evals.TriggerEveryTurn,
			Params: map[string]any{"patterns": []any{"hello"}}, Groups: []string{"quality"}},
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:   defs,
		EvalGroups: []string{"safety"},
		Messages:   []types.Message{types.NewAssistantMessage("hello")},
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "safety", results[0].EvalID)
}

func TestEvaluate_WithEvalGroupsNilRunsAll(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "a", Type: "contains", Trigger: evals.TriggerEveryTurn,
			Params: map[string]any{"patterns": []any{"hello"}}, Groups: []string{"safety"}},
		{ID: "b", Type: "contains", Trigger: evals.TriggerEveryTurn,
			Params: map[string]any{"patterns": []any{"hello"}}},
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: defs,
		Messages: []types.Message{types.NewAssistantMessage("hello")},
	})

	require.NoError(t, err)
	require.Len(t, results, 2, "nil EvalGroups should run all evals")
}

func TestEvaluate_WithEvalGroupsNoMatchReturnsNil(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "a", Type: "contains", Trigger: evals.TriggerEveryTurn,
			Params: map[string]any{"patterns": []any{"hello"}}, Groups: []string{"safety"}},
	}

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:   defs,
		EvalGroups: []string{"latency"},
		Messages:   []types.Message{types.NewAssistantMessage("hello")},
	})

	require.NoError(t, err)
	assert.Empty(t, results, "no matching groups should return nil results")
}

func TestEvaluate_MetricRecorder(t *testing.T) {
	defs := []evals.EvalDef{{
		ID:      "scored-eval",
		Type:    "contains",
		Trigger: evals.TriggerEveryTurn,
		Params:  map[string]any{"patterns": []any{"hello"}},
		Metric: &evals.MetricDef{
			Name: "greeting_score",
			Type: evals.MetricGauge,
		},
	}}

	reg := prometheus.NewRegistry()
	collector := metrics.NewCollector(metrics.CollectorOpts{
		Registerer:             reg,
		Namespace:              "test",
		DisablePipelineMetrics: true,
	})
	metricCtx := collector.Bind(nil)

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:       defs,
		Messages:       []types.Message{types.NewAssistantMessage("hello world")},
		MetricRecorder: metricCtx,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)

	// Verify metric was recorded via prometheus registry
	families, gatherErr := reg.Gather()
	require.NoError(t, gatherErr)

	var found bool
	for _, fam := range families {
		if fam.GetName() == "test_eval_greeting_score" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected test_eval_greeting_score metric in registry")
}

func TestEvaluate_MetricsCollector(t *testing.T) {
	defs := []evals.EvalDef{{
		ID:      "scored-eval",
		Type:    "contains",
		Trigger: evals.TriggerEveryTurn,
		Params:  map[string]any{"patterns": []any{"hello"}},
		Metric: &evals.MetricDef{
			Name: "greeting_score",
			Type: evals.MetricGauge,
		},
	}}

	reg := prometheus.NewRegistry()
	collector := metrics.NewEvalOnlyCollector(metrics.CollectorOpts{
		Registerer: reg,
		Namespace:  "test",
	})

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:         defs,
		Messages:         []types.Message{types.NewAssistantMessage("hello world")},
		MetricsCollector: collector,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)

	// Verify metric was recorded via prometheus registry
	families, gatherErr := reg.Gather()
	require.NoError(t, gatherErr)

	var found bool
	for _, fam := range families {
		if fam.GetName() == "test_eval_greeting_score" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected test_eval_greeting_score metric in registry")
}

func TestEvaluate_MetricsCollector_WithInstanceLabels(t *testing.T) {
	defs := []evals.EvalDef{{
		ID:      "scored-eval",
		Type:    "contains",
		Trigger: evals.TriggerEveryTurn,
		Params:  map[string]any{"patterns": []any{"hello"}},
		Metric: &evals.MetricDef{
			Name: "greeting_score",
			Type: evals.MetricGauge,
		},
	}}

	reg := prometheus.NewRegistry()
	collector := metrics.NewEvalOnlyCollector(metrics.CollectorOpts{
		Registerer:     reg,
		Namespace:      "test",
		InstanceLabels: []string{"tenant"},
	})

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:              defs,
		Messages:              []types.Message{types.NewAssistantMessage("hello world")},
		MetricsCollector:      collector,
		MetricsInstanceLabels: map[string]string{"tenant": "acme"},
	})

	require.NoError(t, err)
	require.Len(t, results, 1)

	families, gatherErr := reg.Gather()
	require.NoError(t, gatherErr)

	var found bool
	for _, fam := range families {
		if fam.GetName() == "test_eval_greeting_score" {
			found = true
			require.NotEmpty(t, fam.GetMetric())
			labels := fam.GetMetric()[0].GetLabel()
			require.Len(t, labels, 1)
			assert.Equal(t, "tenant", labels[0].GetName())
			assert.Equal(t, "acme", labels[0].GetValue())
			break
		}
	}
	assert.True(t, found, "expected test_eval_greeting_score metric with tenant label")
}

func TestEvaluate_MetricsCollector_TakesPrecedence(t *testing.T) {
	// MetricsCollector should take precedence over MetricRecorder
	defs := []evals.EvalDef{{
		ID:      "scored-eval",
		Type:    "contains",
		Trigger: evals.TriggerEveryTurn,
		Params:  map[string]any{"patterns": []any{"hello"}},
		Metric: &evals.MetricDef{
			Name: "greeting_score",
			Type: evals.MetricGauge,
		},
	}}

	reg := prometheus.NewRegistry()
	collector := metrics.NewEvalOnlyCollector(metrics.CollectorOpts{
		Registerer: reg,
		Namespace:  "preferred",
	})

	// MetricRecorder should be ignored when MetricsCollector is set
	otherReg := prometheus.NewRegistry()
	otherCollector := metrics.NewEvalOnlyCollector(metrics.CollectorOpts{
		Registerer: otherReg,
		Namespace:  "ignored",
	})

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:         defs,
		Messages:         []types.Message{types.NewAssistantMessage("hello world")},
		MetricsCollector: collector,
		MetricRecorder:   otherCollector.Bind(nil),
	})

	require.NoError(t, err)
	require.Len(t, results, 1)

	// Metric should be in the preferred registry (from MetricsCollector)
	families, gatherErr := reg.Gather()
	require.NoError(t, gatherErr)
	var found bool
	for _, fam := range families {
		if fam.GetName() == "preferred_eval_greeting_score" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected preferred_eval_greeting_score in MetricsCollector registry")

	// MetricRecorder's registry should be empty
	otherFamilies, gatherErr := otherReg.Gather()
	require.NoError(t, gatherErr)
	assert.Empty(t, otherFamilies, "MetricRecorder should not have received metrics")
}

func TestEvaluate_MetricRecorder_NoMetricDef(t *testing.T) {
	// Evals without Metric defs should not cause errors when MetricRecorder is set
	reg := prometheus.NewRegistry()
	collector := metrics.NewCollector(metrics.CollectorOpts{
		Registerer:             reg,
		DisablePipelineMetrics: true,
	})
	metricCtx := collector.Bind(nil)

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs:       containsDef("simple", "hello"),
		Messages:       []types.Message{types.NewAssistantMessage("hello")},
		MetricRecorder: metricCtx,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)

	// No metrics should have been recorded
	families, gatherErr := reg.Gather()
	require.NoError(t, gatherErr)
	assert.Empty(t, families, "no metrics should be recorded for evals without MetricDef")
}

func TestValidateEvalTypes_AllRegistered(t *testing.T) {
	missing, err := ValidateEvalTypes(ValidateEvalTypesOpts{
		EvalDefs: containsDef("check", "hello"),
	})
	require.NoError(t, err)
	assert.Empty(t, missing, "all built-in types should be registered")
}

func TestValidateEvalTypes_MissingType(t *testing.T) {
	missing, err := ValidateEvalTypes(ValidateEvalTypesOpts{
		EvalDefs: []evals.EvalDef{
			{ID: "good", Type: "contains", Trigger: evals.TriggerEveryTurn},
			{ID: "bad", Type: "nonexistent_eval_type", Trigger: evals.TriggerEveryTurn},
		},
	})
	require.NoError(t, err)
	require.Len(t, missing, 1)
	assert.Equal(t, "bad", missing[0].ID)
	assert.Equal(t, "nonexistent_eval_type", missing[0].Type)
}

func TestValidateEvalTypes_FromPackPath(t *testing.T) {
	missing, err := ValidateEvalTypes(ValidateEvalTypesOpts{
		PackPath:             evalTestPack,
		SkipSchemaValidation: true,
	})
	require.NoError(t, err)
	assert.Empty(t, missing, "pack eval types should all be registered")
}

func TestValidateEvalTypes_RuntimeConfigRegistersHandler(t *testing.T) {
	script := writeTestScript(t, "#!/bin/sh\necho '{}'")
	rcYAML := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: test
spec:
  evals:
    custom_type:
      command: ` + script + `
`)
	rcPath := t.TempDir() + "/runtime.yaml"
	require.NoError(t, os.WriteFile(rcPath, rcYAML, 0o644))

	// Without RuntimeConfig, custom_type is missing
	missing, err := ValidateEvalTypes(ValidateEvalTypesOpts{
		EvalDefs: []evals.EvalDef{{ID: "ext", Type: "custom_type"}},
	})
	require.NoError(t, err)
	require.Len(t, missing, 1, "custom_type should be missing without RuntimeConfig")

	// With RuntimeConfig, custom_type is registered
	missing, err = ValidateEvalTypes(ValidateEvalTypesOpts{
		EvalDefs:          []evals.EvalDef{{ID: "ext", Type: "custom_type"}},
		RuntimeConfigPath: rcPath,
	})
	require.NoError(t, err)
	assert.Empty(t, missing, "custom_type should be registered via RuntimeConfig")
}

func TestValidateEvalTypes_EmptyDefs(t *testing.T) {
	missing, err := ValidateEvalTypes(ValidateEvalTypesOpts{
		EvalDefs: []evals.EvalDef{},
	})
	require.NoError(t, err)
	assert.Empty(t, missing)
}

func TestValidateEvalTypes_InvalidPackPath(t *testing.T) {
	_, err := ValidateEvalTypes(ValidateEvalTypesOpts{
		PackPath: "nonexistent.pack.json",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve eval defs")
}

// writeTestScript creates a temporary executable shell script for tests.
func writeTestScript(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/eval.sh"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}
