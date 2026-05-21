package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// --- shared helpers used by every RAG handler test ---

// TestRAGHandlers_RejectThresholdParams pins the rejection on the
// shared ragJudgeCall path that every RAG handler funnels through.
// Faithfulness is the representative — answer_relevancy / contextual_*
// / hallucination all flow through the same helper.
func TestRAGHandlers_RejectThresholdParams(t *testing.T) {
	t.Parallel()
	h := &FaithfulnessHandler{}
	evalCtx := newRAGEvalCtx(passMock(1.0, ""), "answer")
	for _, banned := range []string{"min_score", "max_score"} {
		res, _ := h.Eval(context.Background(), evalCtx, map[string]any{
			"contexts": []string{"context"},
			banned:     0.5,
		})
		if res.Error == "" || !strings.Contains(res.Error, banned+" is not a valid param") {
			t.Errorf("%s should be rejected at the rag helper layer; got Error=%q",
				banned, res.Error)
		}
	}
}

func newRAGEvalCtx(mock *llmJudgeMock, output string) *evals.EvalContext {
	return &evals.EvalContext{
		CurrentOutput: output,
		Messages: []types.Message{
			{Role: "user", Content: "What is the capital of France?"},
		},
		Metadata: map[string]any{
			"judge_provider": mock,
		},
	}
}

func passMock(score float64, reasoning string) *llmJudgeMock {
	return &llmJudgeMock{
		result: &JudgeResult{
			Passed:    true,
			Score:     score,
			Reasoning: reasoning,
		},
	}
}

func failMock(reasoning string) *llmJudgeMock {
	return &llmJudgeMock{
		result: &JudgeResult{
			Passed:    false,
			Score:     0.0,
			Reasoning: reasoning,
		},
	}
}

// --- faithfulness ---

func TestFaithfulnessHandler_Type(t *testing.T) {
	t.Parallel()
	h := &FaithfulnessHandler{}
	if h.Type() != "faithfulness" {
		t.Errorf("got %q, want %q", h.Type(), "faithfulness")
	}
}

func TestFaithfulnessHandler_Pass(t *testing.T) {
	t.Parallel()
	mock := passMock(0.95, "answer fully supported")
	h := &FaithfulnessHandler{}
	evalCtx := newRAGEvalCtx(mock, "Paris is the capital of France.")
	params := map[string]any{
		"contexts": []string{"Paris is the capital of France."},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.95 {
		t.Errorf("score=%v, want 0.95", result.Score)
	}
	if !strings.Contains(mock.opts.Content, "Paris is the capital of France.") {
		t.Errorf("context not included in judge content: %q", mock.opts.Content)
	}
	if !strings.Contains(mock.opts.SystemPrompt, "faithfulness evaluator") {
		t.Errorf("default system prompt missing: %q", mock.opts.SystemPrompt)
	}
}

func TestFaithfulnessHandler_Fail_NoContext(t *testing.T) {
	t.Parallel()
	mock := passMock(1.0, "should not be called")
	h := &FaithfulnessHandler{}
	evalCtx := newRAGEvalCtx(mock, "any answer")
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Errorf("score=%v, want 0.0", result.Score)
	}
	if !strings.Contains(result.Explanation, "no context provided") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestFaithfulnessHandler_ContextField(t *testing.T) {
	t.Parallel()
	mock := passMock(0.9, "")
	h := &FaithfulnessHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "answer",
		Metadata: map[string]any{
			"judge_provider":   mock,
			"retrieved_chunks": []string{"chunk-a", "chunk-b"},
		},
	}
	params := map[string]any{"context_field": "retrieved_chunks"}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil {
		t.Fatal("expected score, got nil")
	}
	if !strings.Contains(mock.opts.Content, "chunk-a") {
		t.Errorf("context_field chunks not included: %q", mock.opts.Content)
	}
}

