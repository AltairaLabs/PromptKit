package evals_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
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

func TestE2E_EvalRunner_FullFlow(t *testing.T) {
	// Setup: Registry → EvalDefs → EvalRunner → MetricResultWriter → MetricCollector
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

	evalCtx := &evals.EvalContext{
		SessionID:     "test-session",
		TurnIndex:     1,
		CurrentOutput: "Hello, how can I help?",
	}

	// Run evals directly
	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Write results to metrics
	if err := metricWriter.WriteResults(context.Background(), results); err != nil {
		t.Fatal(err)
	}

	// Verify metrics via WritePrometheus
	var buf bytes.Buffer
	if err := collector.WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, `promptpack_response_quality{session_id="test-session",turn_index="1"} 0.92`) {
		t.Errorf("expected labeled quality gauge, got:\n%s", output)
	}
	if !strings.Contains(output, "# TYPE promptpack_response_length histogram") {
		t.Errorf("expected length histogram, got:\n%s", output)
	}
	if !strings.Contains(output, `session_id="test-session"`) {
		t.Errorf("expected session_id label in output, got:\n%s", output)
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
	results := runner.RunTurnEvals(context.Background(), resolved, &evals.EvalContext{
		SessionID: "s1",
		TurnIndex: 1,
	})

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
