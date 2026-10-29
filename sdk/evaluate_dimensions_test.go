package sdk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/metrics"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// dimensionsRun evaluates a session with two evals that each produce named
// sub-scores from ONE call — an llm_judge scoring a rubric through a real
// spec-driven judge, and an exec eval program — records them through the
// metrics collector, and returns what a Prometheus scrape of /metrics serves,
// along with the results and the eval events seen on the bus.
func dimensionsRun(t *testing.T, dimensionLimit int) (scrape string, results []evals.EvalResult, seen []*events.EvalEventData) {
	t.Helper()

	// The judge returns four well-formed dimensions and one that is not
	// snake_case. The exec program returns two.
	judge := handlers.NewSpecJudgeProvider(&providers.ProviderSpec{
		Type: "mock", Model: "judge",
		AdditionalConfig: map[string]any{"repository": mock.NewInMemoryMockRepository(
			`{"score": 0.8, "clarity": 0.9, "accuracy": 0.7, "citations": 0.5, "completeness": 0.6,` +
				` "Tone": 1, "passed": true, "reasoning": "solid"}`)},
	})
	script := writeTestScript(t, `#!/bin/sh
cat > /dev/null
echo '{"score": 0.75, "metrics": {"functional_score": 0.75, "security_score": 0.5}}'
`)
	rcPath := t.TempDir() + "/runtime.yaml"
	require.NoError(t, os.WriteFile(rcPath, []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: test
spec:
  evals:
    sandbox_score:
      command: `+script+`
`), 0o644))

	reg := prometheus.NewRegistry()
	collector := metrics.NewEvalOnlyCollector(metrics.CollectorOpts{
		Registerer: reg, Namespace: "test", EvalDimensionLimit: dimensionLimit,
	})
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	evCh := make(chan *events.EvalEventData, 4)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) { evCh <- e.Data.(*events.EvalEventData) })

	results, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: []evals.EvalDef{
			{
				ID: "rubric", Type: "llm_judge", Trigger: evals.TriggerOnSessionComplete,
				Params: map[string]any{"criteria": "Score clarity, accuracy, citations, completeness"},
				Metric: &evals.MetricDef{Name: "rubric_score", Type: evals.MetricGauge},
			},
			{
				ID: "sandbox", Type: "sandbox_score", Trigger: evals.TriggerOnSessionComplete,
				Metric: &evals.MetricDef{Name: "sandbox_score", Type: evals.MetricGauge},
			},
		},
		Trigger:           evals.TriggerOnSessionComplete,
		Messages:          []types.Message{types.NewUserMessage("q"), types.NewAssistantMessage("an answer")},
		JudgeProvider:     judge,
		RuntimeConfigPath: rcPath,
		MetricsCollector:  collector,
		EventBus:          bus,
	})
	require.NoError(t, err)

	for range 2 {
		select {
		case d := <-evCh:
			seen = append(seen, d)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for eval.completed")
		}
	}

	srv := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body), results, seen
}

// TestEvaluate_EvalDimensions_ScrapedFromPrometheus proves the whole path:
// one judge call and one program run each yield several named scores, which
// reach the result, the bus and a /metrics scrape — and the cap and the name
// check hold the scrape to a bounded set.
func TestEvaluate_EvalDimensions_ScrapedFromPrometheus(t *testing.T) {
	scrape, results, seen := dimensionsRun(t, 3)

	byID := map[string]evals.EvalResult{}
	for _, r := range results {
		require.Empty(t, r.Error, r.EvalID)
		byID[r.EvalID] = r
	}
	assert.Len(t, byID["rubric"].Metrics, 5, "the result keeps every dimension the judge returned")
	assert.Equal(t, map[string]float64{"functional_score": 0.75, "security_score": 0.5}, byID["sandbox"].Metrics)
	for _, d := range seen {
		assert.Equal(t, byID[d.EvalID].Metrics, d.Metrics, "eval.completed carries %s's metrics", d.EvalID)
	}

	for _, line := range []string{
		"test_eval_rubric_score 0.8",
		"test_eval_sandbox_score 0.75",
		// Sorted admission under a cap of 3: accuracy, citations, clarity.
		`test_eval_rubric_score_dimension{dimension="accuracy"} 0.7`,
		`test_eval_rubric_score_dimension{dimension="citations"} 0.5`,
		`test_eval_rubric_score_dimension{dimension="clarity"} 0.9`,
		`test_eval_sandbox_score_dimension{dimension="functional_score"} 0.75`,
		`test_eval_sandbox_score_dimension{dimension="security_score"} 0.5`,
		`test_eval_dimensions_dropped_total{metric="rubric_score",reason="invalid_name"} 1`,
		`test_eval_dimensions_dropped_total{metric="rubric_score",reason="over_limit"} 1`,
	} {
		assert.Contains(t, scrape, line+"\n")
	}
	assert.NotContains(t, scrape, `dimension="completeness"`, "past the cap")
	assert.NotContains(t, scrape, `dimension="Tone"`, "not snake_case")
}

// Without the opt-in the scrape is exactly what it was before: no derived
// families, no drop counter. Results and events still carry the metrics.
func TestEvaluate_EvalDimensions_NotScrapedWithoutOptIn(t *testing.T) {
	scrape, results, seen := dimensionsRun(t, 0)

	assert.Contains(t, scrape, "test_eval_rubric_score 0.8\n")
	assert.False(t, strings.Contains(scrape, "_dimension"), "no derived family without EvalDimensionLimit")
	assert.NotContains(t, scrape, "dimensions_dropped")
	for _, r := range results {
		assert.NotEmpty(t, r.Metrics, r.EvalID)
	}
	for _, d := range seen {
		assert.NotEmpty(t, d.Metrics, d.EvalID)
	}
}
