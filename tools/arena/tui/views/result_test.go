package views

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestNewResultView(t *testing.T) {
	view := NewResultView()
	assert.NotNil(t, view)
}

func TestResultView_Render_Completed(t *testing.T) {
	res := &statestore.RunResult{
		RunID:      "test-run-123",
		ScenarioID: "test-scenario",
		ProviderID: "openai",
		Region:     "us-east-1",
		Duration:   5 * time.Second,
		Cost: types.CostInfo{
			TotalCost: 0.25,
		},
		ConversationAssertions: statestore.AssertionsSummary{
			Total:  10,
			Failed: 2,
		},
	}

	view := NewResultView()
	output := view.Render(res, StatusCompleted)

	// Verify all fields are present
	assert.Contains(t, output, "Run: test-run-123")
	assert.Contains(t, output, "Scenario: test-scenario")
	assert.Contains(t, output, "Provider: openai")
	assert.Contains(t, output, "Region: us-east-1")
	assert.Contains(t, output, "Status: completed")
	assert.Contains(t, output, "Duration: 5s")
	assert.Contains(t, output, "Cost: $0.2500")
	assert.Contains(t, output, "Assertions: 10 total, 2 failed")
}

func TestResultView_Render_Running(t *testing.T) {
	res := &statestore.RunResult{
		RunID:      "test-run-456",
		ScenarioID: "running-scenario",
		ProviderID: "claude",
		Region:     "eu-west-1",
		Duration:   2 * time.Second,
		Cost: types.CostInfo{
			TotalCost: 0.10,
		},
	}

	view := NewResultView()
	output := view.Render(res, StatusRunning)

	assert.Contains(t, output, "Status: running")
	assert.Contains(t, output, "test-run-456")
}

func TestResultView_Render_Failed(t *testing.T) {
	res := &statestore.RunResult{
		RunID:      "test-run-789",
		ScenarioID: "failed-scenario",
		ProviderID: "gemini",
		Region:     "us-west-2",
		Duration:   1 * time.Second,
		Cost: types.CostInfo{
			TotalCost: 0.05,
		},
		Error: "Connection timeout",
	}

	view := NewResultView()
	output := view.Render(res, StatusFailed)

	assert.Contains(t, output, "Status: failed")
	assert.Contains(t, output, "Error: Connection timeout")
}

func TestResultView_Render_NoAssertions(t *testing.T) {
	res := &statestore.RunResult{
		RunID:      "test-run-no-assertions",
		ScenarioID: "simple-scenario",
		ProviderID: "openai",
		Region:     "us-east-1",
		Duration:   3 * time.Second,
		Cost: types.CostInfo{
			TotalCost: 0.15,
		},
		ConversationAssertions: statestore.AssertionsSummary{
			Total:  0,
			Failed: 0,
		},
	}

	view := NewResultView()
	output := view.Render(res, StatusCompleted)

	// Should not contain assertions line when Total is 0
	assert.NotContains(t, output, "Assertions:")
}

func TestResultView_Render_NoError(t *testing.T) {
	res := &statestore.RunResult{
		RunID:      "test-run-success",
		ScenarioID: "success-scenario",
		ProviderID: "claude",
		Region:     "us-east-1",
		Duration:   4 * time.Second,
		Cost: types.CostInfo{
			TotalCost: 0.20,
		},
		Error: "",
	}

	view := NewResultView()
	output := view.Render(res, StatusCompleted)

	// Should not contain error line when Error is empty
	assert.NotContains(t, output, "Error:")
}

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   RunStatus
		expected string
	}{
		{"Running", StatusRunning, "running"},
		{"Completed", StatusCompleted, "completed"},
		{"Failed", StatusFailed, "failed"},
		{"Unknown", RunStatus(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatStatus(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResultView_Render_AllAssertionsPassed(t *testing.T) {
	res := &statestore.RunResult{
		RunID:      "test-run-all-pass",
		ScenarioID: "all-pass-scenario",
		ProviderID: "openai",
		Region:     "us-east-1",
		Duration:   3 * time.Second,
		Cost: types.CostInfo{
			TotalCost: 0.12,
		},
		ConversationAssertions: statestore.AssertionsSummary{
			Total:  15,
			Failed: 0,
		},
	}

	view := NewResultView()
	output := view.Render(res, StatusCompleted)

	assert.Contains(t, output, "Assertions: 15 total, 0 failed")
}

func TestResultView_Render_HighCost(t *testing.T) {
	res := &statestore.RunResult{
		RunID:      "expensive-run",
		ScenarioID: "expensive-scenario",
		ProviderID: "openai",
		Region:     "us-east-1",
		Duration:   60 * time.Second,
		Cost: types.CostInfo{
			TotalCost: 12.5678,
		},
	}

	view := NewResultView()
	output := view.Render(res, StatusCompleted)

	// Cost should be formatted to 4 decimal places
	assert.Contains(t, output, "Cost: $12.5678")
}
