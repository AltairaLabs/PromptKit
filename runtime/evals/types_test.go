package evals

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestEvalDef_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", testutil.Ptr(true), true},
		{"explicit false", testutil.Ptr(false), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &EvalDef{Enabled: tt.enabled}
			if got := IsEnabled(e); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvalDef_GetSamplePercentage(t *testing.T) {
	tests := []struct {
		name string
		pct  *float64
		want float64
	}{
		{"nil defaults to 5.0", nil, DefaultSamplePercentage},
		{"explicit 10", testutil.Ptr(10.0), 10.0},
		{"explicit 0", testutil.Ptr(0.0), 0.0},
		{"explicit 100", testutil.Ptr(100.0), 100.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &EvalDef{SamplePercentage: tt.pct}
			if got := SamplePercentage(e); got != tt.want {
				t.Errorf("GetSamplePercentage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvalDef_JSONRoundTrip(t *testing.T) {
	original := EvalDef{
		ID:               "tone-check",
		Type:             "llm_judge",
		Trigger:          TriggerSampleTurns,
		Params:           map[string]any{"criteria": "professional tone"},
		Description:      "Check tone",
		Enabled:          testutil.Ptr(true),
		SamplePercentage: testutil.Ptr(10.0),
		Metric: &MetricDef{
			Name:  "promptpack_tone_score",
			Type:  MetricGauge,
			Range: &Range{Min: testutil.Ptr(0.0), Max: testutil.Ptr(1.0)},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded EvalDef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Trigger != original.Trigger {
		t.Errorf("Trigger = %q, want %q", decoded.Trigger, original.Trigger)
	}
	if decoded.Metric == nil {
		t.Fatal("Metric is nil after round-trip")
	}
	if decoded.Metric.Name != original.Metric.Name {
		t.Errorf("Metric.Name = %q, want %q", decoded.Metric.Name, original.Metric.Name)
	}
	if decoded.Metric.Type != original.Metric.Type {
		t.Errorf("Metric.Type = %q, want %q", decoded.Metric.Type, original.Metric.Type)
	}
	if decoded.Metric.Range == nil {
		t.Fatal("Metric.Range is nil after round-trip")
	}
	if *decoded.Metric.Range.Min != *original.Metric.Range.Min {
		t.Errorf("Range.Min = %v, want %v", *decoded.Metric.Range.Min, *original.Metric.Range.Min)
	}
	if *decoded.Metric.Range.Max != *original.Metric.Range.Max {
		t.Errorf("Range.Max = %v, want %v", *decoded.Metric.Range.Max, *original.Metric.Range.Max)
	}
}

func TestEvalDef_JSONMinimal(t *testing.T) {
	// Minimal required fields only
	input := `{"id":"check","type":"contains","trigger":"every_turn","params":{"text":"hello"}}`
	var e EvalDef
	if err := json.Unmarshal([]byte(input), &e); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if e.ID != "check" {
		t.Errorf("ID = %q, want %q", e.ID, "check")
	}
	if IsEnabled(&e) != true {
		t.Error("IsEnabled() should default to true")
	}
	if SamplePercentage(&e) != DefaultSamplePercentage {
		t.Errorf("GetSamplePercentage() = %v, want %v", SamplePercentage(&e), DefaultSamplePercentage)
	}
	if e.Metric != nil {
		t.Error("Metric should be nil for minimal input")
	}
}

func TestMetricDef_ExtraFieldsRoundTrip(t *testing.T) {
	input := `{
		"name": "my_metric",
		"type": "gauge",
		"range": {"min": 0, "max": 100},
		"custom_field": "custom_value",
		"another": 42
	}`

	var m MetricDef
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if m.Name != "my_metric" {
		t.Errorf("Name = %q, want %q", m.Name, "my_metric")
	}
	if m.Type != MetricGauge {
		t.Errorf("Type = %q, want %q", m.Type, MetricGauge)
	}
	if m.Extra == nil {
		t.Fatal("Extra is nil, expected custom fields")
	}
	if m.Extra["custom_field"] != "custom_value" {
		t.Errorf("Extra[custom_field] = %v, want %q", m.Extra["custom_field"], "custom_value")
	}
	// JSON numbers unmarshal to float64
	if m.Extra["another"] != float64(42) {
		t.Errorf("Extra[another] = %v, want 42", m.Extra["another"])
	}

	// Round-trip
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m2 MetricDef
	if err := json.Unmarshal(data, &m2); err != nil {
		t.Fatalf("Unmarshal round-trip: %v", err)
	}
	if m2.Extra["custom_field"] != "custom_value" {
		t.Errorf("Round-trip Extra[custom_field] = %v, want %q", m2.Extra["custom_field"], "custom_value")
	}
	if m2.Extra["another"] != float64(42) {
		t.Errorf("Round-trip Extra[another] = %v, want 42", m2.Extra["another"])
	}
	if m2.Name != "my_metric" {
		t.Errorf("Round-trip Name = %q, want %q", m2.Name, "my_metric")
	}
}

func TestMetricDef_NoExtra(t *testing.T) {
	input := `{"name": "simple", "type": "counter"}`
	var m MetricDef
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.Extra != nil {
		t.Errorf("Extra should be nil when no extra fields, got %v", m.Extra)
	}
}

func TestMetricDef_ExtraDoesNotOverrideKnown(t *testing.T) {
	// Extra fields named "name", "type", "range" should not be included in Extra
	m := MetricDef{
		Name: "test",
		Type: MetricBoolean,
		Extra: map[string]any{
			"name":    "should_be_ignored",
			"type":    "should_be_ignored",
			"range":   "should_be_ignored",
			"allowed": "yes",
		},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}

	// Known fields should use struct values, not Extra
	if raw["name"] != "test" {
		t.Errorf("name = %v, want %q (struct value should win)", raw["name"], "test")
	}
	if raw["type"] != string(MetricBoolean) {
		t.Errorf("type = %v, want %q", raw["type"], MetricBoolean)
	}
	if raw["allowed"] != "yes" {
		t.Errorf("allowed = %v, want %q", raw["allowed"], "yes")
	}
}

func TestEvalResult_JSON(t *testing.T) {
	r := EvalResult{
		EvalID:      "tone-check",
		Type:        "llm_judge",
		Score:       testutil.Ptr(0.95),
		MetricValue: testutil.Ptr(0.95),
		Explanation: "Tone is professional",
		DurationMs:  150,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded EvalResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.EvalID != r.EvalID {
		t.Errorf("EvalID = %q, want %q", decoded.EvalID, r.EvalID)
	}
	if decoded.Score == nil || *decoded.Score != *r.Score {
		t.Errorf("Score = %v, want %v", decoded.Score, r.Score)
	}
}

func TestEvalResult_ErrorField(t *testing.T) {
	r := EvalResult{
		EvalID:     "broken",
		Type:       "contains",
		DurationMs: 5,
		Error:      "handler panicked",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EvalResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Error != "handler panicked" {
		t.Errorf("Error = %q, want %q", decoded.Error, "handler panicked")
	}
}

func TestEvalResult_OmitsNilOptionals(t *testing.T) {
	r := EvalResult{
		EvalID:     "check",
		Type:       "regex",
		DurationMs: 3,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	if _, ok := raw["score"]; ok {
		t.Error("score should be omitted when nil")
	}
	if _, ok := raw["metric_value"]; ok {
		t.Error("metric_value should be omitted when nil")
	}
	if _, ok := raw["explanation"]; ok {
		t.Error("explanation should be omitted when empty")
	}
	if _, ok := raw["error"]; ok {
		t.Error("error should be omitted when empty")
	}
}

func TestEvalContext_JSON(t *testing.T) {
	ctx := EvalContext{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
		TurnIndex:     1,
		CurrentOutput: "Hi there",
		SessionID:     "sess-123",
		PromptID:      "chat",
		ToolCalls: []ToolCallRecord{
			{
				TurnIndex: 1,
				ToolName:  "search",
				Arguments: map[string]any{"query": "test"},
			},
		},
		Variables: map[string]any{"user_name": "Alice"},
		Metadata:  map[string]any{"source": "test"},
	}

	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded EvalContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(decoded.Messages) != 2 {
		t.Errorf("Messages len = %d, want 2", len(decoded.Messages))
	}
	if decoded.TurnIndex != 1 {
		t.Errorf("TurnIndex = %d, want 1", decoded.TurnIndex)
	}
	if decoded.CurrentOutput != "Hi there" {
		t.Errorf("CurrentOutput = %q, want %q", decoded.CurrentOutput, "Hi there")
	}
	if decoded.SessionID != "sess-123" {
		t.Errorf("SessionID = %q, want %q", decoded.SessionID, "sess-123")
	}
	if len(decoded.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(decoded.ToolCalls))
	}
	if decoded.ToolCalls[0].ToolName != "search" {
		t.Errorf("ToolCalls[0].ToolName = %q, want %q", decoded.ToolCalls[0].ToolName, "search")
	}
}

func TestToolCallRecord_JSON(t *testing.T) {
	tc := ToolCallRecord{
		TurnIndex: 2,
		ToolName:  "create_ticket",
		Arguments: map[string]any{"title": "Bug fix", "priority": "high"},
		Result:    map[string]any{"id": "T-123"},
		Error:     "",
	}
	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ToolCallRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ToolName != "create_ticket" {
		t.Errorf("ToolName = %q, want %q", decoded.ToolName, "create_ticket")
	}
	if decoded.TurnIndex != 2 {
		t.Errorf("TurnIndex = %d, want 2", decoded.TurnIndex)
	}
}

func TestValidTriggers(t *testing.T) {
	expected := []EvalTrigger{
		TriggerEveryTurn,
		TriggerOnSessionComplete,
		TriggerSampleTurns,
		TriggerSampleSessions,
	}
	for _, trigger := range expected {
		if !ValidTriggers[trigger] {
			t.Errorf("ValidTriggers missing %q", trigger)
		}
	}
	if ValidTriggers["invalid_trigger"] {
		t.Error("ValidTriggers should not contain invalid trigger")
	}
}

func TestValidMetricTypes(t *testing.T) {
	expected := []MetricType{MetricGauge, MetricCounter, MetricHistogram, MetricBoolean}
	for _, mt := range expected {
		if !ValidMetricTypes[mt] {
			t.Errorf("ValidMetricTypes missing %q", mt)
		}
	}
	if ValidMetricTypes["invalid"] {
		t.Error("ValidMetricTypes should not contain invalid type")
	}
}

func TestEvalDef_DisabledExplicit(t *testing.T) {
	input := `{"id":"x","type":"y","trigger":"every_turn","params":{},"enabled":false}`
	var e EvalDef
	if err := json.Unmarshal([]byte(input), &e); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if IsEnabled(&e) {
		t.Error("IsEnabled() should be false for explicit false")
	}
}

// TestThreshold_JSON pins the threshold to the vocabulary the spec defines.
//
// This replaces a test that asserted {passed, min_score, max_score}, which was
// promptkit's own invention: $defs/Eval.threshold is additionalProperties:false
// with exactly {operator, value}, so the old shape emitted a document the schema
// REJECTED, and a spec-authored threshold loaded as all-nil. The old test passed
// throughout, because it only ever round-tripped promptkit's shape against
// itself.
func TestThreshold_JSON(t *testing.T) {
	th := Threshold{Operator: "gte", Value: testutil.Ptr(0.7)}

	data, err := json.Marshal(th)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// The keys matter, not just the round trip — that is what the old test
	// could not catch.
	if got := string(data); got != `{"operator":"gte","value":0.7}` {
		t.Errorf("threshold must emit the spec's keys, got %s", got)
	}

	var decoded Threshold
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Operator != "gte" {
		t.Errorf("Operator = %q, want gte", decoded.Operator)
	}
	if decoded.Value == nil || *decoded.Value != 0.7 {
		t.Errorf("Value = %v, want 0.7", decoded.Value)
	}
}

// TestThresholdLoadsTheSpecVocabulary — a pack authored against the spec must
// arrive intact. It previously loaded as an all-nil Threshold, silently.
func TestThresholdLoadsTheSpecVocabulary(t *testing.T) {
	var th Threshold
	if err := json.Unmarshal([]byte(`{"operator":"lte","value":0.25}`), &th); err != nil {
		t.Fatal(err)
	}
	if th.Operator != "lte" || th.Value == nil || *th.Value != 0.25 {
		t.Errorf("spec-authored threshold dropped: %+v", th)
	}
}

func TestEvalWhen_JSON(t *testing.T) {
	w := EvalWhen{
		ToolCalled:        "search",
		ToolCalledPattern: "search_.*",
		AnyToolCalled:     true,
		MinToolCalls:      2,
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EvalWhen
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ToolCalled != "search" {
		t.Errorf("ToolCalled = %q, want %q", decoded.ToolCalled, "search")
	}
	if decoded.ToolCalledPattern != "search_.*" {
		t.Errorf("ToolCalledPattern = %q, want %q", decoded.ToolCalledPattern, "search_.*")
	}
	if !decoded.AnyToolCalled {
		t.Error("AnyToolCalled should be true")
	}
	if decoded.MinToolCalls != 2 {
		t.Errorf("MinToolCalls = %d, want 2", decoded.MinToolCalls)
	}
}

func TestEvalWhen_OmitsZeroValues(t *testing.T) {
	w := EvalWhen{}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	if _, ok := raw["tool_called"]; ok {
		t.Error("tool_called should be omitted when empty")
	}
	if _, ok := raw["tool_called_pattern"]; ok {
		t.Error("tool_called_pattern should be omitted when empty")
	}
	if _, ok := raw["any_tool_called"]; ok {
		t.Error("any_tool_called should be omitted when false")
	}
	if _, ok := raw["min_tool_calls"]; ok {
		t.Error("min_tool_calls should be omitted when zero")
	}
}

func TestEvalViolation_JSON(t *testing.T) {
	v := EvalViolation{
		TurnIndex:   3,
		Description: "Forbidden tool argument used",
		Evidence:    map[string]any{"arg": "password", "value": "***"},
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EvalViolation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.TurnIndex != 3 {
		t.Errorf("TurnIndex = %d, want 3", decoded.TurnIndex)
	}
	if decoded.Description != "Forbidden tool argument used" {
		t.Errorf("Description = %q, want %q", decoded.Description, "Forbidden tool argument used")
	}
	if decoded.Evidence["arg"] != "password" {
		t.Errorf("Evidence[arg] = %v, want %q", decoded.Evidence["arg"], "password")
	}
}

func TestEvalResult_ExtendedFields(t *testing.T) {
	r := EvalResult{
		EvalID:  "check",
		Type:    "test",
		Message: "assertion failed",
		Details: map[string]any{"expected": "foo", "got": "bar"},
		Violations: []EvalViolation{
			{TurnIndex: 1, Description: "mismatch"},
		},
		Skipped:    true,
		SkipReason: "tool not called",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EvalResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Message != "assertion failed" {
		t.Errorf("Message = %q, want %q", decoded.Message, "assertion failed")
	}
	if decoded.Details["expected"] != "foo" {
		t.Errorf("Details[expected] = %v, want %q", decoded.Details["expected"], "foo")
	}
	if len(decoded.Violations) != 1 {
		t.Fatalf("Violations len = %d, want 1", len(decoded.Violations))
	}
	if decoded.Violations[0].TurnIndex != 1 {
		t.Errorf("Violations[0].TurnIndex = %d, want 1", decoded.Violations[0].TurnIndex)
	}
	if !decoded.Skipped {
		t.Error("Skipped should be true")
	}
	if decoded.SkipReason != "tool not called" {
		t.Errorf("SkipReason = %q, want %q", decoded.SkipReason, "tool not called")
	}
}

func TestEvalResult_OmitsNewOptionals(t *testing.T) {
	r := EvalResult{
		EvalID:     "check",
		Type:       "test",
		DurationMs: 3,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	for _, field := range []string{"message", "details", "violations", "skipped", "skip_reason"} {
		if _, ok := raw[field]; ok {
			t.Errorf("%s should be omitted when zero-value", field)
		}
	}
}

func TestEvalDef_ExtendedFieldsJSON(t *testing.T) {
	def := EvalDef{
		ID:      "check",
		Type:    "contains",
		Trigger: TriggerEveryTurn,
		Params:  map[string]any{"text": "hello"},
		Message: "should contain hello",
		Threshold: &Threshold{Operator: "gte", Value: testutil.Ptr(0.8)},
		// `when` is additionalProperties:true in the spec, so the field is the
		// raw map and EvalWhen is promptkit's reading of it.
		When: map[string]any{"tool_called": "search"},
	}
	data, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EvalDef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Message != "should contain hello" {
		t.Errorf("Message = %q, want %q", decoded.Message, "should contain hello")
	}
	if decoded.Threshold == nil {
		t.Fatal("Threshold is nil")
	}
	if decoded.Threshold.Operator != "gte" ||
		decoded.Threshold.Value == nil || *decoded.Threshold.Value != 0.8 {
		t.Errorf("Threshold = %+v, want {gte 0.8}", decoded.Threshold)
	}
	when := DecodeEvalWhen(decoded.When)
	if when == nil {
		t.Fatal("When is nil")
	}
	if when.ToolCalled != "search" {
		t.Errorf("When.ToolCalled = %q, want %q", when.ToolCalled, "search")
	}
}

func TestEvalContext_Extras(t *testing.T) {
	ctx := EvalContext{
		Messages:      []types.Message{{Role: "user", Content: "hi"}},
		TurnIndex:     0,
		CurrentOutput: "hello",
		SessionID:     "s1",
		PromptID:      "p1",
		Extras:        map[string]any{"workflow_state": "greeting"},
	}
	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EvalContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Extras["workflow_state"] != "greeting" {
		t.Errorf("Extras[workflow_state] = %v, want %q", decoded.Extras["workflow_state"], "greeting")
	}
}

func TestValidTriggers_NewTriggers(t *testing.T) {
	newTriggers := []EvalTrigger{
		TriggerOnConversationComplete,
		TriggerOnWorkflowStep,
	}
	for _, trigger := range newTriggers {
		if !ValidTriggers[trigger] {
			t.Errorf("ValidTriggers missing %q", trigger)
		}
	}
}

func TestRange_JSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		min   *float64
		max   *float64
	}{
		{"both min and max", `{"min":0,"max":1}`, testutil.Ptr(0.0), testutil.Ptr(1.0)},
		{"only min", `{"min":-1}`, testutil.Ptr(-1.0), nil},
		{"only max", `{"max":100}`, nil, testutil.Ptr(100.0)},
		{"empty", `{}`, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r Range
			if err := json.Unmarshal([]byte(tt.input), &r); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if tt.min == nil && r.Min != nil {
				t.Errorf("Min = %v, want nil", r.Min)
			}
			if tt.min != nil {
				if r.Min == nil {
					t.Fatal("Min is nil, want non-nil")
				}
				if *r.Min != *tt.min {
					t.Errorf("Min = %v, want %v", *r.Min, *tt.min)
				}
			}
			if tt.max == nil && r.Max != nil {
				t.Errorf("Max = %v, want nil", r.Max)
			}
			if tt.max != nil {
				if r.Max == nil {
					t.Fatal("Max is nil, want non-nil")
				}
				if *r.Max != *tt.max {
					t.Errorf("Max = %v, want %v", *r.Max, *tt.max)
				}
			}
		})
	}
}
