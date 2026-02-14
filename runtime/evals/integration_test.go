package evals_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// stubHandler is a configurable eval handler for testing.
type stubHandler struct {
	evalType string
	result   *evals.EvalResult
	err      error
}

func (h *stubHandler) Type() string { return h.evalType }

func (h *stubHandler) Eval(
	_ context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	if h.err != nil {
		return nil, h.err
	}
	return h.result, nil
}

func float64P(v float64) *float64 { return &v }

func TestE2E_InProcDispatcher_FullFlow(t *testing.T) {
	// Setup: Registry → EvalDefs → InProcDispatcher → MetricResultWriter → MetricCollector
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(&stubHandler{
		evalType: "quality_check",
		result:   &evals.EvalResult{Passed: true, Score: float64P(0.92)},
	})
	registry.Register(&stubHandler{
		evalType: "length_check",
		result:   &evals.EvalResult{Passed: true, MetricValue: float64P(150)},
	})

	defs := []evals.EvalDef{
		{
			ID:      "quality",
			Type:    "quality_check",
			Trigger: evals.TriggerEveryTurn,
			Metric:  &evals.MetricDef{Name: "response_quality", Type: evals.MetricGauge},
		},
		{
			ID:      "length",
			Type:    "length_check",
			Trigger: evals.TriggerEveryTurn,
			Metric:  &evals.MetricDef{Name: "response_length", Type: evals.MetricHistogram},
		},
	}

	collector := evals.NewMetricCollector()
	metricWriter := evals.NewMetricResultWriter(collector, defs)
	runner := evals.NewEvalRunner(registry)
	dispatcher := evals.NewInProcDispatcher(runner, metricWriter)

	evalCtx := &evals.EvalContext{
		SessionID:     "test-session",
		TurnIndex:     1,
		CurrentOutput: "Hello, how can I help?",
	}

	// Dispatch
	results, err := dispatcher.DispatchTurnEvals(context.Background(), defs, evalCtx)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify metrics via WritePrometheus
	var buf bytes.Buffer
	if err := collector.WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "promptpack_response_quality 0.92") {
		t.Errorf("expected quality gauge, got:\n%s", output)
	}
	if !strings.Contains(output, "# TYPE promptpack_response_length histogram") {
		t.Errorf("expected length histogram, got:\n%s", output)
	}
}

