package render

import (
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// TestAssertionMessageRendering tests that assertion messages are displayed in the HTML report
func TestAssertionMessageRendering(t *testing.T) {
	result := createTestResult("run1", "test-provider", "us", "scenario1", 0.01, 50, 25, false, 100*time.Millisecond)
	result.Messages = append(result.Messages,
		types.Message{
			Role:    "user",
			Content: "Test question",
		},
		types.Message{
			Role:    "assistant",
			Content: "Test response",
			Meta: map[string]interface{}{
				"assertions": map[string]interface{}{
					"test_assertion": map[string]interface{}{
						"passed":  true,
						"message": "This is a test assertion message",
						"details": map[string]interface{}{
							"some": "detail",
						},
					},
				},
			},
		},
	)

	data := prepareReportData([]engine.RunResult{result})
	html, err := generateHTML(data)
	if err != nil {
		t.Fatalf("generateHTML() error = %v", err)
	}

	// Check that the message is in the HTML
	if !strings.Contains(html, "This is a test assertion message") {
		t.Error("Expected assertion message 'This is a test assertion message' not found in HTML")
	}

	// Check that the validation-message div is present
	if !strings.Contains(html, "validation-message") {
		t.Error("Expected 'validation-message' div not found in HTML")
	}

	// Check that the info icon is present
	if !strings.Contains(html, "ℹ️") {
		t.Error("Expected info icon 'ℹ️' not found in HTML")
	}
}

// TestGetMessage tests the getMessage helper function
func TestGetMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name: "map with message",
			input: map[string]interface{}{
				"passed":  true,
				"message": "Test message",
			},
			expected: "Test message",
		},
		{
			name: "map without message",
			input: map[string]interface{}{
				"passed": true,
			},
			expected: "",
		},
		{
			name:     "nil input",
			input:    nil,
			expected: "",
		},
		{
			name:     "non-map input",
			input:    "not a map",
			expected: "",
		},
		{
			name: "AssertionResult struct",
			input: assertions.AssertionResult{
				Passed:  true,
				Message: "Test assertion message from struct",
				Details: map[string]interface{}{"key": "value"},
			},
			expected: "Test assertion message from struct",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMessage(tt.input)
			if result != tt.expected {
				t.Errorf("getMessage() = %v, want %v", result, tt.expected)
			}
		})
	}
}
