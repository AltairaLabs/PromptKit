package panels

import (
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

const (
	logsHeightDivisor   = 3
	logsMinHeight       = 5
	logsWidthPadding    = 50
	logsMinWidth        = 40
	logsViewportOffset  = 15
	logsViewportDivisor = 2
)

// LogEntry represents a single log line
type LogEntry struct {
	Level   string
	Message string
}

// LogsPanel manages the logs viewport display
type LogsPanel struct {
	viewport viewport.Model
	ready    bool
	width    int
}

// NewLogsPanel creates a new logs panel
func NewLogsPanel() *LogsPanel {
	return &LogsPanel{}
}

// Init initializes the logs viewport
func (p *LogsPanel) Init(width, height int) {
	viewportHeight := (height - logsViewportOffset) / logsViewportDivisor
	if viewportHeight < logsMinHeight {
		viewportHeight = logsMinHeight
	}
	viewportWidth := width - logsWidthPadding
	if viewportWidth < logsMinWidth {
		viewportWidth = logsMinWidth
	}

	p.viewport = viewport.New(viewportWidth, viewportHeight)
	p.viewport.SetContent("Waiting for logs...")
	p.width = width
	p.ready = true
}

// Update updates the viewport with log data and dimensions
func (p *LogsPanel) Update(logs []LogEntry, width, height int) {
	if !p.ready {
		p.Init(width, height)
		return
	}

	// Update dimensions
	viewportHeight := height / logsHeightDivisor
	if viewportHeight < logsMinHeight {
		viewportHeight = logsMinHeight
	}
	viewportWidth := width - logsWidthPadding
	if viewportWidth < logsMinWidth {
		viewportWidth = logsMinWidth
	}

	p.viewport.Width = viewportWidth
	p.viewport.Height = viewportHeight
	p.width = width

	// Convert logs to view format
	viewLogs := make([]views.LogEntry, len(logs))
	for i := range logs {
		viewLogs[i] = views.LogEntry{
			Level:   logs[i].Level,
			Message: logs[i].Message,
		}
	}
	p.viewport.SetContent(views.FormatLogLines(viewLogs))
}

// View renders the logs panel
func (p *LogsPanel) View(focused bool) string {
	logsView := views.NewLogsView(focused)
	return logsView.Render(&p.viewport, p.ready, p.width)
}

// Viewport returns the underlying viewport for key handling
func (p *LogsPanel) Viewport() *viewport.Model {
	return &p.viewport
}

// GotoBottom scrolls to the bottom of the logs
func (p *LogsPanel) GotoBottom() {
	if p.ready {
		p.viewport.GotoBottom()
	}
}
