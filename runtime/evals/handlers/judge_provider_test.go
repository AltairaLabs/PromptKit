package handlers

import (
	"context"
	"testing"
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
