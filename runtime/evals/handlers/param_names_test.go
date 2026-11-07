package handlers

import (
	"slices"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// Every registered built-in type, alias and wrapper declares the params it
// reads, so load-time validation can reject a key it would ignore.
func TestBuiltinHandlersDeclareParamNames(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()
	for _, typ := range reg.Types() {
		if _, ok := reg.ParamNames(typ); !ok {
			t.Errorf("%s: handler does not implement evals.ParamNamer", typ)
		}
	}
}

// A default the registry adds and a canonical name a legacy param is rewritten
// to are params the handler reads: if either were missing from ParamNames,
// every declaration of that type would fail to load.
func TestParamNamesCoverDefaultsAndAliases(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()
	for typ, defaults := range evals.ParamDefaults {
		names, _ := reg.ParamNames(typ)
		for key := range defaults {
			if !slices.Contains(names, key) {
				t.Errorf("%s: default param %q is not in ParamNames %v", typ, key, names)
			}
		}
	}
	for typ, aliases := range evals.ParamAliases {
		names, _ := reg.ParamNames(typ)
		for legacy, canonical := range aliases {
			if !slices.Contains(names, canonical) {
				t.Errorf("%s: %q normalizes to %q, which is not in ParamNames %v", typ, legacy, canonical, names)
			}
		}
	}
}

// The judge handlers used to accept any key, so `critera:` loaded clean and the
// judge graded with no criteria at all.
func TestJudgeHandlers_RejectUnknownParamsAtLoad(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()
	for _, typ := range []string{"llm_judge", "llm_judge_session", "llm_judge_conversation", "llm_judge_tool_calls"} {
		errs := evals.ValidateEvalTypes([]evals.EvalDef{
			{ID: "ok", Type: typ, Params: map[string]any{
				"criteria": "c", "rubric": "r", "system_prompt": "s", "extra": map[string]any{}, "provider": "j",
			}},
			{ID: "typo", Type: typ, Params: map[string]any{"critera": "c"}},
		}, reg)
		if len(errs) != 1 {
			t.Fatalf("%s: got %v, want exactly the typo rejected", typ, errs)
		}
		if !strings.Contains(errs[0], `eval "typo": unknown param "critera"`) ||
			!strings.Contains(errs[0], "valid params: criteria, extra, provider, rubric, system_prompt") {
			t.Errorf("%s: error %q does not name the bad key and the valid ones", typ, errs[0])
		}
	}
}

// Each family's real params load clean; a typo in each is rejected.
func TestParamNames_RepresentativeConfigs(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()
	valid := []evals.EvalDef{
		{ID: "banned", Type: "banned_words", Params: map[string]any{"words": []any{"x"}, "case_sensitive": true}},
		{ID: "maxlen", Type: "length", Params: map[string]any{"max_characters": 10, "max_tokens": 3}},
		{ID: "sentences", Type: "max_sentences", Params: map[string]any{"max_sentences": 3}},
		{ID: "fields", Type: "required_fields", Params: map[string]any{"required_fields": []any{"a"}}},
		{ID: "topic", Type: "topic_policy", Params: map[string]any{"description": "d", "allowed": []any{"a"}}},
		{ID: "rag", Type: "contextual_recall", Params: map[string]any{"contexts": []any{"c"}, "reference": "r"}},
		{ID: "classify", Type: "text_toxicity", Params: map[string]any{"model": "m", "expected_label": "toxic"}},
		{ID: "exec", Type: "tool_exec", Params: map[string]any{"tool": "t", "score_path": "a.b", "score_scale": 100}},
		{ID: "rest", Type: "rest_eval", Params: map[string]any{"url": "http://x", "include_tool_calls": true}},
		{ID: "done", Type: "workflow_complete", Params: map[string]any{}},
	}
	if errs := evals.ValidateEvalTypes(valid, reg); len(errs) != 0 {
		t.Fatalf("valid configs rejected: %v", errs)
	}
	typos := []evals.EvalDef{
		{ID: "contains", Type: "contains", Params: map[string]any{"pattern": "x"}},
		{ID: "tools", Type: "tools_called", Params: map[string]any{"tool_names": []any{"a"}, "min_call": 1}},
		{ID: "done", Type: "workflow_complete", Params: map[string]any{"state": "x"}},
	}
	if errs := evals.ValidateEvalTypes(typos, reg); len(errs) != len(typos) {
		t.Fatalf("got %d errors, want %d: %v", len(errs), len(typos), errs)
	}
}
