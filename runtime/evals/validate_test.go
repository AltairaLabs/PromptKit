package evals

import (
	"strings"
	"testing"
)


func TestValidateEvals(t *testing.T) {
	tests := []struct {
		name      string
		defs      []EvalDef
		scope     string
		wantCount int
		wantMsgs  []string // substrings that must appear in errors
	}{
		{
			name:      "nil input returns no errors",
			defs:      nil,
			scope:     "pack",
			wantCount: 0,
		},
		{
			name:      "empty input returns no errors",
			defs:      []EvalDef{},
			scope:     "pack",
			wantCount: 0,
		},
		{
			name: "valid eval with all required fields",
			defs: []EvalDef{
				{ID: "tone-check", Type: "llm_judge", Trigger: TriggerEveryTurn},
			},
			scope:     "pack",
			wantCount: 0,
		},
		{
			name: "valid eval with metric",
			defs: []EvalDef{
				{
					ID: "latency", Type: "custom", Trigger: TriggerEveryTurn,
					Metric: &MetricDef{Name: "eval_latency_seconds", Type: MetricHistogram},
				},
			},
			scope:     "pack",
			wantCount: 0,
		},
		{
			name: "valid eval with sample_percentage",
			defs: []EvalDef{
				{ID: "sampled", Type: "llm_judge", Trigger: TriggerSampleTurns, SamplePercentage: float64Ptr(10.0)},
			},
			scope:     "pack",
			wantCount: 0,
		},
		{
			name: "missing id",
			defs: []EvalDef{
				{Type: "llm_judge", Trigger: TriggerEveryTurn},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"id is required"},
		},
		{
			name: "missing type",
			defs: []EvalDef{
				{ID: "tone-check", Trigger: TriggerEveryTurn},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"type is required"},
		},
		{
			name: "missing trigger",
			defs: []EvalDef{
				{ID: "tone-check", Type: "llm_judge"},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"trigger is required"},
		},
		{
			name: "invalid trigger",
			defs: []EvalDef{
				{ID: "tone-check", Type: "llm_judge", Trigger: "on_full_moon"},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"invalid trigger"},
		},
		{
			name: "duplicate ids",
			defs: []EvalDef{
				{ID: "tone-check", Type: "llm_judge", Trigger: TriggerEveryTurn},
				{ID: "tone-check", Type: "custom", Trigger: TriggerEveryTurn},
			},
			scope:     "prompt:greeting",
			wantCount: 1,
			wantMsgs:  []string{"duplicate eval id"},
		},
		{
			name: "sample_percentage too low",
			defs: []EvalDef{
				{ID: "s", Type: "t", Trigger: TriggerSampleTurns, SamplePercentage: float64Ptr(-1)},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"sample_percentage must be between 0 and 100"},
		},
		{
			name: "sample_percentage too high",
			defs: []EvalDef{
				{ID: "s", Type: "t", Trigger: TriggerSampleTurns, SamplePercentage: float64Ptr(101)},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"sample_percentage must be between 0 and 100"},
		},
		{
			name: "sample_percentage at boundaries is valid",
			defs: []EvalDef{
				{ID: "a", Type: "t", Trigger: TriggerEveryTurn, SamplePercentage: float64Ptr(0)},
				{ID: "b", Type: "t", Trigger: TriggerEveryTurn, SamplePercentage: float64Ptr(100)},
			},
			scope:     "pack",
			wantCount: 0,
		},
		{
			name: "metric missing name",
			defs: []EvalDef{
				{ID: "m", Type: "t", Trigger: TriggerEveryTurn, Metric: &MetricDef{Type: MetricGauge}},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"metric.name is required"},
		},
		{
			name: "metric invalid name",
			defs: []EvalDef{
				{ID: "m", Type: "t", Trigger: TriggerEveryTurn, Metric: &MetricDef{Name: "123bad", Type: MetricGauge}},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"must match Prometheus naming"},
		},
		{
			name: "metric name with spaces is invalid",
			defs: []EvalDef{
				{ID: "m", Type: "t", Trigger: TriggerEveryTurn, Metric: &MetricDef{Name: "has space", Type: MetricGauge}},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"must match Prometheus naming"},
		},
		{
			name: "metric missing type",
			defs: []EvalDef{
				{ID: "m", Type: "t", Trigger: TriggerEveryTurn, Metric: &MetricDef{Name: "good_name"}},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"metric.type is required"},
		},
		{
			name: "metric invalid type",
			defs: []EvalDef{
				{ID: "m", Type: "t", Trigger: TriggerEveryTurn, Metric: &MetricDef{Name: "good_name", Type: "summary"}},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"invalid metric.type"},
		},
		{
			name: "metric range min > max",
			defs: []EvalDef{
				{
					ID: "m", Type: "t", Trigger: TriggerEveryTurn,
					Metric: &MetricDef{
						Name:  "score",
						Type:  MetricGauge,
						Range: &Range{Min: float64Ptr(10), Max: float64Ptr(5)},
					},
				},
			},
			scope:     "pack",
			wantCount: 1,
			wantMsgs:  []string{"range.min", "must be <=", "range.max"},
		},
		{
			name: "metric range valid",
			defs: []EvalDef{
				{
					ID: "m", Type: "t", Trigger: TriggerEveryTurn,
					Metric: &MetricDef{
						Name:  "score",
						Type:  MetricGauge,
						Range: &Range{Min: float64Ptr(0), Max: float64Ptr(1)},
					},
				},
			},
			scope:     "pack",
			wantCount: 0,
		},
		{
			name: "metric range with only min is valid",
			defs: []EvalDef{
				{
					ID: "m", Type: "t", Trigger: TriggerEveryTurn,
					Metric: &MetricDef{
						Name:  "score",
						Type:  MetricGauge,
						Range: &Range{Min: float64Ptr(0)},
					},
				},
			},
			scope:     "pack",
			wantCount: 0,
		},
		{
			name: "multiple errors accumulate",
			defs: []EvalDef{
				{}, // missing id, type, trigger
			},
			scope:     "pack",
			wantCount: 3,
			wantMsgs:  []string{"id is required", "type is required", "trigger is required"},
		},
		{
			name: "scope appears in error messages",
			defs: []EvalDef{
				{Type: "t", Trigger: TriggerEveryTurn},
			},
			scope:     "prompt:greeting",
			wantCount: 1,
			wantMsgs:  []string{"prompt:greeting"},
		},
		{
			name: "valid prometheus metric names",
			defs: []EvalDef{
				{ID: "a", Type: "t", Trigger: TriggerEveryTurn, Metric: &MetricDef{Name: "simple", Type: MetricGauge}},
				{ID: "b", Type: "t", Trigger: TriggerEveryTurn, Metric: &MetricDef{Name: "_private", Type: MetricCounter}},
				{ID: "c", Type: "t", Trigger: TriggerEveryTurn, Metric: &MetricDef{Name: "namespace:metric_name", Type: MetricHistogram}},
				{ID: "d", Type: "t", Trigger: TriggerEveryTurn, Metric: &MetricDef{Name: "CamelCase123", Type: MetricBoolean}},
			},
			scope:     "pack",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateEvals(tt.defs, tt.scope)
			if len(errs) != tt.wantCount {
				t.Errorf("got %d errors, want %d:\n%s", len(errs), tt.wantCount, strings.Join(errs, "\n"))
				return
			}
			for _, msg := range tt.wantMsgs {
				found := false
				for _, err := range errs {
					if strings.Contains(err, msg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got:\n%s", msg, strings.Join(errs, "\n"))
				}
			}
		})
	}
}

