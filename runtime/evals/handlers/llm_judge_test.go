package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// llmJudgeMock is a test double for JudgeProvider that captures
// the opts passed to Judge.
type llmJudgeMock struct {
	result *JudgeResult
	err    error
	opts   JudgeOpts // captures last call
}

func (m *llmJudgeMock) Judge(
	_ context.Context, opts JudgeOpts,
) (*JudgeResult, error) {
	m.opts = opts
	return m.result, m.err
}

func TestLLMJudgeHandler_Type(t *testing.T) {
	t.Parallel()
	h := &LLMJudgeHandler{}
	if h.Type() != "llm_judge" {
		t.Errorf("got %q, want %q", h.Type(), "llm_judge")
	}
}

func TestLLMJudgeHandler_PassingEval(t *testing.T) {
	t.Parallel()
	mock := &llmJudgeMock{
		result: &JudgeResult{
			Passed:    true,
			Score:     0.9,
			Reasoning: "good response",
		},
	}

	h := &LLMJudgeHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "test output",
		Metadata: map[string]any{
			"judge_provider": mock,
		},
	}
	params := map[string]any{
		"criteria": "Is it helpful?",
		"rubric":   "Score 1.0 for helpful",
		"model":    "gpt-4",
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected passed=true")
	}
	if result.Score == nil || *result.Score != 0.9 {
		t.Errorf("expected score 0.9, got %v", result.Score)
	}
	if result.Explanation != "good response" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}

	// Verify opts were forwarded
	if mock.opts.Content != "test output" {
		t.Errorf("content not forwarded: %s", mock.opts.Content)
	}
	if mock.opts.Criteria != "Is it helpful?" {
		t.Errorf("criteria not forwarded: %s", mock.opts.Criteria)
	}
	if mock.opts.Rubric != "Score 1.0 for helpful" {
		t.Errorf("rubric not forwarded: %s", mock.opts.Rubric)
	}
	if mock.opts.Model != "gpt-4" {
		t.Errorf("model not forwarded: %s", mock.opts.Model)
	}
}

func TestLLMJudgeHandler_MissingProvider(t *testing.T) {
	t.Parallel()
	h := &LLMJudgeHandler{}

	// nil metadata
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	result, err := h.Eval(
		context.Background(), evalCtx, map[string]any{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected passed=false for missing provider")
	}
	if result.Explanation != "judge_provider not found in metadata" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}

	// metadata without judge_provider
	evalCtx.Metadata = map[string]any{"other": "value"}
	result, err = h.Eval(
		context.Background(), evalCtx, map[string]any{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected passed=false for missing key")
	}
}

func TestLLMJudgeHandler_WrongProviderType(t *testing.T) {
	t.Parallel()
	h := &LLMJudgeHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "test",
		Metadata: map[string]any{
			"judge_provider": "not-a-provider",
		},
	}

	result, err := h.Eval(
		context.Background(), evalCtx, map[string]any{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected passed=false for wrong type")
	}
	if result.Explanation != "judge_provider has wrong type: string" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestLLMJudgeHandler_MinScoreThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		score    float64
		minScore float64
		wantPass bool
	}{
		{"above threshold", 0.8, 0.7, true},
		{"equal threshold", 0.7, 0.7, true},
		{"below threshold", 0.6, 0.7, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := &llmJudgeMock{
				result: &JudgeResult{
					Passed:    true,
					Score:     tt.score,
					Reasoning: "test",
				},
			}
			h := &LLMJudgeHandler{}
			evalCtx := &evals.EvalContext{
				CurrentOutput: "output",
				Metadata: map[string]any{
					"judge_provider": mock,
				},
			}
			params := map[string]any{
				"criteria":  "test",
				"min_score": tt.minScore,
			}

			result, err := h.Eval(
				context.Background(), evalCtx, params,
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Passed != tt.wantPass {
				t.Errorf(
					"passed=%v, want %v",
					result.Passed, tt.wantPass,
				)
			}
		})
	}
}

