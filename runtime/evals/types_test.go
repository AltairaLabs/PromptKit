package evals

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func ptr[T any](v T) *T {
	return &v
}

func TestEvalDef_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", ptr(true), true},
		{"explicit false", ptr(false), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &EvalDef{Enabled: tt.enabled}
			if got := e.IsEnabled(); got != tt.want {
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
		{"explicit 10", ptr(10.0), 10.0},
		{"explicit 0", ptr(0.0), 0.0},
		{"explicit 100", ptr(100.0), 100.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &EvalDef{SamplePercentage: tt.pct}
			if got := e.GetSamplePercentage(); got != tt.want {
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
		Enabled:          ptr(true),
		SamplePercentage: ptr(10.0),
		Metric: &MetricDef{
			Name:  "promptpack_tone_score",
			Type:  MetricGauge,
			Range: &Range{Min: ptr(0.0), Max: ptr(1.0)},
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
	if e.IsEnabled() != true {
		t.Error("IsEnabled() should default to true")
	}
	if e.GetSamplePercentage() != DefaultSamplePercentage {
		t.Errorf("GetSamplePercentage() = %v, want %v", e.GetSamplePercentage(), DefaultSamplePercentage)
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
		Passed:      true,
		Score:       ptr(0.95),
		MetricValue: ptr(0.95),
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
	if decoded.Passed != r.Passed {
		t.Errorf("Passed = %v, want %v", decoded.Passed, r.Passed)
	}
	if decoded.Score == nil || *decoded.Score != *r.Score {
		t.Errorf("Score = %v, want %v", decoded.Score, r.Score)
	}
}

func TestEvalResult_ErrorField(t *testing.T) {
	r := EvalResult{
		EvalID:     "broken",
		Type:       "contains",
		Passed:     false,
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
		Passed:     true,
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
	if e.IsEnabled() {
		t.Error("IsEnabled() should be false for explicit false")
	}
}

func TestThreshold_Apply(t *testing.T) {
	tests := []struct {
		name       string
		threshold  *Threshold
		result     EvalResult
		wantPassed bool
	}{
		{
			name:       "nil threshold is no-op",
			threshold:  nil,
			result:     EvalResult{Passed: true, Score: ptr(0.5)},
			wantPassed: true,
		},
		{
			name:       "passed required but result failed",
			threshold:  &Threshold{Passed: ptr(true)},
			result:     EvalResult{Passed: false, Score: ptr(0.9)},
			wantPassed: false,
		},
		{
			name:       "min_score met",
			threshold:  &Threshold{MinScore: ptr(0.7)},
			result:     EvalResult{Passed: true, Score: ptr(0.8)},
			wantPassed: true,
		},
		{
			name:       "min_score not met",
			threshold:  &Threshold{MinScore: ptr(0.7)},
			result:     EvalResult{Passed: true, Score: ptr(0.5)},
			wantPassed: false,
		},
		{
			name:       "max_score met",
			threshold:  &Threshold{MaxScore: ptr(0.9)},
			result:     EvalResult{Passed: true, Score: ptr(0.8)},
			wantPassed: true,
		},
		{
			name:       "max_score exceeded",
			threshold:  &Threshold{MaxScore: ptr(0.9)},
			result:     EvalResult{Passed: true, Score: ptr(0.95)},
			wantPassed: false,
		},
		{
			name:       "nil score with min_score is no-op",
			threshold:  &Threshold{MinScore: ptr(0.7)},
			result:     EvalResult{Passed: true},
			wantPassed: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.result
			tt.threshold.Apply(&result)
			if result.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v", result.Passed, tt.wantPassed)
			}
		})
	}
}

func TestThreshold_JSON(t *testing.T) {
	th := Threshold{
		Passed:   ptr(true),
		MinScore: ptr(0.7),
		MaxScore: ptr(0.95),
	}
	data, err := json.Marshal(th)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Threshold
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Passed == nil || !*decoded.Passed {
		t.Error("Passed should be true")
	}
	if decoded.MinScore == nil || *decoded.MinScore != 0.7 {
		t.Errorf("MinScore = %v, want 0.7", decoded.MinScore)
	}
	if decoded.MaxScore == nil || *decoded.MaxScore != 0.95 {
		t.Errorf("MaxScore = %v, want 0.95", decoded.MaxScore)
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
		Passed:  false,
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
		Passed:     true,
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
		Threshold: &Threshold{
			Passed: ptr(true),
		},
		When: &EvalWhen{
			ToolCalled: "search",
		},
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
	if decoded.Threshold.Passed == nil || !*decoded.Threshold.Passed {
		t.Error("Threshold.Passed should be true")
	}
	if decoded.When == nil {
		t.Fatal("When is nil")
	}
	if decoded.When.ToolCalled != "search" {
		t.Errorf("When.ToolCalled = %q, want %q", decoded.When.ToolCalled, "search")
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
		{"both min and max", `{"min":0,"max":1}`, ptr(0.0), ptr(1.0)},
		{"only min", `{"min":-1}`, ptr(-1.0), nil},
		{"only max", `{"max":100}`, nil, ptr(100.0)},
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
