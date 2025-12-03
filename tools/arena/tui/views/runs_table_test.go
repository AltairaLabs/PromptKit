package views

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/viewmodels"
)

func TestNewRunsTableView(t *testing.T) {
	view := NewRunsTableView()

	assert.NotNil(t, view)
	assert.NotNil(t, view.tableStyle)
}

func TestSetDimensions(t *testing.T) {
	view := NewRunsTableView()

	view.SetDimensions(100, 20)

	assert.Equal(t, 100, view.width)
	assert.Equal(t, 20, view.height)
}

func TestSetFocused(t *testing.T) {
	view := NewRunsTableView()

	view.SetFocused(true)
	assert.True(t, view.focused)

	view.SetFocused(false)
	assert.False(t, view.focused)
}

func TestGetColumns(t *testing.T) {
	view := NewRunsTableView()

	columns := view.GetColumns()

	assert.Len(t, columns, 7)
	assert.Equal(t, "Status", columns[0].Title)
	assert.Equal(t, "Provider", columns[1].Title)
	assert.Equal(t, "Scenario", columns[2].Title)
	assert.Equal(t, "Region", columns[3].Title)
	assert.Equal(t, "Duration", columns[4].Title)
	assert.Equal(t, "Cost", columns[5].Title)
	assert.Equal(t, "Notes", columns[6].Title)
}

func TestGetTableStyle(t *testing.T) {
	view := NewRunsTableView()

	style := view.GetTableStyle()

	assert.NotNil(t, style)
}

func TestRender_EmptyData(t *testing.T) {
	view := NewRunsTableView()
	view.SetDimensions(120, 10)

	vm := viewmodels.NewRunsTableViewModel([]viewmodels.RunData{})

	result := view.Render(vm)

	assert.NotEmpty(t, result)
	assert.Contains(t, result, "Active Runs")
	assert.Contains(t, result, "0 concurrent workers")
}

func TestRender_WithData(t *testing.T) {
	view := NewRunsTableView()
	view.SetDimensions(120, 10)
	view.SetFocused(true)

	runs := []viewmodels.RunData{
		{
			Status:    viewmodels.StatusRunning,
			Provider:  "openai",
			Scenario:  "test-scenario",
			Region:    "us-east-1",
			StartTime: time.Now().Add(-5 * time.Second),
		},
		{
			Status:   viewmodels.StatusCompleted,
			Provider: "anthropic",
			Scenario: "completed-test",
			Region:   "eu-west-1",
			Duration: 2 * time.Second,
			Cost:     0.0123,
		},
	}
	vm := viewmodels.NewRunsTableViewModel(runs)

	result := view.Render(vm)

	assert.NotEmpty(t, result)
	assert.Contains(t, result, "Active Runs")
	assert.Contains(t, result, "2 concurrent workers")
	// The actual data is in the table which is rendered by bubbletea
}

func TestRender_FocusedVsUnfocused(t *testing.T) {
	view := NewRunsTableView()
	view.SetDimensions(120, 10)

	runs := []viewmodels.RunData{
		{
			Status:    viewmodels.StatusRunning,
			Provider:  "openai",
			Scenario:  "test",
			Region:    "us-east-1",
			StartTime: time.Now(),
		},
	}
	vm := viewmodels.NewRunsTableViewModel(runs)

	// Render unfocused
	view.SetFocused(false)
	unfocusedResult := view.Render(vm)

	// Render focused
	view.SetFocused(true)
	focusedResult := view.Render(vm)

	assert.NotEmpty(t, unfocusedResult)
	assert.NotEmpty(t, focusedResult)
	// Both should contain the same content, just different border colors
	assert.Contains(t, unfocusedResult, "Active Runs")
	assert.Contains(t, focusedResult, "Active Runs")
}

func TestRender_DifferentRunCounts(t *testing.T) {
	view := NewRunsTableView()
	view.SetDimensions(120, 10)

	tests := []struct {
		name     string
		runCount int
	}{
		{"zero runs", 0},
		{"one run", 1},
		{"multiple runs", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runs := make([]viewmodels.RunData, tt.runCount)
			for i := 0; i < tt.runCount; i++ {
				runs[i] = viewmodels.RunData{
					Status:    viewmodels.StatusRunning,
					Provider:  "openai",
					Scenario:  "test",
					Region:    "us-east-1",
					StartTime: time.Now(),
				}
			}
			vm := viewmodels.NewRunsTableViewModel(runs)

			result := view.Render(vm)

			assert.NotEmpty(t, result)
			if tt.runCount > 1 {
				assert.Contains(t, result, "concurrent workers")
			}
		})
	}
}
