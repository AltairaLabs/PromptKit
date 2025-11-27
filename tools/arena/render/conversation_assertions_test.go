package render

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a RunResult with conversation assertions
			result := engine.RunResult{
				RunID:                        "test-run-1",
				ConversationAssertionResults: tt.results,
			}

			// Prepare report data with this result
			data := HTMLReportData{
				Title: "Test Report",
				ScenarioGroups: []ScenarioGroup{
					{
						ScenarioID: "test-scenario",
						Results:    []engine.RunResult{result},
					},
				},
			}

			// Generate HTML
			html, err := generateHTML(data)
			if err != nil {
				t.Fatalf("generateHTML() error = %v", err)
			}

			// Check for expected strings
			for _, expected := range tt.expectInHTML {
				if !strings.Contains(html, expected) {
					t.Errorf("Expected HTML to contain %q, but it didn't", expected)
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
					ConversationAssertionResults: []assertions.ConversationValidationResult{
						{Passed: true},
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
