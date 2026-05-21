package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// --- bias ---

func TestBiasHandler_Type(t *testing.T) {
	t.Parallel()
	h := &BiasHandler{}
	if h.Type() != "bias" {
		t.Errorf("got %q, want %q", h.Type(), "bias")
	}
}

// TestSafetyHandlers_RejectThresholdParams pins the rejection on the
// shared evalSafetyOutput path that every safety handler goes through.
// Bias is the representative — toxicity/pii_leakage/role_violation all
// funnel through the same helper, so they all inherit the rejection.
func TestSafetyHandlers_RejectThresholdParams(t *testing.T) {
	t.Parallel()
	h := &BiasHandler{}
	evalCtx := newRAGEvalCtx(passMock(1.0, ""), "any")
	for _, banned := range []string{"min_score", "max_score"} {
		res, _ := h.Eval(context.Background(), evalCtx, map[string]any{banned: 0.5})
		if res.Error == "" || !strings.Contains(res.Error, banned+" is not a valid param") {
			t.Errorf("%s should be rejected at the safety helper layer; got Error=%q",
				banned, res.Error)
		}
	}
}

func TestBiasHandler_Pass(t *testing.T) {
	t.Parallel()
	mock := passMock(0.95, "no bias detected")
	h := &BiasHandler{}
	evalCtx := newRAGEvalCtx(mock, "All applicants are evaluated on merit.")
	params := map[string]any{}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.95 {
		t.Errorf("score=%v, want 0.95", result.Score)
	}
	if !strings.Contains(mock.opts.SystemPrompt, "bias detector") {
		t.Errorf("default bias prompt missing: %q", mock.opts.SystemPrompt)
	}
}

func TestBiasHandler_Fail(t *testing.T) {
	t.Parallel()
	mock := passMock(0.1, "stereotype detected")
	h := &BiasHandler{}
	evalCtx := newRAGEvalCtx(mock, "stereotyped response")

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.1 {
		t.Errorf("score=%v, want 0.1", result.Score)
	}
}

// --- toxicity ---

func TestToxicityHandler_Type(t *testing.T) {
	t.Parallel()
	h := &ToxicityHandler{}
	if h.Type() != "toxicity" {
		t.Errorf("got %q, want %q", h.Type(), "toxicity")
	}
}

func TestToxicityHandler_Pass(t *testing.T) {
	t.Parallel()
	mock := passMock(1.0, "non-toxic")
	h := &ToxicityHandler{}
	evalCtx := newRAGEvalCtx(mock, "Thank you for your patience.")

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Errorf("score=%v, want 1.0", result.Score)
	}
	if !strings.Contains(mock.opts.SystemPrompt, "toxicity detector") {
		t.Errorf("default toxicity prompt missing")
	}
}

func TestToxicityHandler_UserOverridesSystemPrompt(t *testing.T) {
	t.Parallel()
	mock := passMock(1.0, "")
	h := &ToxicityHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	params := map[string]any{"system_prompt": "custom"}

	_, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.opts.SystemPrompt != "custom" {
		t.Errorf("user system_prompt not respected: %q", mock.opts.SystemPrompt)
	}
}

// --- pii_leakage ---

func TestPIILeakageHandler_Type(t *testing.T) {
	t.Parallel()
	h := &PIILeakageHandler{}
	if h.Type() != "pii_leakage" {
		t.Errorf("got %q, want %q", h.Type(), "pii_leakage")
	}
}

func TestPIILeakageHandler_RegexPrePass_Email(t *testing.T) {
	t.Parallel()
	mock := passMock(1.0, "should not be called")
	h := &PIILeakageHandler{}
	evalCtx := newRAGEvalCtx(mock, "Contact me at john.doe@example.com for details.")

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Errorf("score=%v, want 0.0", result.Score)
	}
	if !strings.Contains(result.Explanation, "regex pre-pass detected email") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
	// LLM judge must NOT have been called.
	if mock.opts.Content != "" {
		t.Errorf("LLM judge was called on regex match; opts.Content=%q", mock.opts.Content)
	}
}

func TestPIILeakageHandler_RegexPrePass_SSN(t *testing.T) {
	t.Parallel()
	mock := passMock(1.0, "")
	h := &PIILeakageHandler{}
	evalCtx := newRAGEvalCtx(mock, "Their SSN is 123-45-6789.")

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Errorf("score=%v, want 0.0", result.Score)
	}
	if !strings.Contains(result.Explanation, "ssn") {
		t.Errorf("expected ssn in explanation: %s", result.Explanation)
	}
}

func TestPIILeakageHandler_RegexPrePass_CreditCard(t *testing.T) {
	t.Parallel()
	mock := passMock(1.0, "")
	h := &PIILeakageHandler{}
	cases := []string{
		"Card: 4111 1111 1111 1111",
		"Card: 4111-1111-1111-1111",
		"Card: 4111111111111111",
	}
	for _, c := range cases {
		c := c
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			evalCtx := newRAGEvalCtx(mock, c)
			result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Score == nil || *result.Score != 0.0 {
				t.Errorf("score=%v, want 0.0", result.Score)
			}
			if !strings.Contains(result.Explanation, "credit_card") {
				t.Errorf("expected credit_card in explanation: %s", result.Explanation)
			}
		})
	}
}

func TestPIILeakageHandler_FallsThroughToJudge(t *testing.T) {
	t.Parallel()
	mock := passMock(0.85, "ambiguous PII")
	h := &PIILeakageHandler{}
	// "Jane lives in Springfield" — no high-confidence regex hit
	evalCtx := newRAGEvalCtx(mock, "Jane lives in Springfield.")

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.85 {
		t.Errorf("score=%v, want 0.85", result.Score)
	}
	if !strings.Contains(mock.opts.SystemPrompt, "PII-leakage detector") {
		t.Errorf("LLM judge was not called: %q", mock.opts.SystemPrompt)
	}
}

