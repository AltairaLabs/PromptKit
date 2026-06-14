package composition

import (
	"encoding/json"
	"testing"
)

func TestComposition_JSONRoundTrip(t *testing.T) {
	raw := `{
		"version": 1,
		"description": "analyze",
		"input_schema": "schemas/in.json",
		"output_schema": "schemas/out.json",
		"output": "synthesize",
		"engine": {"vendor": "x"},
		"steps": [
			{"id": "classify", "kind": "prompt", "prompt_task": "doc_classifier",
			 "input": "${input.text}", "output_schema": "schemas/doctype.json"},
			{"id": "route", "kind": "branch",
			 "predicate": {"path": "${classify.output.type}", "op": "equals", "value": "research_paper"},
			 "then": "extract", "else": "general"},
			{"id": "meta", "kind": "parallel",
			 "branches": [
				{"id": "structure", "kind": "tool", "tool": "doc.parse", "args": {"content": "${input.text}"}},
				{"id": "citations", "kind": "tool", "tool": "doc.cite", "args": {"content": "${input.text}"}}
			 ],
			 "reduce": {"strategy": "barrier", "into": "metadata"}},
			{"id": "synthesize", "kind": "agent", "prompt_task": "doc_analyzer",
			 "input": "${meta.output.metadata}", "tools": ["ref.search"],
			 "termination": {"max_steps": 10},
			 "modifiers": {"retry": {"max_attempts": 3}, "eval": ["analysis_quality"]}}
		]
	}`

	var c Composition
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Version != 1 {
		t.Errorf("version = %d, want 1", c.Version)
	}
	if c.Output != "synthesize" || len(c.Steps) != 4 {
		t.Fatalf("output=%q steps=%d", c.Output, len(c.Steps))
	}
	if c.Engine["vendor"] != "x" {
		t.Errorf("engine.vendor = %v, want x", c.Engine["vendor"])
	}
	if c.Steps[0].Kind != KindPrompt || c.Steps[0].PromptTask != "doc_classifier" {
		t.Errorf("step0 = %+v", c.Steps[0])
	}
	if c.Steps[1].Predicate.Op != "equals" || c.Steps[1].Then != "extract" {
		t.Errorf("branch = %+v", c.Steps[1])
	}
	if c.Steps[2].Reduce.Strategy != ReduceBarrier || c.Steps[2].Reduce.Into != "metadata" {
		t.Errorf("reduce = %+v", c.Steps[2].Reduce)
	}
	if len(c.Steps[2].Branches) != 2 {
		t.Errorf("branches = %d", len(c.Steps[2].Branches))
	}
	if c.Steps[3].Termination.MaxSteps != 10 || c.Steps[3].Modifiers.Retry.MaxAttempts != 3 {
		t.Errorf("agent = %+v", c.Steps[3])
	}
	if len(c.Steps[3].Modifiers.Eval) != 1 || c.Steps[3].Modifiers.Eval[0] != "analysis_quality" {
		t.Errorf("eval = %+v", c.Steps[3].Modifiers.Eval)
	}
}

func TestPredicate_ExistsVariant(t *testing.T) {
	var p Predicate
	if err := json.Unmarshal([]byte(`{"path":"${a.output.x}","exists":true}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Exists == nil || !*p.Exists {
		t.Fatalf("exists not parsed: %+v", p)
	}
}