func TestFaithfulnessHandler_UserOverridesSystemPrompt(t *testing.T) {
	t.Parallel()
	mock := passMock(1.0, "")
	h := &FaithfulnessHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	params := map[string]any{
		"contexts":      []string{"ctx"},
		"system_prompt": "custom",
	}

	_, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.opts.SystemPrompt != "custom" {
		t.Errorf("user system_prompt not respected: %q", mock.opts.SystemPrompt)
	}
}

// --- answer_relevancy ---

func TestAnswerRelevancyHandler_Type(t *testing.T) {
	t.Parallel()
	h := &AnswerRelevancyHandler{}
	if h.Type() != "answer_relevancy" {
		t.Errorf("got %q, want %q", h.Type(), "answer_relevancy")
	}
}

func TestAnswerRelevancyHandler_Pass(t *testing.T) {
	t.Parallel()
	mock := passMock(0.9, "directly addresses")
	h := &AnswerRelevancyHandler{}
	evalCtx := newRAGEvalCtx(mock, "Paris.")
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.9 {
		t.Errorf("score=%v, want 0.9", result.Score)
	}
	if !strings.Contains(mock.opts.Content, "QUESTION:") || !strings.Contains(mock.opts.Content, "ANSWER:") {
		t.Errorf("content missing structured sections: %q", mock.opts.Content)
	}
}

func TestAnswerRelevancyHandler_NoQuestion(t *testing.T) {
	t.Parallel()
	mock := passMock(1.0, "")
	h := &AnswerRelevancyHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "answer",
		Metadata:      map[string]any{"judge_provider": mock},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Explanation, "no question") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestAnswerRelevancyHandler_ExplicitQuestion(t *testing.T) {
	t.Parallel()
	mock := passMock(0.8, "")
	h := &AnswerRelevancyHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Paris",
		Metadata:      map[string]any{"judge_provider": mock},
	}
	params := map[string]any{"question": "Which French city?"}

	_, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(mock.opts.Content, "Which French city?") {
		t.Errorf("explicit question not used: %q", mock.opts.Content)
	}
}

// --- contextual_precision ---

func TestContextualPrecisionHandler_Type(t *testing.T) {
	t.Parallel()
	h := &ContextualPrecisionHandler{}
	if h.Type() != "contextual_precision" {
		t.Errorf("got %q, want %q", h.Type(), "contextual_precision")
	}
}

