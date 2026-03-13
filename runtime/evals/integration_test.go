package evals_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/metrics"
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
	// Setup: Registry → EvalDefs → EvalRunner → MetricResultWriter → metrics.Collector
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(&stubHandler{
		evalType: "quality_check",
		result:   &evals.EvalResult{Score: float64P(0.92)},
	})
	registry.Register(&stubHandler{
		evalType: "length_check",
		result:   &evals.EvalResult{MetricValue: float64P(150)},
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

	reg := prometheus.NewRegistry()
	collector := metrics.NewCollector(metrics.CollectorOpts{
		Registerer:             reg,
		DisablePipelineMetrics: true,
	})
	metricCtx := collector.Bind(nil)
	metricWriter := evals.NewMetricResultWriter(metricCtx, defs)
	runner := evals.NewEvalRunner(registry)

	evalCtx := &evals.EvalContext{
		SessionID:     "test-session",
		TurnIndex:     1,
		CurrentOutput: "Hello, how can I help?",
	}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if err := metricWriter.WriteResults(context.Background(), results); err != nil {
		t.Fatal(err)
	}

	// Verify metrics via prometheus registry
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}

	famByName := make(map[string]*dto.MetricFamily, len(families))
	for _, fam := range families {
		famByName[fam.GetName()] = fam
	}

	// Check gauge metric
	qualityFam, ok := famByName["promptkit_eval_response_quality"]
	if !ok {
		t.Fatal("expected promptkit_eval_response_quality metric")
	}
	if qualityFam.GetType() != dto.MetricType_GAUGE {
		t.Errorf("expected gauge, got %v", qualityFam.GetType())
	}
	if got := qualityFam.GetMetric()[0].GetGauge().GetValue(); got != 0.92 {
		t.Errorf("expected 0.92, got %v", got)
	}

	// Check histogram metric
	lengthFam, ok := famByName["promptkit_eval_response_length"]
	if !ok {
		t.Fatal("expected promptkit_eval_response_length metric")
	}
	if lengthFam.GetType() != dto.MetricType_HISTOGRAM {
		t.Errorf("expected histogram, got %v", lengthFam.GetType())
	}
	if got := lengthFam.GetMetric()[0].GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("expected sample count 1, got %d", got)
	}
}

func TestE2E_PackPromptOverrideResolution(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(&stubHandler{
		evalType: "type_a",
		result:   &evals.EvalResult{Score: float64P(1.0)},
	})
	registry.Register(&stubHandler{
		evalType: "type_b_override",
		result:   &evals.EvalResult{Score: float64P(0.8)},
	})
	registry.Register(&stubHandler{
		evalType: "type_c",
		result:   &evals.EvalResult{Score: float64P(0.3)},
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
	// Verify c ran and had low score
	if results[2].Score == nil || *results[2].Score >= 1.0 {
		t.Error("expected eval c to have score < 1.0")
	}
}