func TestValidateEvals_MetricLabelValidation(t *testing.T) {
	tests := []struct {
		name      string
		labels    map[string]string
		wantCount int
		wantMsgs  []string
	}{
		{
			name:      "valid label names",
			labels:    map[string]string{"env": "prod", "eval_type": "contains", "_private": "val"},
			wantCount: 0,
		},
		{
			name:      "digit prefix is invalid",
			labels:    map[string]string{"1bad": "val"},
			wantCount: 1,
			wantMsgs:  []string{"must match Prometheus label naming"},
		},
		{
			name:      "dash is invalid",
			labels:    map[string]string{"my-label": "val"},
			wantCount: 1,
			wantMsgs:  []string{"must match Prometheus label naming"},
		},
		{
			name:      "reserved __ prefix",
			labels:    map[string]string{"__internal": "val"},
			wantCount: 1,
			wantMsgs:  []string{`must not start with "__"`},
		},
		{
			name:      "nil labels is valid",
			labels:    nil,
			wantCount: 0,
		},
		{
			name:      "empty labels is valid",
			labels:    map[string]string{},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defs := []EvalDef{{
				ID: "test", Type: "custom", Trigger: TriggerEveryTurn,
				Metric: &MetricDef{Name: "test_metric", Type: MetricGauge, Labels: tt.labels},
			}}
			errs := ValidateEvals(defs, "pack")
			if len(errs) != tt.wantCount {
				t.Errorf("got %d errors, want %d:\n%s", len(errs), tt.wantCount, strings.Join(errs, "\n"))
				return
			}
			for _, msg := range tt.wantMsgs {
				found := false
				for _, err := range errs {
					if strings.Contains(err, msg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got:\n%s", msg, strings.Join(errs, "\n"))
				}
			}
		})
	}
}