func TestContextualPrecisionHandler_Pass(t *testing.T) {
	t.Parallel()
	mock := passMock(0.75, "3 of 4 chunks relevant")
	h := &ContextualPrecisionHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	params := map[string]any{
		"contexts": []string{"relevant-a", "relevant-b", "noise", "relevant-c"},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.75 {
		t.Errorf("score=%v, want 0.75", result.Score)
	}
	for _, want := range []string{"chunk 1", "chunk 2", "chunk 3", "chunk 4"} {
		if !strings.Contains(mock.opts.Content, want) {
			t.Errorf("missing chunk label %q in content: %q", want, mock.opts.Content)
		}
	}
}

func TestContextualPrecisionHandler_NoContext(t *testing.T) {
	t.Parallel()
	mock := failMock("")
	h := &ContextualPrecisionHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Explanation, "no context provided") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

// --- contextual_recall ---

func TestContextualRecallHandler_Type(t *testing.T) {
	t.Parallel()
	h := &ContextualRecallHandler{}
	if h.Type() != "contextual_recall" {
		t.Errorf("got %q, want %q", h.Type(), "contextual_recall")
	}
}

func TestContextualRecallHandler_Pass(t *testing.T) {
	t.Parallel()
	mock := passMock(0.9, "all reference claims supported")
	h := &ContextualRecallHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	params := map[string]any{
		"contexts":  []string{"Paris is the capital of France."},
		"reference": "Paris is the capital of France.",
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.9 {
		t.Errorf("score=%v, want 0.9", result.Score)
	}
	if !strings.Contains(mock.opts.Content, "REFERENCE ANSWER") {
		t.Errorf("REFERENCE ANSWER section missing: %q", mock.opts.Content)
	}
}

func TestContextualRecallHandler_NoReference(t *testing.T) {
	t.Parallel()
	mock := failMock("")
	h := &ContextualRecallHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	params := map[string]any{"contexts": []string{"x"}}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Explanation, "ground-truth") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestContextualRecallHandler_ExpectedOutputAlias(t *testing.T) {
	t.Parallel()
	mock := passMock(0.5, "")
	h := &ContextualRecallHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	params := map[string]any{
		"contexts":        []string{"x"},
		"expected_output": "y",
	}
	_, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(mock.opts.Content, "y") {
		t.Errorf("expected_output not used as reference: %q", mock.opts.Content)
	}
}

// --- contextual_relevancy ---

func TestContextualRelevancyHandler_Type(t *testing.T) {
	t.Parallel()
	h := &ContextualRelevancyHandler{}
	if h.Type() != "contextual_relevancy" {
		t.Errorf("got %q, want %q", h.Type(), "contextual_relevancy")
	}
}

func TestContextualRelevancyHandler_Pass(t *testing.T) {
	t.Parallel()
	mock := passMock(0.65, "mean relevance 0.65")
	h := &ContextualRelevancyHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	params := map[string]any{
		"contexts": []string{"a", "b"},
	}
	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.65 {
		t.Errorf("score=%v, want 0.65", result.Score)
	}
}

func TestContextualRelevancyHandler_NoContext(t *testing.T) {
	t.Parallel()
	mock := failMock("")
	h := &ContextualRelevancyHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Explanation, "no context provided") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestContextualRelevancyHandler_NoQuestion(t *testing.T) {
	t.Parallel()
	mock := failMock("")
	h := &ContextualRelevancyHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "answer",
		Metadata:      map[string]any{"judge_provider": mock},
	}
	params := map[string]any{"contexts": []string{"a"}}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Explanation, "no question") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

// --- hallucination ---

func TestHallucinationHandler_Type(t *testing.T) {
	t.Parallel()
	h := &HallucinationHandler{}
	if h.Type() != "hallucination" {
		t.Errorf("got %q, want %q", h.Type(), "hallucination")
	}
}

func TestHallucinationHandler_Pass_NoHallucination(t *testing.T) {
	t.Parallel()
	mock := passMock(1.0, "no hallucination detected")
	h := &HallucinationHandler{}
	evalCtx := newRAGEvalCtx(mock, "Paris is the capital of France.")
	params := map[string]any{
		"contexts": []string{"Paris is the capital of France."},
	}
	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Errorf("score=%v, want 1.0", result.Score)
	}
}

func TestHallucinationHandler_Fail(t *testing.T) {
	t.Parallel()
	mock := passMock(0.2, "two unsupported claims")
	h := &HallucinationHandler{}
	evalCtx := newRAGEvalCtx(mock, "Paris is the capital of France and has 100M residents.")
	params := map[string]any{
		"contexts": []string{"Paris is the capital of France."},
	}
	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.2 {
		t.Errorf("score=%v, want 0.2", result.Score)
	}
}

// --- shared handling: missing provider, judge error ---

