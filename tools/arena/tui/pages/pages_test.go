package pages

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
)

func TestMainPage_Basic(t *testing.T) {
	page := NewMainPage()

	runs := []panels.RunInfo{
		{RunID: "run-1", Scenario: "s1", Provider: "p1", Status: panels.StatusRunning},
	}

	logs := []panels.LogEntry{
		{Level: "INFO", Message: "test"},
	}

	// Basic smoke test
	page.SetDimensions(100, 30)
	page.SetData(runs, logs, "runs", nil)
	view := page.Render()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Active Runs")
}

func TestMainPage_RenderFocusAndSizes(t *testing.T) {
	runs := []panels.RunInfo{
		{RunID: "run-1", Scenario: "s1", Provider: "p1", Status: panels.StatusRunning},
	}
	logs := []panels.LogEntry{{Level: "INFO", Message: "hello"}}
	result := &panels.ResultPanelData{}

	for _, focus := range []string{"runs", "logs", "result"} {
		for _, sz := range [][2]int{{100, 30}, {60, 20}, {200, 60}} {
			page := NewMainPage()
			page.SetDimensions(sz[0], sz[1])
			page.SetData(runs, logs, focus, result)
			page.SetFocusedPanel(focus)
			view := page.Render()
			assert.NotEmpty(t, view, "focus=%s size=%v", focus, sz)
			assert.Contains(t, view, "Active Runs", "focus=%s size=%v", focus, sz)
		}
	}

	// Direct-access getters used by key handling.
	page := NewMainPage()
	assert.NotNil(t, page.RunsPanel())
	assert.NotNil(t, page.LogsPanel())
	assert.NotNil(t, page.ResultPanel())
}

func mainPageWithData(focus string) *MainPage {
	page := NewMainPage()
	page.SetDimensions(100, 30)
	page.SetData(
		[]panels.RunInfo{{RunID: "r", Scenario: "s", Provider: "p", Status: panels.StatusRunning}},
		[]panels.LogEntry{{Level: "INFO", Message: "x"}},
		focus, nil)
	page.Render() // warm up lazy panel initialization so later renders are stable
	return page
}

func TestMainPage_GrowFocusedChangesLayout(t *testing.T) {
	page := mainPageWithData(paneIDRuns)
	before := page.Render()
	page.GrowFocused(5)
	after := page.Render()
	assert.NotEqual(t, before, after, "growing the runs pane should change the layout")
}

func TestMainPage_CollapseRoundTrip(t *testing.T) {
	page := mainPageWithData(paneIDLogs)
	original := page.Render()

	page.ToggleCollapseFocused()
	collapsed := page.Render()
	assert.NotEqual(t, original, collapsed, "collapsing logs should change the layout")

	page.ToggleCollapseFocused()
	restored := page.Render()
	assert.Equal(t, original, restored, "restoring should return to the original layout")
}

func TestMainPage_GrowWithoutFocusIsNoop(t *testing.T) {
	page := mainPageWithData("")
	before := page.Render()
	page.GrowFocused(5)
	page.ToggleCollapseFocused()
	after := page.Render()
	assert.Equal(t, before, after, "with no focused pane, resize/collapse should do nothing")
}

func TestConversationPage_Basic(t *testing.T) {
	page := NewConversationPage()
	page.SetDimensions(100, 30)

	res := &statestore.RunResult{
		RunID: "run-1",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	}

	page.SetData("run-1", "scn", "prov", res)
	view := page.Render()
	assert.Contains(t, view, "Conversation")
}
