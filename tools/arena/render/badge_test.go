package render

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// TestValidatorBadgeDisplay tests that validator badges show correct pass/fail icons
func TestValidatorBadgeDisplay(t *testing.T) {
	tests := []struct {
		name             string
		validationPassed bool
		wantCheckMark    bool
		wantCrossMark    bool
	}{
		{
			name:             "passed validator shows check mark",
			validationPassed: true,
			wantCheckMark:    true,
			wantCrossMark:    false,
		},
		{
			name:             "failed validator shows cross mark",
			validationPassed: false,
			wantCheckMark:    false,
			wantCrossMark:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a result with a message that has validations
			result := createTestResult("run1", "test-provider", "us", "scenario1", 0.01, 50, 25, false, 100*time.Millisecond)
			result.Messages = []types.Message{
				{
					Role:    "user",
					Content: "Test prompt",
				},
				{
					Role:    "assistant",
					Content: "Test response",
					Validations: []types.ValidationResult{
						{
							ValidatorType: "*validators.BannedWordsValidator",
							Passed:        tt.validationPassed,
							Details: map[string]interface{}{
								"value": []string{"test"},
							},
						},
					},
				},
			}

			data := prepareReportData([]engine.RunResult{result})
			html, err := generateHTML(data)
			if err != nil {
				t.Fatalf("generateHTML() error = %v", err)
			}

			// Debug: save HTML for inspection (first test only)
			if tt.name == "passed_validator_shows_check_mark" {
				_ = os.WriteFile("/tmp/test-validator-badge.html", []byte(html), 0644)
			}

			// Check for validation badge presence
			hasValidationBadge := strings.Contains(html, "badge-label\">V</span>")
			if !hasValidationBadge {
				t.Error("Expected validation badge to be present")
			}

			// Look for the badge icon specifically in the badge context
			// The badge should contain either ✓ or ✗ based on pass/fail
			badgeIconPattern := fmt.Sprintf("<span class=\"badge-icon\">%s</span>", func() string {
				if tt.wantCheckMark {
					return "✓"
				}
				return "✗"
			}())

			if !strings.Contains(html, badgeIconPattern) {
				t.Errorf("Expected badge icon pattern %q in HTML", badgeIconPattern)
			}

			// Check for validation-badge passed class (green) when passed
			hasPassedClass := strings.Contains(html, "validation-badge passed")
			if tt.validationPassed && !hasPassedClass {
				t.Error("Expected 'validation-badge passed' class for passed validator")
			}

			// Check for validation-badge failed class (red) when failed
			hasFailedClass := strings.Contains(html, "validation-badge failed")
			if !tt.validationPassed && !hasFailedClass {
				t.Error("Expected 'validation-badge failed' class for failed validator")
			}
		})
	}
}

// TestAssertionBadgeDisplay tests that assertion badges show correct pass/fail icons
func TestAssertionBadgeDisplay(t *testing.T) {
	tests := []struct {
		name            string
		assertionPassed bool
		wantCheckMark   bool
		wantCrossMark   bool
	}{
		{
			name:            "passed assertion shows check mark",
			assertionPassed: true,
			wantCheckMark:   true,
			wantCrossMark:   false,
		},
		{
			name:            "failed assertion shows cross mark",
			assertionPassed: false,
			wantCheckMark:   false,
			wantCrossMark:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a result with a message that has assertions
			result := createTestResult("run1", "test-provider", "us", "scenario1", 0.01, 50, 25, false, 100*time.Millisecond)
			result.Messages = []types.Message{
				{
					Role:    "user",
					Content: "Test prompt",
				},
				{
					Role:    "assistant",
					Content: "Test response",
					Meta: map[string]interface{}{
						"assertions": map[string]interface{}{
							"test_assertion": map[string]interface{}{
								"passed":  tt.assertionPassed,
								"details": "test details",
							},
						},
					},
				},
			}

			data := prepareReportData([]engine.RunResult{result})
			html, err := generateHTML(data)
			if err != nil {
				t.Fatalf("generateHTML() error = %v", err)
			}

			// Check for assertion badge presence
			hasAssertionBadge := strings.Contains(html, "badge-label\">A</span>")
			if !hasAssertionBadge {
				t.Error("Expected assertion badge to be present")
			}

			// Look for the badge icon specifically in the badge context
			badgeIconPattern := fmt.Sprintf("<span class=\"badge-icon\">%s</span>", func() string {
				if tt.wantCheckMark {
					return "✓"
				}
				return "✗"
			}())

			if !strings.Contains(html, badgeIconPattern) {
				t.Errorf("Expected badge icon pattern %q in HTML", badgeIconPattern)
			}

			// Check for validation-badge passed class (green) when passed
			hasPassedClass := strings.Contains(html, "validation-badge passed")
			if tt.assertionPassed && !hasPassedClass {
				t.Error("Expected 'validation-badge passed' class for passed assertion")
			}

			// Check for validation-badge failed class (red) when failed
			hasFailedClass := strings.Contains(html, "validation-badge failed")
			if !tt.assertionPassed && !hasFailedClass {
				t.Error("Expected 'validation-badge failed' class for failed assertion")
			}
		})
	}
}