func TestRAGHandlers_MissingProvider(t *testing.T) {
	t.Parallel()
	handlers := []evals.EvalTypeHandler{
		&FaithfulnessHandler{},
		&AnswerRelevancyHandler{},
		&ContextualPrecisionHandler{},
		&ContextualRecallHandler{},
		&ContextualRelevancyHandler{},
		&HallucinationHandler{},
	}
	for _, h := range handlers {
		t.Run(h.Type(), func(t *testing.T) {
			t.Parallel()
			evalCtx := &evals.EvalContext{CurrentOutput: "x"}
			result, err := h.Eval(context.Background(), evalCtx, map[string]any{
				"contexts":  []string{"ctx"},
				"reference": "ref",
				"question":  "q",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result.Explanation, "judge_provider not found") {
				t.Errorf("unexpected explanation: %s", result.Explanation)
			}
		})
	}
}

func TestRAGHandlers_JudgeError(t *testing.T) {
	t.Parallel()
	mock := &llmJudgeMock{err: errors.New("LLM unavailable")}
	h := &FaithfulnessHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	params := map[string]any{"contexts": []string{"ctx"}}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Explanation, "judge error: LLM unavailable") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

// --- rag_helpers tests ---

func TestExtractRAGContexts_PreferenceOrder(t *testing.T) {
	t.Parallel()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{"chunks": []string{"meta-a"}},
	}
	// contexts takes precedence over context, which takes precedence over context_field
	got := extractRAGContexts(evalCtx, map[string]any{
		"contexts":      []string{"params-a", "params-b"},
		"context":       "ignored",
		"context_field": "chunks",
	})
	if len(got) != 2 || got[0] != "params-a" {
		t.Errorf("contexts param ignored: %v", got)
	}
	// context (singular) when contexts absent
	got = extractRAGContexts(evalCtx, map[string]any{
		"context":       "single",
		"context_field": "chunks",
	})
	if len(got) != 1 || got[0] != "single" {
		t.Errorf("context param ignored: %v", got)
	}
	// context_field fallback
	got = extractRAGContexts(evalCtx, map[string]any{
		"context_field": "chunks",
	})
	if len(got) != 1 || got[0] != "meta-a" {
		t.Errorf("context_field not consulted: %v", got)
	}
	// nothing
	got = extractRAGContexts(evalCtx, map[string]any{})
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestCoerceContextSlice(t *testing.T) {
	t.Parallel()
	if got := coerceContextSlice([]string{"a", "b"}); len(got) != 2 {
		t.Errorf("[]string: %v", got)
	}
	if got := coerceContextSlice([]any{"a", 42, "b"}); len(got) != 2 {
		t.Errorf("[]any with non-strings: %v", got)
	}
	if got := coerceContextSlice("single"); len(got) != 1 || got[0] != "single" {
		t.Errorf("string: %v", got)
	}
	if got := coerceContextSlice(""); got != nil {
		t.Errorf("empty string: %v", got)
	}
	if got := coerceContextSlice(123); got != nil {
		t.Errorf("int: %v", got)
	}
}

func TestExtractRAGQuestion_FromMessages(t *testing.T) {
	t.Parallel()
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "reply"},
			{Role: "user", Content: "second"},
		},
	}
	got := extractRAGQuestion(evalCtx, map[string]any{})
	if got != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}
}

func TestExtractRAGQuestion_ExplicitOverride(t *testing.T) {
	t.Parallel()
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{{Role: "user", Content: "msg"}},
	}
	got := extractRAGQuestion(evalCtx, map[string]any{"question": "explicit"})
	if got != "explicit" {
		t.Errorf("got %q, want %q", got, "explicit")
	}
}

func TestExtractRAGReference(t *testing.T) {
	t.Parallel()
	got := extractRAGReference(map[string]any{"reference": "a"})
	if got != "a" {
		t.Errorf("got %q, want %q", got, "a")
	}
	got = extractRAGReference(map[string]any{"expected_output": "b"})
	if got != "b" {
		t.Errorf("got %q, want %q", got, "b")
	}
	got = extractRAGReference(map[string]any{})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestFormatContexts(t *testing.T) {
	t.Parallel()
	got := formatContexts([]string{"a", "b"})
	if !strings.Contains(got, "[chunk 1] a") || !strings.Contains(got, "[chunk 2] b") {
		t.Errorf("unexpected format: %q", got)
	}
	if got := formatContexts(nil); got != "(none)" {
		t.Errorf("nil case: %q", got)
	}
}
