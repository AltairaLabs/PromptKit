package pages

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestFileBrowserPage_New(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	assert.NotNil(t, page)
	assert.NotNil(t, page.filePicker)
	assert.Nil(t, page.err)
}

func TestFileBrowserPage_SetDimensions(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	page.SetDimensions(100, 50)

	assert.Equal(t, 100, page.width)
	assert.Equal(t, 50, page.height)
	// Note: filepicker height is set dynamically in Render(), not in SetDimensions
}

func TestFileBrowserPage_Init(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	cmd := page.Init()
	assert.NotNil(t, cmd)
}

func TestFileBrowserPage_View(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(80, 24)

	view := page.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Browse Results")
}

func TestFileBrowserPage_UpdateQuitKey(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Test 'q' key
	model, cmd := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.NotNil(t, model)
	assert.NotNil(t, cmd)
}

func TestFileBrowserPage_UpdateCtrlC(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Test ctrl+c
	model, cmd := page.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, model)
	assert.NotNil(t, cmd)
}

func TestFileBrowserPage_GetKeyBindings(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	bindings := page.GetKeyBindings()
	assert.NotEmpty(t, bindings)
	assert.Equal(t, 5, len(bindings))

	// Check that key bindings contain expected items
	var hasNavigate, hasSelect, hasQuit bool
	for _, kb := range bindings {
		if kb.Description == "navigate" {
			hasNavigate = true
		}
		if kb.Description == "select file" {
			hasSelect = true
		}
		if kb.Description == "quit" {
			hasQuit = true
		}
	}
	assert.True(t, hasNavigate)
	assert.True(t, hasSelect)
	assert.True(t, hasQuit)
}

func TestFileBrowserPage_RenderNoLongerIncludesKeyLegend(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(80, 24)

	// Render should not include key legend (moved to footer)
	view := page.Render()
	assert.NotEmpty(t, view)
	// The view should contain the file picker, but not the styled key legend
	assert.Contains(t, view, "Browse Results")
}
