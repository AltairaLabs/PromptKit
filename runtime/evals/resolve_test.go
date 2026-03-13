package evals

import (
	"testing"
)

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

func TestDefaultGroupsForType(t *testing.T) {
	tests := []struct {
		name       string
		evalType   string
		wantGroups []string
	}{
		{
			name:       "fast-running type (contains)",
			evalType:   "contains",
			wantGroups: []string{DefaultEvalGroup, GroupFastRunning},
		},
		{
			name:       "fast-running type (regex)",
			evalType:   "regex",
			wantGroups: []string{DefaultEvalGroup, GroupFastRunning},
		},
		{
			name:       "fast-running type (json_valid)",
			evalType:   "json_valid",
			wantGroups: []string{DefaultEvalGroup, GroupFastRunning},
		},
		{
			name:       "long-running and external (llm_judge)",
			evalType:   "llm_judge",
			wantGroups: []string{DefaultEvalGroup, GroupLongRunning, GroupExternal},
		},
		{
			name:       "long-running and external (rest_eval)",
			evalType:   "rest_eval",
			wantGroups: []string{DefaultEvalGroup, GroupLongRunning, GroupExternal},
		},
		{
			name:       "long-running and external (a2a_eval_session)",
			evalType:   "a2a_eval_session",
			wantGroups: []string{DefaultEvalGroup, GroupLongRunning, GroupExternal},
		},
		{
			name:       "long-running only (cosine_similarity)",
			evalType:   "cosine_similarity",
			wantGroups: []string{DefaultEvalGroup, GroupLongRunning},
		},
		{
			name:       "long-running only (outcome_equivalent)",
			evalType:   "outcome_equivalent",
			wantGroups: []string{DefaultEvalGroup, GroupLongRunning},
		},
		{
			name:       "unknown type defaults to fast-running",
			evalType:   "some_unknown_type",
			wantGroups: []string{DefaultEvalGroup, GroupFastRunning},
		},
		{
			name:       "empty type defaults to fast-running",
			evalType:   "",
			wantGroups: []string{DefaultEvalGroup, GroupFastRunning},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultGroupsForType(tt.evalType)
			if len(got) != len(tt.wantGroups) {
				t.Fatalf("length mismatch: got %v, want %v", got, tt.wantGroups)
			}
			for i, g := range got {
				if g != tt.wantGroups[i] {
					t.Errorf("index %d: got %q, want %q", i, g, tt.wantGroups[i])
				}
			}
		})
	}
}

func TestDefaultGroupsForType_CustomRegistration(t *testing.T) {
	// Register a custom type as long-running + external (simulates exec handler)
	RegisterTypeGroups("my_custom_exec", []string{GroupLongRunning, GroupExternal})
	defer func() {
		delete(customTypeGroups, "my_custom_exec")
	}()

	got := DefaultGroupsForType("my_custom_exec")
	want := []string{DefaultEvalGroup, GroupLongRunning, GroupExternal}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("index %d: got %q, want %q", i, g, want[i])
		}
	}
}

func TestGetGroups_WithTypeClassification(t *testing.T) {
	tests := []struct {
		name       string
		def        EvalDef
		wantGroups []string
	}{
		{
			name:       "no explicit groups, fast-running type",
			def:        EvalDef{Type: "contains"},
			wantGroups: []string{DefaultEvalGroup, GroupFastRunning},
		},
		{
			name:       "no explicit groups, long-running type",
			def:        EvalDef{Type: "llm_judge"},
			wantGroups: []string{DefaultEvalGroup, GroupLongRunning, GroupExternal},
		},
		{
			name:       "explicit groups override classification",
			def:        EvalDef{Type: "llm_judge", Groups: []string{"custom"}},
			wantGroups: []string{"custom"},
		},
		{
			name:       "explicit groups preserve user choice",
			def:        EvalDef{Type: "contains", Groups: []string{DefaultEvalGroup, "safety"}},
			wantGroups: []string{DefaultEvalGroup, "safety"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.def.GetGroups()
			if len(got) != len(tt.wantGroups) {
				t.Fatalf("length mismatch: got %v, want %v", got, tt.wantGroups)
			}
			for i, g := range got {
				if g != tt.wantGroups[i] {
					t.Errorf("index %d: got %q, want %q", i, g, tt.wantGroups[i])
				}
			}
		})
	}
}