// TestMultipleValidatorsBadgeDisplay tests badge display with multiple validators
func TestMultipleValidatorsBadgeDisplay(t *testing.T) {
	tests := []struct {
		name               string
		validator1Passed   bool
		validator2Passed   bool
		wantOverallSuccess bool
	}{
		{
			name:               "all validators pass",
			validator1Passed:   true,
			validator2Passed:   true,
			wantOverallSuccess: true,
		},
		{
			name:               "one validator fails",
			validator1Passed:   true,
			validator2Passed:   false,
			wantOverallSuccess: false,
		},
		{
			name:               "all validators fail",
			validator1Passed:   false,
			validator2Passed:   false,
			wantOverallSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createTestResult("run1", "test-provider", "us", "scenario1", 0.01, 50, 25, false, 100*time.Millisecond)
			result.Messages = []types.Message{
				{
					Role:    "user",
					Content: "Test prompt",
				},
				{
					Role:    "assistant",
					Content: "Test response",
					Validations: []types.ValidationResult{
						{
							ValidatorType: "*validators.BannedWordsValidator",
							Passed:        tt.validator1Passed,
							Details:       map[string]interface{}{"value": []string{"word1"}},
						},
						{
							ValidatorType: "*validators.LengthValidator",
							Passed:        tt.validator2Passed,
							Details:       map[string]interface{}{"length": 100},
						},
					},
				},
			}

			data := prepareReportData([]engine.RunResult{result})
			html, err := generateHTML(data)
			if err != nil {
				t.Fatalf("generateHTML() error = %v", err)
			}

			// The overall badge should reflect the worst result (any failure = failure)
			if tt.wantOverallSuccess {
				if !strings.Contains(html, "validation-badge passed") {
					t.Error("Expected 'validation-badge passed' when all validators pass")
				}
			} else {
				if !strings.Contains(html, "validation-badge failed") {
					t.Error("Expected 'validation-badge failed' when any validator fails")
				}
			}

			// Check individual validator results are shown
			checkMarkCount := strings.Count(html, "✓")
			crossMarkCount := strings.Count(html, "✗")

			expectedCheckMarks := 0
			expectedCrossMarks := 0
			if tt.validator1Passed {
				expectedCheckMarks++
			} else {
				expectedCrossMarks++
			}
			if tt.validator2Passed {
				expectedCheckMarks++
			} else {
				expectedCrossMarks++
			}

			if checkMarkCount < expectedCheckMarks {
				t.Errorf("Expected at least %d check marks, got %d", expectedCheckMarks, checkMarkCount)
			}
			if crossMarkCount < expectedCrossMarks {
				t.Errorf("Expected at least %d cross marks, got %d", expectedCrossMarks, crossMarkCount)
			}
		})
	}
}

// TestValidatorAndAssertionBadgesTogether tests both badges appearing together
func TestValidatorAndAssertionBadgesTogether(t *testing.T) {
	result := createTestResult("run1", "test-provider", "us", "scenario1", 0.01, 50, 25, false, 100*time.Millisecond)
	result.Messages = []types.Message{
		{
			Role:    "user",
			Content: "Test prompt",
		},
		{
			Role:    "assistant",
			Content: "Test response",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "*validators.BannedWordsValidator",
					Passed:        false,
					Details:       map[string]interface{}{"value": []string{"damn"}},
				},
			},
			Meta: map[string]interface{}{
				"assertions": map[string]interface{}{
					"guardrail_triggered": map[string]interface{}{
						"passed":  false,
						"details": "expected validator to fail but it passed",
					},
				},
			},
		},
	}

	data := prepareReportData([]engine.RunResult{result})
	html, err := generateHTML(data)
	if err != nil {
		t.Fatalf("generateHTML() error = %v", err)
	}

	// Should have both V and A badges
	hasValidationBadge := strings.Contains(html, "badge-label\">V</span>")
	hasAssertionBadge := strings.Contains(html, "badge-label\">A</span>")

	if !hasValidationBadge {
		t.Error("Expected validation badge to be present")
	}
	if !hasAssertionBadge {
		t.Error("Expected assertion badge to be present")
	}

	// Both should show as errors (red) since both failed
	failedBadgeCount := strings.Count(html, "validation-badge failed")
	if failedBadgeCount < 2 {
		t.Errorf("Expected at least 2 'validation-badge failed' classes, got %d", failedBadgeCount)
	}

	// Should have cross marks for failures
	hasCrossMark := strings.Contains(html, "✗")
	if !hasCrossMark {
		t.Error("Expected cross marks for failed validator and assertion")
	}
}
