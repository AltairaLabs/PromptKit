package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// systemPromptJudge records the system prompt it was asked to grade with.
type systemPromptJudge struct {
	systemPrompt string
	called       bool
}

func (j *systemPromptJudge) Judge(_ context.Context, opts JudgeOpts) (*JudgeResult, error) {
	j.called = true
	j.systemPrompt = opts.SystemPrompt
	return &JudgeResult{Score: 1, Reasoning: "ok"}, nil
}

// judgeTemplateRegistry holds the prompt Arena's llm-judge example names in
// judge_defaults.prompt.
func judgeTemplateRegistry(t *testing.T) *prompt.Registry {
	t.Helper()
	reg := prompt.NewRegistryWithRepository(memory.NewPromptRepository())
	err := reg.RegisterConfig("judge-simple", &prompt.Config{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "PromptConfig",
		Spec: prompt.Spec{
			TaskType: "judge-simple",
			SystemTemplate: "JUDGE. CRITERIA: {{criteria}} RUBRIC: {{rubric}} " +
				"CONVERSATION: {{conversation}} RESPONSE: {{response}}",
			Variables: []prompt.VariableMetadata{
				{Name: "criteria", Required: true},
				{Name: "rubric", Required: false, Default: ""},
				{Name: "conversation", Required: false, Default: ""},
				{Name: "response", Required: true},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func judgeDefaultsCtx(judge JudgeProvider, meta map[string]any) *evals.EvalContext {
	md := map[string]any{"judge_provider": judge}
	for k, v := range meta {
		md[k] = v
	}
	return &evals.EvalContext{
		CurrentOutput: "Hold the power button for ten seconds.",
		Messages: []types.Message{
			{Role: "user", Content: "How do I reset my device?"},
			{Role: "assistant", Content: "Hold the power button for ten seconds."},
		},
		Metadata: md,
	}
}

// Arena passes judge_defaults (prompt, prompt_registry) and the prompt registry
// in eval metadata. The judge handlers used to ignore both and grade with the
// built-in system prompt.
func TestJudgeHandlers_UseJudgeDefaultsPrompt(t *testing.T) {
	reg := judgeTemplateRegistry(t)
	for _, h := range []evals.EvalTypeHandler{
		&LLMJudgeHandler{}, &LLMJudgeSessionHandler{}, &LLMJudgeToolCallsHandler{},
	} {
		t.Run(h.Type(), func(t *testing.T) {
			judge := &systemPromptJudge{}
			evalCtx := judgeDefaultsCtx(judge, map[string]any{
				"judge_defaults":  map[string]any{"prompt": "judge-simple", "prompt_registry": "./prompts"},
				"prompt_registry": reg,
			})
			evalCtx.ToolCalls = []evals.ToolCallRecord{{ToolName: "reset", Result: "done"}}
			result, err := h.Eval(context.Background(), evalCtx, map[string]any{"criteria": "actionable steps"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Error != "" || !judge.called {
				t.Fatalf("judge not called: %+v", result)
			}
			for _, want := range []string{
				"JUDGE.", "CRITERIA: actionable steps", "RESPONSE: ", "CONVERSATION: User: How do I reset my device?",
			} {
				if !strings.Contains(judge.systemPrompt, want) {
					t.Errorf("system prompt %q does not contain %q", judge.systemPrompt, want)
				}
			}
		})
	}
}

// The check's own system_prompt wins over judge_defaults.
func TestJudgeDefaultsPrompt_SystemPromptParamWins(t *testing.T) {
	judge := &systemPromptJudge{}
	evalCtx := judgeDefaultsCtx(judge, map[string]any{
		"judge_defaults":  map[string]any{"prompt": "judge-simple"},
		"prompt_registry": judgeTemplateRegistry(t),
	})
	_, err := (&LLMJudgeHandler{}).Eval(context.Background(), evalCtx,
		map[string]any{"criteria": "c", "system_prompt": "MINE"})
	if err != nil {
		t.Fatal(err)
	}
	if judge.systemPrompt != "MINE" {
		t.Fatalf("system prompt = %q, want the check's own", judge.systemPrompt)
	}
}

// Without judge_defaults the judge keeps its built-in prompt (empty
// SystemPrompt; judgeWithProvider fills in the default).
func TestJudgeDefaultsPrompt_AbsentKeepsDefault(t *testing.T) {
	for _, meta := range []map[string]any{
		nil,
		{"judge_defaults": map[string]any{"prompt": ""}},
		{"judge_defaults": map[string]any{"prompt_registry": "./prompts"}},
	} {
		judge := &systemPromptJudge{}
		_, err := (&LLMJudgeHandler{}).Eval(context.Background(), judgeDefaultsCtx(judge, meta),
			map[string]any{"criteria": "c"})
		if err != nil {
			t.Fatal(err)
		}
		if !judge.called || judge.systemPrompt != "" {
			t.Errorf("meta %v: system prompt = %q, called %v; want the built-in default", meta, judge.systemPrompt, judge.called)
		}
	}
}

// A named default prompt that cannot be used is an error, not a silent fall
// back to the built-in prompt.
func TestJudgeDefaultsPrompt_UnusableIsAnError(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want string
	}{
		{"no registry", map[string]any{"judge_defaults": map[string]any{"prompt": "judge-simple"}},
			`judge_defaults.prompt "judge-simple": no prompt registry`},
		{"unknown prompt", map[string]any{
			"judge_defaults":  map[string]any{"prompt": "nope"},
			"prompt_registry": judgeTemplateRegistry(t),
		}, `judge_defaults.prompt "nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			judge := &systemPromptJudge{}
			result, err := (&LLMJudgeHandler{}).Eval(context.Background(), judgeDefaultsCtx(judge, tt.meta),
				map[string]any{"criteria": "c"})
			if err != nil {
				t.Fatal(err)
			}
			if judge.called {
				t.Error("judge was called with an unusable default prompt")
			}
			if !strings.Contains(result.Error, tt.want) {
				t.Errorf("Error = %q, want it to contain %q", result.Error, tt.want)
			}
		})
	}
}
