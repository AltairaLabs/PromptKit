package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// mockJudgeProvider implements JudgeProvider for testing.
type mockJudgeProvider struct {
	result *JudgeResult
	err    error
}

func (m *mockJudgeProvider) Judge(
	_ context.Context, _ JudgeOpts,
) (*JudgeResult, error) {
	return m.result, m.err
}

func TestJudgeProviderInterface(t *testing.T) {
	// Verify the interface can be implemented and used
	var provider JudgeProvider = &mockJudgeProvider{
		result: &JudgeResult{
			Passed:    true,
			Score:     0.95,
			Reasoning: "Content is helpful and accurate",
			Raw:       `{"passed": true, "score": 0.95}`,
		},
	}

	result, err := provider.Judge(context.Background(), JudgeOpts{
		Content:  "Hello, how can I help you?",
		Criteria: "Is the response helpful?",
		Model:    "claude-sonnet-4-5-20250929",
	})

	if err != nil {
		t.Fatalf("Judge returned error: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true")
	}
	if result.Score != 0.95 {
		t.Errorf("got score %f, want 0.95", result.Score)
	}
	if result.Reasoning == "" {
		t.Error("expected non-empty reasoning")
	}
}

func TestJudgeOptsFields(t *testing.T) {
	minScore := 0.8
	opts := JudgeOpts{
		Content:      "test content",
		Criteria:     "be helpful",
		Rubric:       "detailed rubric",
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are a judge",
		MinScore:     &minScore,
		Extra:        map[string]any{"temperature": 0.0},
	}

	if opts.Content != "test content" {
		t.Error("Content not set correctly")
	}
	if opts.MinScore == nil || *opts.MinScore != 0.8 {
		t.Error("MinScore not set correctly")
	}
	if opts.Extra["temperature"] != 0.0 {
		t.Error("Extra not set correctly")
	}
}

func TestJudgeProviderError(t *testing.T) {
	provider := &mockJudgeProvider{
		err: context.DeadlineExceeded,
	}

	_, err := provider.Judge(context.Background(), JudgeOpts{
		Content:  "test",
		Criteria: "test",
	})

	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestParseJudgeResponse_ValidJSON(t *testing.T) {
	t.Parallel()
	raw := `{"passed": true, "score": 0.92, "reasoning": "Good quality"}`
	result, err := parseJudgeResponse(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true")
	}
	if result.Score != 0.92 {
		t.Errorf("got score %f, want 0.92", result.Score)
	}
	if result.Reasoning != "Good quality" {
		t.Errorf("got reasoning %q", result.Reasoning)
	}
}

func TestParseJudgeResponse_WrappedInMarkdown(t *testing.T) {
	t.Parallel()
	raw := "```json\n{\"passed\": false, \"score\": 0.3, \"reasoning\": \"Poor\"}\n```"
	result, err := parseJudgeResponse(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false")
	}
	if result.Score != 0.3 {
		t.Errorf("got score %f, want 0.3", result.Score)
	}
}

func TestParseJudgeResponse_NoPassed_UsesMinScore(t *testing.T) {
	t.Parallel()
	raw := `{"score": 0.6, "reasoning": "OK"}`
	minScore := 0.7
	result, err := parseJudgeResponse(raw, &minScore)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false (0.6 < 0.7)")
	}
}

func TestParseJudgeResponse_NoPassed_UsesDefaultThreshold(t *testing.T) {
	t.Parallel()
	raw := `{"score": 0.6, "reasoning": "OK"}`
	result, err := parseJudgeResponse(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true (0.6 >= 0.5 default)")
	}
}

func TestParseJudgeResponse_InvalidJSON(t *testing.T) {
	t.Parallel()
	raw := "This is not JSON at all"
	result, err := parseJudgeResponse(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Fallback: treat as passed
	if !result.Passed {
		t.Error("expected Passed=true on parse failure fallback")
	}
	if result.Reasoning != "Could not parse judge response" {
		t.Errorf("unexpected reasoning: %s", result.Reasoning)
	}
}

func TestSpecJudgeProvider_Implements(t *testing.T) {
	t.Parallel()
	var _ JudgeProvider = (*SpecJudgeProvider)(nil)
}

func TestCoerceJudgeTargets_TypedMap(t *testing.T) {
	t.Parallel()
	targets := map[string]providers.ProviderSpec{
		"default": {Type: "mock", Model: "test"},
	}
	result := coerceJudgeTargets(targets)
	if len(result) != 1 {
		t.Fatalf("expected 1 target, got %d", len(result))
	}
	if result["default"].Type != "mock" {
		t.Errorf("expected type mock, got %s", result["default"].Type)
	}
}

func TestCoerceJudgeTargets_InterfaceMap(t *testing.T) {
	t.Parallel()
	targets := map[string]any{
		"judge1": providers.ProviderSpec{Type: "openai", Model: "gpt-4"},
	}
	result := coerceJudgeTargets(targets)
	if len(result) != 1 {
		t.Fatalf("expected 1 target, got %d", len(result))
	}
	if result["judge1"].Model != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", result["judge1"].Model)
	}
}

func TestCoerceJudgeTargets_WrongType(t *testing.T) {
	t.Parallel()
	result := coerceJudgeTargets("not a map")
	if result != nil {
		t.Error("expected nil for wrong type")
	}
}

func TestExtractJudgeProvider_FallbackToTargets(t *testing.T) {
	t.Parallel()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"judge_targets": map[string]providers.ProviderSpec{
				"default": {Type: "mock", Model: "test-model"},
			},
		},
	}
	provider, err := extractJudgeProvider(evalCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	// Verify it's a SpecJudgeProvider
	if _, ok := provider.(*SpecJudgeProvider); !ok {
		t.Errorf("expected *SpecJudgeProvider, got %T", provider)
	}
}

func TestExtractJudgeProvider_PrefersDirectProvider(t *testing.T) {
	t.Parallel()
	mock := &mockJudgeProvider{
		result: &JudgeResult{Passed: true, Score: 1.0},
	}
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"judge_provider": mock,
			"judge_targets": map[string]providers.ProviderSpec{
				"default": {Type: "mock"},
			},
		},
	}
	provider, err := extractJudgeProvider(evalCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should prefer direct provider over targets
	if _, ok := provider.(*mockJudgeProvider); !ok {
		t.Errorf("expected *mockJudgeProvider, got %T", provider)
	}
}

func TestExtractJudgeProvider_EmptyTargets(t *testing.T) {
	t.Parallel()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"judge_targets": map[string]providers.ProviderSpec{},
		},
	}
	_, err := extractJudgeProvider(evalCtx)
	if err == nil {
		t.Error("expected error for empty targets")
	}
}

func TestExtractJudgeProvider_NilMetadata(t *testing.T) {
	t.Parallel()
	evalCtx := &evals.EvalContext{}
	_, err := extractJudgeProvider(evalCtx)
	if err == nil {
		t.Error("expected error for nil metadata")
	}
}

func TestExtractJudgeProvider_NoProviderOrTargets(t *testing.T) {
	t.Parallel()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{"other": "value"},
	}
	_, err := extractJudgeProvider(evalCtx)
	if err == nil {
		t.Error("expected error when neither provider nor targets present")
	}
}
