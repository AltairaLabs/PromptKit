package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func resizeTestModel() *Model {
	m := NewModel("", 0)
	m.isTUIMode = true
	m.width = 100
	m.height = 30
	m.handleRunStarted(&RunStartedMsg{RunID: "r1", Scenario: "s", Provider: "p"})
	m.setFocusToRunsPane()
	m.View() // warm up lazy panel initialization so later renders are stable
	return m
}

func TestResizeKeysChangeMainLayout(t *testing.T) {
	m := resizeTestModel()
	before := m.View()

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlUp}) // grow focused (runs)
	grown := m.View()
	assert.NotEqual(t, before, grown, "ctrl+up should resize the focused pane")

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlDown}) // shrink back
	shrunk := m.View()
	assert.NotEqual(t, grown, shrunk, "ctrl+down should resize the focused pane")
}

func TestCollapseKeyTogglesPane(t *testing.T) {
	m := resizeTestModel()
	m.setFocusToLogsPane()
	before := m.View()

	m.Update(keyRune('z')) // collapse logs
	collapsed := m.View()
	assert.NotEqual(t, before, collapsed, "z should collapse the focused pane")

	m.Update(keyRune('z')) // restore logs
	restored := m.View()
	assert.Equal(t, before, restored, "z again should restore the pane")
}

// keyRune builds a rune key message for tests.
func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