func TestDetectHighConfidencePII(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text string
		want string
	}{
		{"plain text", ""},
		{"john@example.com", "email"},
		{"123-45-6789", "ssn"},
		{"4111-1111-1111-1111", "credit_card"},
		{"4111 1111 1111 1111", "credit_card"},
		{"4111111111111111", "credit_card"},
		{"Jane Doe", ""},
		{"123-45-678", ""}, // wrong digit count
	}
	for _, c := range cases {
		got := detectHighConfidencePII(c.text)
		if got != c.want {
			t.Errorf("text=%q: got %q, want %q", c.text, got, c.want)
		}
	}
}

// --- role_violation ---

func TestRoleViolationHandler_Type(t *testing.T) {
	t.Parallel()
	h := &RoleViolationHandler{}
	if h.Type() != "role_violation" {
		t.Errorf("got %q, want %q", h.Type(), "role_violation")
	}
}

func TestRoleViolationHandler_Pass_WithExplicitRole(t *testing.T) {
	t.Parallel()
	mock := passMock(0.95, "role-consistent")
	h := &RoleViolationHandler{}
	evalCtx := newRAGEvalCtx(mock, "I can help with your refund. Let me look up your order.")
	params := map[string]any{
		"agent_role": "You are a refund support agent.",
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.95 {
		t.Errorf("score=%v, want 0.95", result.Score)
	}
	if !strings.Contains(mock.opts.Content, "AGENT ROLE") {
		t.Errorf("AGENT ROLE section missing: %q", mock.opts.Content)
	}
	if !strings.Contains(mock.opts.Content, "refund support agent") {
		t.Errorf("agent role not included: %q", mock.opts.Content)
	}
}

func TestRoleViolationHandler_NoRole_FallsBackToGeneric(t *testing.T) {
	t.Parallel()
	mock := passMock(0.8, "")
	h := &RoleViolationHandler{}
	evalCtx := newRAGEvalCtx(mock, "I'll help with your question.")

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil {
		t.Fatal("expected score, got nil")
	}
	if strings.Contains(mock.opts.Content, "AGENT ROLE") {
		t.Errorf("AGENT ROLE included with no role supplied: %q", mock.opts.Content)
	}
	if !strings.Contains(mock.opts.Content, "ANSWER:") {
		t.Errorf("ANSWER section missing: %q", mock.opts.Content)
	}
}

func TestRoleViolationHandler_RoleFromMetadata(t *testing.T) {
	t.Parallel()
	mock := passMock(0.9, "")
	h := &RoleViolationHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "answer",
		Metadata: map[string]any{
			"judge_provider": mock,
			"system_prompt":  "You are a customer support agent.",
		},
	}

	_, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(mock.opts.Content, "customer support agent") {
		t.Errorf("metadata system_prompt not used: %q", mock.opts.Content)
	}
}

func TestRoleViolationHandler_ExplicitOverridesMetadata(t *testing.T) {
	t.Parallel()
	mock := passMock(0.9, "")
	h := &RoleViolationHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "answer",
		Metadata: map[string]any{
			"judge_provider": mock,
			"system_prompt":  "metadata-role",
		},
	}
	_, err := h.Eval(context.Background(), evalCtx, map[string]any{"agent_role": "param-role"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(mock.opts.Content, "param-role") {
		t.Errorf("explicit agent_role param not used: %q", mock.opts.Content)
	}
	if strings.Contains(mock.opts.Content, "metadata-role") {
		t.Errorf("metadata role leaked through: %q", mock.opts.Content)
	}
}

// --- shared: missing provider, judge error ---

func TestSafetyHandlers_MissingProvider(t *testing.T) {
	t.Parallel()
	handlers := []evals.EvalTypeHandler{
		&BiasHandler{},
		&ToxicityHandler{},
		&RoleViolationHandler{},
	}
	for _, h := range handlers {
		h := h
		t.Run(h.Type(), func(t *testing.T) {
			t.Parallel()
			evalCtx := &evals.EvalContext{CurrentOutput: "clean text"}
			result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result.Explanation, "judge_provider not found") {
				t.Errorf("unexpected explanation: %s", result.Explanation)
			}
		})
	}
}

func TestPIILeakageHandler_NoJudge_NoRegexHit_PassesCleanly(t *testing.T) {
	// pii_leakage is the special case among safety handlers: it has a
	// regex pre-pass, so when no judge is configured AND the regex
	// finds nothing, the handler degrades to "regex-only" and passes
	// (score 1.0). Wiring pii_leakage as a guardrail without an LLM
	// key MUST NOT cause every output to be blocked — the regex still
	// provides deterministic coverage, the LLM judge is optional.
	t.Parallel()
	h := &PIILeakageHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "clean text with no PII"}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Errorf("score=%v, want 1.0", result.Score)
	}
	if !strings.Contains(result.Explanation, "LLM judge not configured") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestPIILeakageHandler_NoJudge_RegexHit_StillFires(t *testing.T) {
	// Sanity: the regex layer must still fire even when no judge is
	// configured. The "degrade to pass" path only applies when the
	// regex found nothing.
	t.Parallel()
	h := &PIILeakageHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "Email: jane@example.com"}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Errorf("score=%v, want 0.0", result.Score)
	}
	if !strings.Contains(result.Explanation, "regex pre-pass detected email") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestSafetyHandlers_JudgeError(t *testing.T) {
	t.Parallel()
	mock := &llmJudgeMock{err: errors.New("LLM unavailable")}
	h := &BiasHandler{}
	evalCtx := newRAGEvalCtx(mock, "answer")
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Explanation, "judge error: LLM unavailable") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}