func TestValidateEvals_ValidAllTriggerTypes(t *testing.T) {
	for trigger := range ValidTriggers {
		defs := []EvalDef{{ID: "test", Type: "custom", Trigger: trigger}}
		errs := ValidateEvals(defs, "pack")
		if len(errs) != 0 {
			t.Errorf("trigger %q should be valid, got errors: %v", trigger, errs)
		}
	}
}

func TestValidateEvals_ValidAllMetricTypes(t *testing.T) {
	for mt := range ValidMetricTypes {
		defs := []EvalDef{{
			ID: "test", Type: "custom", Trigger: TriggerEveryTurn,
			Metric: &MetricDef{Name: "test_metric", Type: mt},
		}}
		errs := ValidateEvals(defs, "pack")
		if len(errs) != 0 {
			t.Errorf("metric type %q should be valid, got errors: %v", mt, errs)
		}
	}
}

func TestValidateEvalTypes(t *testing.T) {
	// Create a registry with only "contains" and "regex" handlers
	registry := NewEmptyEvalTypeRegistry()
	registry.Register(&stubHandler{typeName: "contains"})
	registry.Register(&stubHandler{typeName: "regex"})

	tests := []struct {
		name      string
		defs      []EvalDef
		wantCount int
		wantMsgs  []string
	}{
		{
			name:      "nil defs returns no errors",
			defs:      nil,
			wantCount: 0,
		},
		{
			name:      "empty defs returns no errors",
			defs:      []EvalDef{},
			wantCount: 0,
		},
		{
			name: "all known types pass",
			defs: []EvalDef{
				{ID: "a", Type: "contains"},
				{ID: "b", Type: "regex"},
			},
			wantCount: 0,
		},
		{
			name: "unknown type returns error",
			defs: []EvalDef{
				{ID: "a", Type: "contians"},
			},
			wantCount: 1,
			wantMsgs:  []string{`unknown type "contians"`, `eval "a"`},
		},
		{
			name: "multiple unknown types",
			defs: []EvalDef{
				{ID: "a", Type: "contians"},
				{ID: "b", Type: "regx"},
			},
			wantCount: 2,
			wantMsgs:  []string{`"contians"`, `"regx"`},
		},
		{
			name: "empty type is skipped (caught by ValidateEvals)",
			defs: []EvalDef{
				{ID: "a", Type: ""},
			},
			wantCount: 0,
		},
		{
			name: "mix of known and unknown types",
			defs: []EvalDef{
				{ID: "a", Type: "contains"},
				{ID: "b", Type: "bogus"},
				{ID: "c", Type: "regex"},
			},
			wantCount: 1,
			wantMsgs:  []string{`"bogus"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateEvalTypes(tt.defs, registry)
			if len(errs) != tt.wantCount {
				t.Errorf("got %d errors, want %d:\n%s", len(errs), tt.wantCount, strings.Join(errs, "\n"))
				return
			}
			for _, msg := range tt.wantMsgs {
				found := false
				for _, err := range errs {
					if strings.Contains(err, msg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got:\n%s", msg, strings.Join(errs, "\n"))
				}
			}
		})
	}
}

func TestValidateEvalTypes_ErrorIncludesRegisteredTypes(t *testing.T) {
	registry := NewEmptyEvalTypeRegistry()
	registry.Register(&stubHandler{typeName: "contains"})

	errs := ValidateEvalTypes([]EvalDef{{ID: "x", Type: "typo"}}, registry)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !strings.Contains(errs[0], "contains") {
		t.Errorf("error should list registered types, got: %s", errs[0])
	}
}

func TestValidateEvals_DisabledEvalStillValidated(t *testing.T) {
	defs := []EvalDef{{
		ID:      "test",
		Type:    "custom",
		Trigger: "bogus",
		Enabled: boolPtr(false),
	}}
	errs := ValidateEvals(defs, "pack")
	if len(errs) != 1 {
		t.Errorf("disabled eval should still be validated, got %d errors", len(errs))
	}
}
