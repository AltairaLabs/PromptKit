package panels

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

func TestBuildAssertionRow_WithScore(t *testing.T) {
	p := NewResultPanel()

	result := statestore.ConversationValidationResult{
		Type:    "pack_eval:llm_judge",
		Passed:  true,
		Message: "Good response",
		Details: map[string]interface{}{
			"score":       0.85,
			"eval_id":     "eval-1",
			"duration_ms": int64(42),
		},
	}

	row := p.buildAssertionRow(result)
	require.Len(t, row, 4)
	assert.Equal(t, "✓", row[0])
	assert.Equal(t, "pack_eval:llm_judge", row[1])
	assert.Equal(t, "0.85", row[2])
	assert.Contains(t, row[3], "Good response")
}

func TestBuildAssertionRow_WithoutScore(t *testing.T) {
	p := NewResultPanel()

	result := statestore.ConversationValidationResult{
		Type:    "tools_called",
		Passed:  true,
		Message: "ok",
	}

	row := p.buildAssertionRow(result)
	require.Len(t, row, 4)
	assert.Equal(t, "✓", row[0])
	assert.Equal(t, "—", row[2]) // no score
}

func TestBuildAssertionRow_Skipped(t *testing.T) {
	p := NewResultPanel()

	result := statestore.ConversationValidationResult{
		Type:    "pack_eval:contains",
		Passed:  true,
		Message: "skipped",
		Details: map[string]interface{}{
			"skipped":     true,
			"skip_reason": "condition not met",
		},
	}

	row := p.buildAssertionRow(result)
	require.Len(t, row, 4)
	assert.Equal(t, "⊘", row[0]) // skipped icon
}

func TestBuildAssertionRow_Failed(t *testing.T) {
	p := NewResultPanel()

	result := statestore.ConversationValidationResult{
		Type:    "contains",
		Passed:  false,
		Message: "missing keyword",
	}

	row := p.buildAssertionRow(result)
	require.Len(t, row, 4)
	assert.Equal(t, "✗", row[0])
}

func TestBuildTableContent_WithScores(t *testing.T) {
	p := NewResultPanel()

	data := &ResultPanelData{
		Status: views.StatusCompleted,
		Result: &statestore.RunResult{
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  2,
				Failed: 0,
				Passed: true,
				Results: []assertions.ConversationValidationResult{
					{
						Type: "pack_eval:llm_judge", Passed: true, Message: "good",
						Details: map[string]interface{}{"score": 0.92},
					},
					{
						Type: "tools_called", Passed: true, Message: "ok",
					},
				},
			},
		},
	}

	rows, summary := p.buildTableContent(data)
	require.Len(t, rows, 2)
	assert.Contains(t, summary, "2/2 passed")
	// First row should have score
	assert.Equal(t, "0.92", rows[0][2])
	// Second row should have em dash
	assert.Equal(t, "—", rows[1][2])
}

func TestBuildTableContent_NilData(t *testing.T) {
	p := NewResultPanel()

	rows, summary := p.buildTableContent(nil)
	assert.Empty(t, rows)
	assert.Equal(t, "No run selected", summary)
}

func TestBuildTableContent_NoAssertions(t *testing.T) {
	p := NewResultPanel()

	data := &ResultPanelData{
		Result: &statestore.RunResult{},
	}

	rows, summary := p.buildTableContent(data)
	assert.Empty(t, rows)
	assert.Equal(t, "No assertions", summary)
}