func TestFilterByGroups(t *testing.T) {
	tests := []struct {
		name    string
		defs    []EvalDef
		groups  []string
		wantIDs []string
	}{
		{
			name:    "nil groups returns all",
			defs:    []EvalDef{{ID: "a"}, {ID: "b"}},
			groups:  nil,
			wantIDs: []string{"a", "b"},
		},
		{
			name:    "empty groups returns all",
			defs:    []EvalDef{{ID: "a"}, {ID: "b"}},
			groups:  []string{},
			wantIDs: []string{"a", "b"},
		},
		{
			name:    "empty defs returns empty",
			defs:    []EvalDef{},
			groups:  []string{"safety"},
			wantIDs: []string{},
		},
		{
			name: "default group matches evals with no explicit groups",
			defs: []EvalDef{
				{ID: "a"},                             // no groups → default
				{ID: "b", Groups: []string{"safety"}}, // explicit
			},
			groups:  []string{DefaultEvalGroup},
			wantIDs: []string{"a"},
		},
		{
			name: "single group filter",
			defs: []EvalDef{
				{ID: "a", Groups: []string{"safety"}},
				{ID: "b", Groups: []string{"quality"}},
				{ID: "c", Groups: []string{"safety", "quality"}},
			},
			groups:  []string{"safety"},
			wantIDs: []string{"a", "c"},
		},
		{
			name: "multiple groups filter",
			defs: []EvalDef{
				{ID: "a", Groups: []string{"safety"}},
				{ID: "b", Groups: []string{"quality"}},
				{ID: "c", Groups: []string{"latency"}},
			},
			groups:  []string{"safety", "quality"},
			wantIDs: []string{"a", "b"},
		},
		{
			name: "no match returns empty",
			defs: []EvalDef{
				{ID: "a", Groups: []string{"safety"}},
				{ID: "b", Groups: []string{"quality"}},
			},
			groups:  []string{"latency"},
			wantIDs: []string{},
		},
		{
			name: "eval in multiple groups matches any",
			defs: []EvalDef{
				{ID: "a", Groups: []string{"safety", "compliance"}},
			},
			groups:  []string{"compliance"},
			wantIDs: []string{"a"},
		},
		{
			name: "well-known group fast-running matches unclassified evals",
			defs: []EvalDef{
				{ID: "a", Type: "contains"},   // fast-running (auto)
				{ID: "b", Type: "llm_judge"},  // long-running + external (auto)
				{ID: "c", Type: "json_valid"}, // fast-running (auto)
			},
			groups:  []string{GroupFastRunning},
			wantIDs: []string{"a", "c"},
		},
		{
			name: "well-known group long-running matches LLM/external evals",
			defs: []EvalDef{
				{ID: "a", Type: "contains"},
				{ID: "b", Type: "llm_judge"},
				{ID: "c", Type: "cosine_similarity"},
			},
			groups:  []string{GroupLongRunning},
			wantIDs: []string{"b", "c"},
		},
		{
			name: "well-known group external matches external evals",
			defs: []EvalDef{
				{ID: "a", Type: "contains"},
				{ID: "b", Type: "llm_judge"},
				{ID: "c", Type: "cosine_similarity"},
				{ID: "d", Type: "rest_eval"},
			},
			groups:  []string{GroupExternal},
			wantIDs: []string{"b", "d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterByGroups(tt.defs, tt.groups)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tt.wantIDs))
			}
			for i, e := range got {
				if e.ID != tt.wantIDs[i] {
					t.Errorf("index %d: got ID %q, want %q", i, e.ID, tt.wantIDs[i])
				}
			}
		})
	}
}
