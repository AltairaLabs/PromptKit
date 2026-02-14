package evals

import (
	"testing"
)

func boolPtr(b bool) *bool       { return &b }
func float64Ptr(f float64) *float64 { return &f }

func TestResolveEvals(t *testing.T) {
	tests := []struct {
		name        string
		packEvals   []EvalDef
		promptEvals []EvalDef
		wantIDs     []string // expected IDs in order
		wantTypes   []string // expected Type values in order (to verify overrides)
	}{
		{
			name:        "empty pack and empty prompt",
			packEvals:   []EvalDef{},
			promptEvals: []EvalDef{},
			wantIDs:     nil,
			wantTypes:   nil,
		},
		{
			name:        "nil pack and nil prompt",
			packEvals:   nil,
			promptEvals: nil,
			wantIDs:     nil,
			wantTypes:   nil,
		},
		{
			name: "pack only no prompt",
			packEvals: []EvalDef{
				{ID: "tone", Type: "llm_judge"},
				{ID: "latency", Type: "latency"},
			},
			promptEvals: nil,
			wantIDs:     []string{"tone", "latency"},
			wantTypes:   []string{"llm_judge", "latency"},
		},
		{
			name:      "prompt only no pack",
			packEvals: nil,
			promptEvals: []EvalDef{
				{ID: "accuracy", Type: "llm_judge"},
			},
			wantIDs:   []string{"accuracy"},
			wantTypes: []string{"llm_judge"},
		},
		{
			name: "override by ID",
			packEvals: []EvalDef{
				{ID: "tone", Type: "llm_judge", Trigger: TriggerEveryTurn},
				{ID: "latency", Type: "latency", Trigger: TriggerEveryTurn},
			},
			promptEvals: []EvalDef{
				{ID: "tone", Type: "custom_tone", Trigger: TriggerSampleTurns},
			},
			wantIDs:   []string{"tone", "latency"},
			wantTypes: []string{"custom_tone", "latency"},
		},
		{
			name: "mix of overrides and additions",
			packEvals: []EvalDef{
				{ID: "tone", Type: "llm_judge"},
				{ID: "latency", Type: "latency"},
				{ID: "safety", Type: "keyword"},
			},
			promptEvals: []EvalDef{
				{ID: "tone", Type: "custom_tone"},
				{ID: "accuracy", Type: "llm_judge"},
				{ID: "format", Type: "regex"},
			},
			wantIDs:   []string{"tone", "latency", "safety", "accuracy", "format"},
			wantTypes: []string{"custom_tone", "latency", "keyword", "llm_judge", "regex"},
		},
		{
			name: "ordering preserved pack first then prompt additions",
			packEvals: []EvalDef{
				{ID: "c", Type: "t1"},
				{ID: "a", Type: "t2"},
				{ID: "b", Type: "t3"},
			},
			promptEvals: []EvalDef{
				{ID: "d", Type: "t4"},
				{ID: "a", Type: "t5"},
				{ID: "e", Type: "t6"},
			},
			wantIDs:   []string{"c", "a", "b", "d", "e"},
			wantTypes: []string{"t1", "t5", "t3", "t4", "t6"},
		},
		{
			name:      "empty pack with prompt evals",
			packEvals: []EvalDef{},
			promptEvals: []EvalDef{
				{ID: "x", Type: "t1"},
			},
			wantIDs:   []string{"x"},
			wantTypes: []string{"t1"},
		},
		{
			name: "override preserves prompt eval fields",
			packEvals: []EvalDef{
				{ID: "tone", Type: "llm_judge", Enabled: boolPtr(true), SamplePercentage: float64Ptr(10.0)},
			},
			promptEvals: []EvalDef{
				{ID: "tone", Type: "llm_judge", Enabled: boolPtr(false), SamplePercentage: float64Ptr(50.0)},
			},
			wantIDs:   []string{"tone"},
			wantTypes: []string{"llm_judge"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveEvals(tt.packEvals, tt.promptEvals)

			// Check nil vs empty.
			if tt.wantIDs == nil {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}

			if len(got) != len(tt.wantIDs) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tt.wantIDs))
			}

			for i, e := range got {
				if e.ID != tt.wantIDs[i] {
					t.Errorf("index %d: got ID %q, want %q", i, e.ID, tt.wantIDs[i])
				}
				if e.Type != tt.wantTypes[i] {
					t.Errorf("index %d: got Type %q, want %q", i, e.Type, tt.wantTypes[i])
				}
			}
		})
	}
}

func TestResolveEvals_OverridePreservesFields(t *testing.T) {
	pack := []EvalDef{
		{
			ID:               "tone",
			Type:             "llm_judge",
			Trigger:          TriggerEveryTurn,
			Enabled:          boolPtr(true),
			SamplePercentage: float64Ptr(10.0),
			Params:           map[string]any{"model": "gpt-4"},
		},
	}
	prompt := []EvalDef{
		{
			ID:               "tone",
			Type:             "llm_judge",
			Trigger:          TriggerSampleTurns,
			Enabled:          boolPtr(false),
			SamplePercentage: float64Ptr(50.0),
			Params:           map[string]any{"model": "claude-3"},
		},
	}

	got := ResolveEvals(pack, prompt)
	if len(got) != 1 {
		t.Fatalf("expected 1 eval, got %d", len(got))
	}

	e := got[0]
	if e.Trigger != TriggerSampleTurns {
		t.Errorf("trigger: got %q, want %q", e.Trigger, TriggerSampleTurns)
	}
	if e.IsEnabled() {
		t.Error("expected eval to be disabled after override")
	}
	if e.GetSamplePercentage() != 50.0 {
		t.Errorf("sample percentage: got %f, want 50.0", e.GetSamplePercentage())
	}
	if e.Params["model"] != "claude-3" {
		t.Errorf("params model: got %v, want claude-3", e.Params["model"])
	}
}