func TestLLMJudgeHandler_JudgeError(t *testing.T) {
	t.Parallel()
	mock := &llmJudgeMock{
		err: errors.New("LLM unavailable"),
	}
	h := &LLMJudgeHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "test",
		Metadata: map[string]any{
			"judge_provider": mock,
		},
	}

	result, err := h.Eval(
		context.Background(), evalCtx, map[string]any{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected passed=false on judge error")
	}
	if result.Explanation != "judge error: LLM unavailable" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestLLMJudgeHandler_SystemPromptForwarded(t *testing.T) {
	t.Parallel()
	mock := &llmJudgeMock{
		result: &JudgeResult{Passed: true, Score: 1.0},
	}
	h := &LLMJudgeHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "test",
		Metadata: map[string]any{
			"judge_provider": mock,
		},
	}
	params := map[string]any{
		"criteria":      "test",
		"system_prompt": "You are a strict judge.",
	}

	_, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.opts.SystemPrompt != "You are a strict judge." {
		t.Errorf(
			"system_prompt not forwarded: %s",
			mock.opts.SystemPrompt,
		)
	}
}

// --- Session handler tests ---

func TestLLMJudgeSessionHandler_Type(t *testing.T) {
	t.Parallel()
	h := &LLMJudgeSessionHandler{}
	if h.Type() != "llm_judge_session" {
		t.Errorf(
			"got %q, want %q", h.Type(), "llm_judge_session",
		)
	}
}

func TestLLMJudgeSessionHandler_ConcatenatesAssistant(t *testing.T) {
	t.Parallel()
	mock := &llmJudgeMock{
		result: &JudgeResult{
			Passed:    true,
			Score:     0.85,
			Reasoning: "coherent conversation",
		},
	}

	h := &LLMJudgeSessionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "How are you?"},
			{Role: "assistant", Content: "I'm doing great."},
			{Role: "user", Content: "Bye"},
			{Role: "assistant", Content: "Goodbye!"},
		},
		Metadata: map[string]any{
			"judge_provider": mock,
		},
	}
	params := map[string]any{
		"criteria": "Is the conversation coherent?",
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected passed=true")
	}
	if result.Type != "llm_judge_session" {
		t.Errorf("unexpected type: %s", result.Type)
	}

	// Verify content is concatenated assistant messages
	want := "Hi there!\nI'm doing great.\nGoodbye!"
	if mock.opts.Content != want {
		t.Errorf(
			"content mismatch:\ngot:  %q\nwant: %q",
			mock.opts.Content, want,
		)
	}
}

func TestLLMJudgeSessionHandler_MissingProvider(t *testing.T) {
	t.Parallel()
	h := &LLMJudgeSessionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "test"},
		},
	}

	result, err := h.Eval(
		context.Background(), evalCtx, map[string]any{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected passed=false")
	}
}

func TestLLMJudgeSessionHandler_JudgeError(t *testing.T) {
	t.Parallel()
	mock := &llmJudgeMock{
		err: errors.New("timeout"),
	}
	h := &LLMJudgeSessionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "test"},
		},
		Metadata: map[string]any{
			"judge_provider": mock,
		},
	}

	result, err := h.Eval(
		context.Background(), evalCtx, map[string]any{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected passed=false on judge error")
	}
	if result.Explanation != "judge error: timeout" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestLLMJudgeSessionHandler_MinScore(t *testing.T) {
	t.Parallel()
	mock := &llmJudgeMock{
		result: &JudgeResult{
			Passed: true,
			Score:  0.5,
		},
	}
	h := &LLMJudgeSessionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "short answer"},
		},
		Metadata: map[string]any{
			"judge_provider": mock,
		},
	}
	params := map[string]any{
		"criteria":  "quality",
		"min_score": 0.7,
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected passed=false (0.5 < 0.7)")
	}
}

func TestLLMJudgeSessionHandler_NoAssistantMessages(t *testing.T) {
	t.Parallel()
	mock := &llmJudgeMock{
		result: &JudgeResult{
			Passed:    false,
			Score:     0.0,
			Reasoning: "no content to evaluate",
		},
	}
	h := &LLMJudgeSessionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Metadata: map[string]any{
			"judge_provider": mock,
		},
	}

	result, err := h.Eval(
		context.Background(), evalCtx, map[string]any{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty content is sent to the judge
	if mock.opts.Content != "" {
		t.Errorf(
			"expected empty content, got: %q",
			mock.opts.Content,
		)
	}
	if result.Passed {
		t.Error("expected passed=false")
	}
}

func TestLLMJudgeSessionHandler_WrongProviderType(t *testing.T) {
	t.Parallel()
	h := &LLMJudgeSessionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "test"},
		},
		Metadata: map[string]any{
			"judge_provider": 42,
		},
	}

	result, err := h.Eval(
		context.Background(), evalCtx, map[string]any{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected passed=false for wrong type")
	}
	if result.Explanation != "judge_provider has wrong type: int" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}
