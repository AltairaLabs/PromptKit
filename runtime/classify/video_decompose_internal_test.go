package classify

import "testing"

func TestConfigHelpers(t *testing.T) {
	m := map[string]any{"s": "x", "asInt": 3, "asFloat": 2.5, "n": 4, "nf": 7.0}

	if got := stringFromConfig(m, "s"); got != "x" {
		t.Errorf("stringFromConfig hit = %q, want x", got)
	}
	if got := stringFromConfig(m, "missing"); got != "" {
		t.Errorf("stringFromConfig missing = %q, want empty", got)
	}
	if got := stringFromConfig(m, "n"); got != "" {
		t.Errorf("stringFromConfig wrong-type = %q, want empty", got)
	}

	if got := floatFromConfig(m, "asFloat"); got != 2.5 {
		t.Errorf("floatFromConfig float64 = %v, want 2.5", got)
	}
	if got := floatFromConfig(m, "asInt"); got != 3 {
		t.Errorf("floatFromConfig int = %v, want 3", got)
	}
	if got := floatFromConfig(m, "s"); got != 0 {
		t.Errorf("floatFromConfig default = %v, want 0", got)
	}

	if got := intFromConfig(m, "n"); got != 4 {
		t.Errorf("intFromConfig int = %v, want 4", got)
	}
	if got := intFromConfig(m, "nf"); got != 7 {
		t.Errorf("intFromConfig float64 = %v, want 7", got)
	}
	if got := intFromConfig(m, "s"); got != 0 {
		t.Errorf("intFromConfig default = %v, want 0", got)
	}
}

func TestAggregateScores_Empty(t *testing.T) {
	if got := aggregateScores(nil, AggregationMax); got != nil {
		t.Errorf("aggregateScores(nil) = %v, want nil", got)
	}
}

func TestAggregateScores_UnknownStrategyFallsBackToMax(t *testing.T) {
	groups := [][]LabelScore{
		{{Label: "a", Score: 0.2}},
		{{Label: "a", Score: 0.9}},
	}
	got := aggregateScores(groups, "not-a-strategy")
	if len(got) != 1 || got[0].Score != 0.9 {
		t.Errorf("unknown strategy = %v, want max fallback (a=0.9)", got)
	}
}

func TestTopLabel_Empty(t *testing.T) {
	if _, ok := topLabel(nil); ok {
		t.Error("topLabel(nil) should report no label")
	}
}
