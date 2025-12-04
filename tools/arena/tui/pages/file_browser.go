// Package pages provides top-level page components for the TUI.
package pages

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

const (
	fileBrowserChrome       = 5
	defaultViewportWidth    = 80
	defaultViewportHeight   = 20
	minFileBrowserHeight    = 5
	minFileBrowserWidth     = 20
	fileBrowserWidthChrome  = 6 // border (2) + padding (4)
	fileBrowserPaddingHoriz = 2
)

// FileBrowserPage renders the file browser view for viewing past results
type FileBrowserPage struct {
	width      int
	height     int
	filePicker filepicker.Model
	viewport   viewport.Model
	err        error
}

// NewFileBrowserPage creates a new file browser page
func NewFileBrowserPage(resultsDir string) *FileBrowserPage {
	fp := filepicker.New()
	fp.CurrentDirectory = resultsDir
	fp.AllowedTypes = []string{".json"}
	fp.ShowHidden = false

	// Custom key bindings
	fp.KeyMap = filepicker.KeyMap{
		GoToTop:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "first")),
		GoToLast: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "last")),
		Down:     key.NewBinding(key.WithKeys("j", "down", "ctrl+n"), key.WithHelp("↓/j", "down")),
		Up:       key.NewBinding(key.WithKeys("k", "up", "ctrl+p"), key.WithHelp("↑/k", "up")),
		PageDown: key.NewBinding(key.WithKeys("J", "pgdown"), key.WithHelp("pgdn", "page down")),
		PageUp:   key.NewBinding(key.WithKeys("K", "pgup"), key.WithHelp("pgup", "page up")),
		Back:     key.NewBinding(key.WithKeys("h", "backspace", "left", "esc"), key.WithHelp("←/h", "back")),
		Open:     key.NewBinding(key.WithKeys("l", "right", "enter"), key.WithHelp("→/l/enter", "open")),
		Select:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	}

	vp := viewport.New(defaultViewportWidth, defaultViewportHeight)

	return &FileBrowserPage{
		filePicker: fp,
		viewport:   vp,
	}
}

// Init initializes the file browser page (tea.Model interface)
func (p *FileBrowserPage) Init() tea.Cmd {
	return p.filePicker.Init()
}

// Reset clears the selection state so the same file can be selected again
func (p *FileBrowserPage) Reset() {
	p.filePicker.Path = ""
}

// View renders the file browser page (tea.Model interface)
func (p *FileBrowserPage) View() string {
	return p.Render()
}

// fileBrowserErrorMsg is sent when an error occurs
type fileBrowserErrorMsg struct {
	err error
}

// FileSelectedMsg is sent to parent TUI when user selects a file
type FileSelectedMsg struct {
	RunID string
	Path  string
}

// SetDimensions updates the page dimensions
func (p *FileBrowserPage) SetDimensions(width, height int) {
	p.width = width
	p.height = height
}

// Update handles input for the file browser (tea.Model interface)
func (p *FileBrowserPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fileBrowserErrorMsg:
		p.err = msg.err
		return p, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return p, tea.Quit
		case "enter":
			// Handle Enter key: check if it's a file or directory
			cmd := p.handleEnterKey(msg)
			return p, cmd
		}
	}

	var cmd tea.Cmd
	p.filePicker, cmd = p.filePicker.Update(msg)
	return p, cmd
}

// handleEnterKey handles the Enter key press: opens files or navigates into directories
func (p *FileBrowserPage) handleEnterKey(msg tea.Msg) tea.Cmd {
	// Let filepicker process the Enter key first to set Path
	oldPath := p.filePicker.Path
	var cmd tea.Cmd
	p.filePicker, cmd = p.filePicker.Update(msg)

	// If Path didn't change, nothing was selected (might be empty directory)
	if p.filePicker.Path == oldPath || p.filePicker.Path == "" {
		return cmd
	}

	// Path changed - check if it's a file or directory
	info, err := os.Stat(p.filePicker.Path)
	if err != nil {
		return func() tea.Msg {
			return fileBrowserErrorMsg{err: fmt.Errorf("failed to access %s: %w", p.filePicker.Path, err)}
		}
	}

	if info.IsDir() {
		// It's a directory - filepicker already handled navigation, just return its command
		return cmd
	}

	// It's a file - emit selection message for parent TUI to handle
	return func() tea.Msg {
		// Extract runID from filename
		filename := filepath.Base(p.filePicker.Path)
		runID := strings.TrimSuffix(filename, ".json")
		// Note: For index.json, runID will be "index"
		return FileSelectedMsg{RunID: runID, Path: p.filePicker.Path}
	}
}

// Render renders the file browser page
func (p *FileBrowserPage) Render() string {
	if p.err != nil {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true)
		helpStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Italic(true)

		errorMsg := fmt.Sprintf("Error: %v", p.err)
		if strings.Contains(p.err.Error(), "parse") || strings.Contains(p.err.Error(), "unmarshal") {
			helpMsg := helpStyle.Render(
				"This file doesn't contain valid Arena result data.\nPlease select a different file.",
			)
			errorMsg = "Invalid JSON file\n\n" + helpMsg
		}

		return lipgloss.NewStyle().
			Width(p.width).
			Height(p.height).
			AlignHorizontal(lipgloss.Center).
			AlignVertical(lipgloss.Center).
			Render(errorStyle.Render(errorMsg))
	}

	// Build the view with border
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12"))
	title := titleStyle.Render("📁 Browse Results")

	// Calculate available height for filepicker
	// Account for border (2) + padding (2) + title (1) = 5 lines of chrome
	availableHeight := p.height - fileBrowserChrome
	if availableHeight < minFileBrowserHeight {
		availableHeight = minFileBrowserHeight
	}

	// Set filepicker height to use available space
	p.filePicker.SetHeight(availableHeight)

	// Build content
	content := lipgloss.JoinVertical(lipgloss.Left, title, p.filePicker.View())

	// Calculate inner width (account for border 2 + padding 4)
	innerWidth := p.width - fileBrowserWidthChrome
	if innerWidth < minFileBrowserWidth {
		innerWidth = minFileBrowserWidth
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, fileBrowserPaddingHoriz).
		Width(innerWidth).
		Render(content)
}

// GetKeyBindings returns the key bindings for this page
func (p *FileBrowserPage) GetKeyBindings() []views.KeyBinding {
	return []views.KeyBinding{
		{Keys: "↑/↓ j/k", Description: "navigate"},
		{Keys: "←/→ h/l", Description: "back/open"},
		{Keys: "enter", Description: "select file"},
		{Keys: "g/G", Description: "top/bottom"},
		{Keys: "q/ctrl+c", Description: "quit"},
	}
}
