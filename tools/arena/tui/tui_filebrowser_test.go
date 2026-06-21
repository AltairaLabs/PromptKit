package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
)

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestFileBrowser_OpenFromMainAndEscapeBack(t *testing.T) {
	m := NewModel("", 0)
	require.Equal(t, pageMain, m.currentPage)

	// 'f' opens the file browser.
	m.Update(keyRune('f'))
	assert.Equal(t, pageFileBrowser, m.currentPage)

	// Esc returns to the main page.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, pageMain, m.currentPage)
	assert.Equal(t, paneRuns, m.activePane)
}

func TestFileBrowser_RenderDoesNotPanic(t *testing.T) {
	m := NewModel("", 0)
	m.width = 100
	m.height = 30
	m.currentPage = pageFileBrowser
	assert.NotEmpty(t, m.renderFileBrowserPage(25))
}

func TestFileBrowser_SelectLoadsConversation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run-123.json")
	res := statestore.RunResult{
		RunID:      "run-123",
		ScenarioID: "scn",
		ProviderID: "prov",
		Messages:   []types.Message{{Role: "user", Content: "hi"}},
	}
	data, err := json.Marshal(res)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	m := NewModel("", 0)
	m.currentPage = pageFileBrowser
	m.Update(pages.FileSelectedMsg{RunID: "run-123", Path: path})

	assert.Equal(t, pageConversation, m.currentPage)
}

func TestFileBrowser_SelectInvalidFileLogsAndStays(t *testing.T) {
	m := NewModel("", 0)
	m.currentPage = pageFileBrowser
	before := len(m.logs)

	m.Update(pages.FileSelectedMsg{RunID: "x", Path: "/no/such/file.json"})

	assert.Equal(t, pageFileBrowser, m.currentPage, "should stay on browser on error")
	assert.Greater(t, len(m.logs), before, "error should be logged")
}

func TestFileBrowser_SelectMalformedJSONLogsAndStays(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	m := NewModel("", 0)
	m.currentPage = pageFileBrowser
	before := len(m.logs)

	m.Update(pages.FileSelectedMsg{RunID: "bad", Path: path})

	assert.Equal(t, pageFileBrowser, m.currentPage)
	assert.Greater(t, len(m.logs), before)
}
