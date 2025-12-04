// Package pages provides top-level page components for the TUI.
package pages

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/results"
	"github.com/AltairaLabs/PromptKit/tools/arena/results/filesystem"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// FileBrowserPage renders the file browser view for viewing past results
type FileBrowserPage struct {
	width      int
	height     int
	filePicker filepicker.Model
	reader     results.ResultReader
	metadata   []results.ResultMetadata
	selected   *statestore.RunResult
	err        error
	loading    bool
}

// NewFileBrowserPage creates a new file browser page
func NewFileBrowserPage(resultsDir string) *FileBrowserPage {
	fp := filepicker.New()
	fp.CurrentDirectory = resultsDir
	fp.AllowedTypes = []string{".json"}
	fp.ShowHidden = false
	fp.DirAllowed = true
	fp.FileAllowed = true

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
	}

	reader := filesystem.NewFilesystemResultReader(resultsDir)

	return &FileBrowserPage{
		filePicker: fp,
		reader:     reader,
		loading:    true,
	}
}

// Init initializes the file browser page (tea.Model interface)
func (p *FileBrowserPage) Init() tea.Cmd {
	return tea.Batch(
		p.filePicker.Init(),
		p.loadMetadata,
	)
}

// View renders the file browser page (tea.Model interface)
func (p *FileBrowserPage) View() string {
	return p.Render()
}

// loadMetadata loads result metadata from the reader
func (p *FileBrowserPage) loadMetadata() tea.Msg {
	metadata, err := p.reader.ListResults()
	if err != nil {
		return fileBrowserErrorMsg{err: err}
	}
	return fileBrowserMetadataMsg{metadata: metadata}
}

// fileBrowserMetadataMsg is sent when metadata is loaded
type fileBrowserMetadataMsg struct {
	metadata []results.ResultMetadata
}

// fileBrowserErrorMsg is sent when an error occurs
type fileBrowserErrorMsg struct {
	err error
}

// fileBrowserResultMsg is sent when a result is selected and loaded
type fileBrowserResultMsg struct {
	result *statestore.RunResult
}

// SetDimensions updates the page dimensions
func (p *FileBrowserPage) SetDimensions(width, height int) {
	p.width = width
	p.height = height
	p.filePicker.Height = height - 10 // Reserve space for header and metadata display //nolint:mnd // UI spacing constant
}

// Update handles input for the file browser (tea.Model interface)
func (p *FileBrowserPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fileBrowserMetadataMsg:
		p.metadata = msg.metadata
		p.loading = false
		return p, nil

	case fileBrowserErrorMsg:
		p.err = msg.err
		p.loading = false
		return p, nil

	case fileBrowserResultMsg:
		p.selected = msg.result
		return p, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return p, tea.Quit
		}
	}

	var cmd tea.Cmd
	p.filePicker, cmd = p.filePicker.Update(msg)

	// Check if a file was selected
	if didSelect, path := p.filePicker.DidSelectFile(msg); didSelect {
		// Load the result
		return p, p.loadResult(path)
	}

	return p, cmd
}

// loadResult loads a result from the selected file
func (p *FileBrowserPage) loadResult(path string) tea.Cmd {
	return func() tea.Msg {
		// Extract runID from filename (assuming format like runID.json)
		filename := filepath.Base(path)
		runID := strings.TrimSuffix(filename, ".json")

		result, err := p.reader.LoadResult(runID)
		if err != nil {
			return fileBrowserErrorMsg{err: err}
		}
		return fileBrowserResultMsg{result: result}
	}
}

// Render renders the file browser page
func (p *FileBrowserPage) Render() string {
	if p.loading {
		return lipgloss.NewStyle().
			Width(p.width).
			Height(p.height).
			AlignHorizontal(lipgloss.Center).
			AlignVertical(lipgloss.Center).
			Render("Loading results...")
	}

	if p.err != nil {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true)
		return lipgloss.NewStyle().
			Width(p.width).
			Height(p.height).
			AlignHorizontal(lipgloss.Center).
			AlignVertical(lipgloss.Center).
			Render(errorStyle.Render(fmt.Sprintf("Error: %v", p.err)))
	}

	// Build the view
	var sections []string

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Padding(0, 1)
	sections = append(sections, titleStyle.Render("📁 Browse Results"))

	// Metadata summary
	if len(p.metadata) > 0 {
		summaryStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Padding(0, 1)
		sections = append(sections, summaryStyle.Render(
			fmt.Sprintf("Found %d results", len(p.metadata)),
		))
	}

	// File picker
	sections = append(sections, p.filePicker.View())

	// Selected result preview
	if p.selected != nil {
		sections = append(sections, p.renderResultPreview())
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderResultPreview renders a preview of the selected result
func (p *FileBrowserPage) renderResultPreview() string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(1, 2).     //nolint:mnd // UI padding constants
		Width(p.width - 4) //nolint:mnd // Border width adjustment

	previewStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7"))

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)

	var lines []string
	lines = append(lines, labelStyle.Render("Selected Result:"))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("RunID:"), p.selected.RunID))
	lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("Scenario:"), p.selected.ScenarioID))
	lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("Provider:"), p.selected.ProviderID))
	lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("Region:"), p.selected.Region))

	if p.selected.Error != "" {
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("Status:"), errorStyle.Render("Failed")))
		lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("Error:"), errorStyle.Render(p.selected.Error)))
	} else {
		successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("Status:"), successStyle.Render("Success")))
	}

	lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("Duration:"), p.selected.Duration.String()))
	lines = append(lines, fmt.Sprintf("%s $%.4f", labelStyle.Render("Cost:"), p.selected.Cost.TotalCost))
	lines = append(lines, fmt.Sprintf("%s %d", labelStyle.Render("Messages:"), len(p.selected.Messages)))

	return borderStyle.Render(previewStyle.Render(strings.Join(lines, "\n")))
}

// SelectedResult returns the currently selected result
func (p *FileBrowserPage) SelectedResult() *statestore.RunResult {
	return p.selected
}

// GetMetadata returns the loaded metadata
func (p *FileBrowserPage) GetMetadata() []results.ResultMetadata {
	return p.metadata
}