func TestE2E_PackPromptOverrideResolution(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(&stubHandler{
		evalType: "type_a",
		result:   &evals.EvalResult{Passed: true, Score: float64P(1.0)},
	})
	registry.Register(&stubHandler{
		evalType: "type_b_override",
		result:   &evals.EvalResult{Passed: true, Score: float64P(0.8)},
	})
	registry.Register(&stubHandler{
		evalType: "type_c",
		result:   &evals.EvalResult{Passed: false, Score: float64P(0.3)},
	})

	packEvals := []evals.EvalDef{
		{ID: "a", Type: "type_a", Trigger: evals.TriggerEveryTurn},
		{ID: "b", Type: "type_b", Trigger: evals.TriggerEveryTurn},
	}
	promptEvals := []evals.EvalDef{
		{ID: "b", Type: "type_b_override", Trigger: evals.TriggerEveryTurn}, // Override
		{ID: "c", Type: "type_c", Trigger: evals.TriggerEveryTurn},
	}

	resolved := evals.ResolveEvals(packEvals, promptEvals)
	if len(resolved) != 3 {
		t.Fatalf("expected 3 resolved, got %d", len(resolved))
	}

	runner := evals.NewEvalRunner(registry)
	dispatcher := evals.NewInProcDispatcher(runner, nil)

	results, err := dispatcher.DispatchTurnEvals(context.Background(), resolved, &evals.EvalContext{
		SessionID: "s1",
		TurnIndex: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify override: b should use type_b_override
	if results[1].Type != "type_b_override" {
		t.Errorf("expected type_b_override, got %q", results[1].Type)
	}
	// Verify c ran and failed
	if results[2].Passed {
		t.Error("expected eval c to fail")
	}
}

// e2eEvalLoader is a test PackEvalLoader for integration tests.
type e2eEvalLoader struct {
	defs []evals.EvalDef
}

func (l *e2eEvalLoader) LoadEvals(_ string) ([]evals.EvalDef, error) {
	return l.defs, nil
}

// e2eResultWriter records results with a notification channel.
type e2eResultWriter struct {
	mu      sync.Mutex
	results []evals.EvalResult
	written chan struct{}
}

func newE2EResultWriter() *e2eResultWriter {
	return &e2eResultWriter{written: make(chan struct{}, 100)}
}

func (w *e2eResultWriter) WriteResults(_ context.Context, results []evals.EvalResult) error {
	w.mu.Lock()
	w.results = append(w.results, results...)
	w.mu.Unlock()
	w.written <- struct{}{}
	return nil
}

func (w *e2eResultWriter) Results() []evals.EvalResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := make([]evals.EvalResult, len(w.results))
	copy(r, w.results)
	return r
}

func TestE2E_EventBusEvalListener_MessageCreated(t *testing.T) {
	// Setup registry with a stub handler
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(&stubHandler{
		evalType: "contains",
		result:   &evals.EvalResult{Passed: true, Score: float64P(1.0)},
	})

	defs := []evals.EvalDef{
		{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
	}

	runner := evals.NewEvalRunner(registry)
	writer := newE2EResultWriter()
	dispatcher := evals.NewInProcDispatcher(runner, writer)
	loader := &e2eEvalLoader{defs: defs}

	bus := events.NewEventBus()
	listener := evals.NewEventBusEvalListener(bus, dispatcher, loader, writer)
	defer listener.Close()

	// Pre-seed the session accumulator with a promptID.
	// In production, the SDK would set this when starting a conversation.
	// The EventBusEvalListener uses the promptID to load eval definitions.
	listener.Accumulator().AddMessage("session-1", "test-prompt", "user", "hello")

	// Now publish assistant message to trigger evals
	bus.Publish(&events.Event{
		Type:      events.EventMessageCreated,
		SessionID: "session-1",
		Data: &events.MessageCreatedData{
			Role:    "assistant",
			Content: "Hi there! How can I help?",
		},
	})

	// Wait for async results — the EventBus dispatches listeners async,
	// and the listener dispatches evals async
	select {
	case <-writer.written:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for eval results")
	}

	results := writer.Results()
	if len(results) < 1 {
		t.Fatalf("expected at least 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("expected eval to pass")
	}
}

func TestE2E_MetricCollector_AllTypes(t *testing.T) {
	collector := evals.NewMetricCollector()

	// Gauge
	_ = collector.Record(
		evals.EvalResult{Score: float64P(0.75)},
		&evals.MetricDef{Name: "quality_score", Type: evals.MetricGauge},
	)

	// Counter
	for range 5 {
		_ = collector.Record(
			evals.EvalResult{},
			&evals.MetricDef{Name: "eval_count", Type: evals.MetricCounter},
		)
	}

	// Histogram
	for _, v := range []float64{0.01, 0.05, 0.1, 0.5, 1.0} {
		_ = collector.Record(
			evals.EvalResult{MetricValue: float64P(v)},
			&evals.MetricDef{Name: "latency_seconds", Type: evals.MetricHistogram},
		)
	}

	// Boolean
	_ = collector.Record(
		evals.EvalResult{Passed: true},
		&evals.MetricDef{Name: "safety_check", Type: evals.MetricBoolean},
	)

	var buf bytes.Buffer
	if err := collector.WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}

	output := buf.String()

	// Verify all 4 types present
	checks := []string{
		"# TYPE promptpack_quality_score gauge",
		"promptpack_quality_score 0.75",
		"# TYPE promptpack_eval_count counter",
		"promptpack_eval_count 5",
		"# TYPE promptpack_latency_seconds histogram",
		"promptpack_latency_seconds_count 5",
		"# TYPE promptpack_safety_check gauge", // boolean rendered as gauge
		"promptpack_safety_check 1",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("missing: %q\nin output:\n%s", check, output)
		}
	}
}
