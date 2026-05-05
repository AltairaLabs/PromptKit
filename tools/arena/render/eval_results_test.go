package render

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestRenderEvalResults_TableShape(t *testing.T) {
	score := 1.0
	results := []evals.EvalResult{
		{
			EvalID: "diff_stats",
			Type:   "tool_exec",
			Score:  &score,
			Details: map[string]any{
				"tool":       "Bash",
				"latency_ms": int64(5),
				"result": map[string]any{
					"total_loc": float64(19),
					"impl_loc":  float64(5),
				},
			},
		},
	}
	html := string(renderEvalResults(results))
	for _, want := range []string{
		`eval-results-section`,
		`Eval Observations`,
		`1 eval(s)`,
		`<th>#</th>`,
		`<th>Eval ID</th>`,
		`<th>Type</th>`,
		`<th>Metrics</th>`,
		`diff_stats`,
		`tool_exec`,
		`total_loc`,
		`19`,
		`impl_loc`,
		`5`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("table HTML missing %q.\nGot:\n%s", want, html)
		}
	}
	for _, unwanted := range []string{
		`<th>Score</th>`,
		`<th>Explanation</th>`,
	} {
		if strings.Contains(html, unwanted) {
			t.Errorf("table HTML should not contain %q (assertions/score columns are gone)", unwanted)
		}
	}
}

func TestRenderEvalResults_EmptyReturnsEmpty(t *testing.T) {
	if got := string(renderEvalResults(nil)); got != "" {
		t.Errorf("expected empty HTML for nil results, got %q", got)
	}
}

func TestEvalMetricsMap_PromotesNestedResultMap(t *testing.T) {
	r := &evals.EvalResult{
		Details: map[string]any{
			"tool":       "Bash",       // skipped
			"latency_ms": int64(7),     // skipped
			"eval_id":    "diff_stats", // skipped
			"result": map[string]any{
				"total_loc": float64(36),
				"impl_loc":  float64(17),
			},
		},
	}
	got := evalMetricsMap(r)
	if got["total_loc"] != float64(36) {
		t.Errorf("expected total_loc promoted to top level, got %v", got)
	}
	if got["impl_loc"] != float64(17) {
		t.Errorf("expected impl_loc promoted to top level, got %v", got)
	}
	for _, skipped := range []string{"tool", "latency_ms", "eval_id"} {
		if _, ok := got[skipped]; ok {
			t.Errorf("transport-level field %q should be skipped", skipped)
		}
	}
}

func TestEvalMetricsMap_ScalarResultStaysUnderResultKey(t *testing.T) {
	r := &evals.EvalResult{
		Details: map[string]any{
			"tool":   "echo",
			"result": "hello",
		},
	}
	got := evalMetricsMap(r)
	if got["result"] != "hello" {
		t.Errorf("expected scalar result preserved under 'result' key, got %v", got)
	}
}

func TestEvalMetricsMap_FallsBackToValueWhenDetailsEmpty(t *testing.T) {
	r := &evals.EvalResult{
		Value: map[string]any{
			"tool":   "Bash",
			"custom": "metric",
		},
	}
	got := evalMetricsMap(r)
	if got["custom"] != "metric" {
		t.Errorf("expected fallback to Value when Details empty, got %v", got)
	}
	// AssertionEvalHandler stores Value=bool when wrapping; must not
	// be misread as a metric set.
	bare := evalMetricsMap(&evals.EvalResult{Value: true})
	if len(bare) != 0 {
		t.Errorf("bool Value (assertion wrapper) should produce no metrics, got %v", bare)
	}
}

func TestFormatMetricValue_TypeBranches(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "—"},
		{"hello", "hello"},
		{true, "true"},
		{false, "false"},
		{float64(42), "42"},
		{float64(3.14), "3.14"},
		{int(7), "7"},          // falls through to JSON
		{[]int{1, 2}, "[1,2]"}, // falls through to JSON
	}
	for _, c := range cases {
		if got := formatMetricValue(c.in); got != c.want {
			t.Errorf("formatMetricValue(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderEvalResultsSummary_ProducesMetricChips(t *testing.T) {
	results := []evals.EvalResult{
		{
			EvalID: "diff_stats",
			Type:   "tool_exec",
			Details: map[string]any{
				"result": map[string]any{
					"total_loc": float64(19),
					"impl_loc":  float64(5),
				},
			},
		},
	}
	html := string(renderEvalResultsSummary(results))
	for _, want := range []string{
		`summary-chip metric-chip`,
		`chip-key`,
		`chip-val`,
		`total_loc`,
		`19`,
		`impl_loc`,
		`5`,
		`title="diff_stats"`, // tooltip carries the eval id
	} {
		if !strings.Contains(html, want) {
			t.Errorf("summary HTML missing %q.\nGot:\n%s", want, html)
		}
	}
}

func TestRenderEvalResultsSummary_EmptyReturnsEmpty(t *testing.T) {
	if got := string(renderEvalResultsSummary(nil)); got != "" {
		t.Errorf("expected empty HTML for nil results, got %q", got)
	}
}

func TestRenderEvalResultRow_HandlesMissingFields(t *testing.T) {
	r := &evals.EvalResult{} // no id, no type, no metrics
	row := renderEvalResultRow(0, r)
	if !strings.Contains(row, `>—<`) {
		t.Errorf("expected em-dash placeholder for missing id/type, got %s", row)
	}
}
