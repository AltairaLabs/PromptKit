package render

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// TestConversationAssertionsRendering exercises the helper that
// produces the full table. The template no longer invokes it for the
// per-result body — the body carries only the conversation now and the
// summary chips live in the result-card header (see
// TestConversationAssertionsSummaryRendering). The helper stays public
// for any caller that wants the detailed table view.
func TestConversationAssertionsRendering(t *testing.T) {
	tests := []struct {
		name         string
		results      []assertions.ConversationValidationResult
		expectInHTML []string
	}{
		{
			name: "all passing assertions",
			results: []assertions.ConversationValidationResult{
				{
					Passed:  true,
					Message: "Tools not called as expected",
					Details: map[string]interface{}{
						"tools_checked": []string{"forbidden_tool"},
					},
				},
				{
					Passed:  true,
					Message: "Content requirements met",
				},
			},
			expectInHTML: []string{
				"conversation-assertions-section",
				"Conversation Assertions",
				"2 passed, 0 failed",
				"passed",
				"Tools not called as expected",
				"Content requirements met",
			},
		},
		{
			name: "failing assertion with violations",
			results: []assertions.ConversationValidationResult{
				{
					Passed:  false,
					Message: "Forbidden tool was called",
					Violations: []assertions.ConversationViolation{
						{
							TurnIndex:   2,
							Description: "Called forbidden_tool with sensitive args",
							Evidence: map[string]interface{}{
								"tool":      "forbidden_tool",
								"arguments": map[string]interface{}{"secret": "leaked"},
							},
						},
					},
				},
			},
			expectInHTML: []string{
				"conversation-assertions-section",
				"0 passed, 1 failed",
				"failed",
				"Forbidden tool was called",
				"1 violation(s)",
				"Turn 3:",
				"Called forbidden_tool with sensitive args",
				"violation-evidence",
			},
		},
		{
			name: "mixed passing and failing",
			results: []assertions.ConversationValidationResult{
				{
					Passed:  true,
					Message: "Required tools called",
				},
				{
					Passed:  false,
					Message: "Content check failed",
					Violations: []assertions.ConversationViolation{
						{
							TurnIndex:   1,
							Description: "Missing required keyword",
						},
					},
				},
			},
			expectInHTML: []string{
				"1 passed, 1 failed",
				"Required tools called",
				"Content check failed",
				"Turn 2:",
				"Missing required keyword",
			},
		},
		{
			name: "pack-eval rendered as Evaluations title",
			results: []assertions.ConversationValidationResult{
				{
					Type:    "pack_eval:llm_judge",
					Passed:  true,
					Message: "Good response",
					Details: map[string]interface{}{
						"eval_id":     "eval-1",
						"duration_ms": int64(42),
					},
				},
				{
					Type:    "pack_eval:contains",
					Passed:  true,
					Message: "Contains keyword",
				},
			},
			expectInHTML: []string{
				"Evaluations",    // dynamic title for pack evals
				"Duration: 42ms", // inline duration
			},
		},
		{
			name: "skipped assertion displayed",
			results: []assertions.ConversationValidationResult{
				{
					Type:    "pack_eval:llm_judge",
					Passed:  true,
					Message: "skipped eval",
					Details: map[string]interface{}{
						"skipped":     true,
						"skip_reason": "condition not met",
					},
				},
			},
			expectInHTML: []string{
				"Skipped",
				"condition not met",
				"skipped",
				"1 skipped",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the helper directly — the template no longer invokes
			// it for per-result rendering, but the helper remains public.
			html := string(renderConversationAssertions(tt.results))
			for _, expected := range tt.expectInHTML {
				if !strings.Contains(html, expected) {
					t.Errorf("Expected HTML to contain %q, but it didn't.\nGot:\n%s", expected, html)
				}
			}
		})
	}
}

// TestConversationAssertionsSummaryRendering exercises the chip-based
// header summary that surfaces alongside the result-card pass/fail
// status. This is the rendering the template uses today.
func TestConversationAssertionsSummaryRendering(t *testing.T) {
	tests := []struct {
		name         string
		results      []assertions.ConversationValidationResult
		expectInHTML []string
	}{
		{
			name: "passing assertion produces a passed chip",
			results: []assertions.ConversationValidationResult{
				{Type: "tool_exec", Passed: true, Message: "all good"},
			},
			expectInHTML: []string{
				"summary-chip assertion-chip passed",
				"tool_exec",
				"✓",
			},
		},
		{
			name: "failing assertion produces a failed chip with the message in the title",
			results: []assertions.ConversationValidationResult{
				{Type: "content_includes", Passed: false, Message: "missing keyword"},
			},
			expectInHTML: []string{
				"summary-chip assertion-chip failed",
				"missing keyword", // surfaced via title attribute
				"✗",
			},
		},
		{
			name: "skipped assertion produces a skipped chip",
			results: []assertions.ConversationValidationResult{
				{
					Type:   "tool_exec",
					Passed: true,
					Details: map[string]interface{}{
						"skipped": true,
					},
				},
			},
			expectInHTML: []string{
				"summary-chip assertion-chip skipped",
				"⊘",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html := string(renderConversationAssertionsSummary(tt.results))
			for _, expected := range tt.expectInHTML {
				if !strings.Contains(html, expected) {
					t.Errorf("Expected summary HTML to contain %q.\nGot:\n%s", expected, html)
				}
			}
		})
	}
}

func TestConversationAssertionsHelpers(t *testing.T) {
	t.Run("hasConversationAssertions", func(t *testing.T) {
		tests := []struct {
			name     string
			result   engine.RunResult
			expected bool
		}{
			{
				name:     "no assertions",
				result:   engine.RunResult{},
				expected: false,
			},
			{
				name: "has assertions",
				result: engine.RunResult{
					ConversationAssertions: engine.AssertionsSummary{
						Failed:  0,
						Passed:  true,
						Results: []assertions.ConversationValidationResult{{Passed: true}},
						Total:   1,
					},
				},
				expected: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := hasConversationAssertions(tt.result)
				if got != tt.expected {
					t.Errorf("hasConversationAssertions() = %v, want %v", got, tt.expected)
				}
			})
		}
	})

	t.Run("conversationAssertionsPassed", func(t *testing.T) {
		tests := []struct {
			name     string
			results  []assertions.ConversationValidationResult
			expected bool
		}{
			{
				name:     "empty results",
				results:  []assertions.ConversationValidationResult{},
				expected: true,
			},
			{
				name: "all passing",
				results: []assertions.ConversationValidationResult{
					{Passed: true},
					{Passed: true},
				},
				expected: true,
			},
			{
				name: "one failing",
				results: []assertions.ConversationValidationResult{
					{Passed: true},
					{Passed: false},
				},
				expected: false,
			},
			{
				name: "all failing",
				results: []assertions.ConversationValidationResult{
					{Passed: false},
					{Passed: false},
				},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := conversationAssertionsPassed(tt.results)
				if got != tt.expected {
					t.Errorf("conversationAssertionsPassed() = %v, want %v", got, tt.expected)
				}
			})
		}
	})
}

// countFailures is a small helper for tests to compute failures.
func countFailures(results []assertions.ConversationValidationResult) int {
	c := 0
	for i := range results {
		if !results[i].Passed {
			c++
		}
	}
	return c
}
