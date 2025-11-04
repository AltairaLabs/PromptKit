package render

import (
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// TestBadgeSemantics_GuardrailTriggeredExpected tests the specific case:
// - Guardrail IS triggered (validator.passed = false) → should show red ✗
// - Assertion expects it (assertion.ok = true) → should show green ✓
func TestBadgeSemantics_GuardrailTriggeredExpected(t *testing.T) {
	result := createTestResult("run1", "test-provider", "us", "scenario1", 0.01, 50, 25, false, 100*time.Millisecond)
	result.Messages = []types.Message{
		{
			Role:    "user",
			Content: "Say some banned words",
		},
		{
			Role:    "assistant",
			Content: "damn and hell",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "*validators.BannedWordsValidator",
					Passed:        false, // Validator FAILED (guardrail triggered - bad outcome)
					Details: map[string]interface{}{
						"value": []string{"damn", "hell"},
					},
				},
			},
			Meta: map[string]interface{}{
				"assertions": map[string]interface{}{
					"guardrail_triggered": map[string]interface{}{
						"passed": true, // Assertion PASSED (we expected this - good outcome)
						"details": map[string]interface{}{
							"triggered": true,
							"validator": "banned_words",
						},
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

	// Validator badge should show FAILED (red ✗) because guardrail triggered
	if !strings.Contains(html, "validation-badge failed") {
		t.Error("Expected validator badge to show 'failed' when guardrail triggers")
	}

	// Should have a cross mark in validator badge
	validatorBadgeSection := extractBadgeSection(html, "V")
	if !strings.Contains(validatorBadgeSection, "✗") {
		t.Error("Expected ✗ in validator badge when guardrail triggers")
	}
	if strings.Contains(validatorBadgeSection, "✓") {
		t.Error("Did not expect ✓ in validator badge when guardrail triggers")
	}

	// Assertion badge should show PASSED (green ✓) because test expectation met
	if !strings.Contains(html, "validation-badge passed") {
		t.Error("Expected assertion badge to show 'passed' when assertion succeeds")
	}

	// Should have a check mark in assertion badge
	assertionBadgeSection := extractBadgeSection(html, "A")
	if !strings.Contains(assertionBadgeSection, "✓") {
		t.Error("Expected ✓ in assertion badge when test passes")
	}
	if strings.Contains(assertionBadgeSection, "✗") {
		t.Error("Did not expect ✗ in assertion badge when test passes")
	}
}

// TestBadgeSemantics_GuardrailNotTriggeredNotExpected tests:
// - Guardrail NOT triggered (validator.passed = true) → should show green ✓
// - No assertion checking it → no assertion badge
func TestBadgeSemantics_GuardrailNotTriggeredGood(t *testing.T) {
	result := createTestResult("run1", "test-provider", "us", "scenario1", 0.01, 50, 25, false, 100*time.Millisecond)
	result.Messages = []types.Message{
		{
			Role:    "user",
			Content: "Say something nice",
		},
		{
			Role:    "assistant",
			Content: "Hello, how can I help you today?",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "*validators.BannedWordsValidator",
					Passed:        true, // Validator PASSED (no violations - good outcome)
					Details: map[string]interface{}{
						"value": []string{},
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

	// Validator badge should show PASSED (green ✓) because no violations
	if !strings.Contains(html, "validation-badge passed") {
		t.Error("Expected validator badge to show 'passed' when no violations")
	}

	validatorBadgeSection := extractBadgeSection(html, "V")
	if !strings.Contains(validatorBadgeSection, "✓") {
		t.Error("Expected ✓ in validator badge when no violations")
	}
	if strings.Contains(validatorBadgeSection, "✗") {
		t.Error("Did not expect ✗ in validator badge when no violations")
	}

	// Should NOT have assertion badge
	if strings.Contains(html, "badge-label\">A</span>") {
		t.Error("Did not expect assertion badge when no assertions present")
	}
}

// TestBadgeSemantics_GuardrailTriggeredNotExpected tests the BAD case:
// - Guardrail IS triggered (validator.passed = false) → should show red ✗
// - Assertion expects it NOT to trigger (assertion.ok = false) → should show red ✗
func TestBadgeSemantics_GuardrailTriggeredNotExpected(t *testing.T) {
	result := createTestResult("run1", "test-provider", "us", "scenario1", 0.01, 50, 25, false, 100*time.Millisecond)
	result.Error = "validation failed" // This would normally cause an error
	result.Messages = []types.Message{
		{
			Role:    "user",
			Content: "Say something nice",
		},
		{
			Role:    "assistant",
			Content: "damn", // Accidentally said banned word
			Validations: []types.ValidationResult{
				{
					ValidatorType: "*validators.BannedWordsValidator",
					Passed:        false, // Validator FAILED (unexpected violation - bad)
					Details: map[string]interface{}{
						"value": []string{"damn"},
					},
				},
			},
			Meta: map[string]interface{}{
				"assertions": map[string]interface{}{
					"no_guardrail_triggered": map[string]interface{}{
						"passed":  false, // Assertion FAILED (we didn't expect this - bad)
						"details": "expected no violations but got violations",
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

	// Validator badge should show FAILED (red ✗)
	validatorBadgeSection := extractBadgeSection(html, "V")
	if !strings.Contains(validatorBadgeSection, "✗") {
		t.Error("Expected ✗ in validator badge when unexpected violation occurs")
	}

	// Assertion badge should ALSO show FAILED (red ✗)
	assertionBadgeSection := extractBadgeSection(html, "A")
	if !strings.Contains(assertionBadgeSection, "✗") {
		t.Error("Expected ✗ in assertion badge when test fails")
	}

	// Both should have failed class
	failedCount := strings.Count(html, "validation-badge failed")
	if failedCount < 2 {
		t.Errorf("Expected at least 2 failed badges, got %d", failedCount)
	}
}

// extractBadgeSection extracts the HTML section for a specific badge (V or A)
func extractBadgeSection(html, badgeLabel string) string {
	// Find the badge section
	badgeMarker := "badge-label\">" + badgeLabel + "</span>"
	idx := strings.Index(html, badgeMarker)
	if idx == -1 {
		return ""
	}

	// Extract surrounding context (200 chars before and after)
	start := idx - 200
	if start < 0 {
		start = 0
	}
	end := idx + 200
	if end > len(html) {
		end = len(html)
	}

	return html[start:end]
}
