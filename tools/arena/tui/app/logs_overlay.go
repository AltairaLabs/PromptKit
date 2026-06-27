package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
)

// logsOverlayMaxLines bounds the in-memory log buffer so a long-running
// session can't grow it without limit. Mirrors the run model's 100-line cap.
const logsOverlayMaxLines = 100

// LogsOverlay is a reusable, toggleable log view. Hosts (conversation
// drill-in, chat) feed it logging.Msg values; it buffers them (capped) and
// renders the shared LogsPanel when visible. It is the in-page counterpart to
// the run monitor's Logs pane, so runtime logs are inspectable everywhere a
// hub TUI owns the screen instead of corrupting it.
type LogsOverlay struct {
	panel   *panels.LogsPanel
	entries []panels.LogEntry
	w, h    int
	visible bool
}

// NewLogsOverlay creates an empty, hidden LogsOverlay.
func NewLogsOverlay() *LogsOverlay {
	return &LogsOverlay{panel: panels.NewLogsPanel()}
}

// Append records a log line, trimming the oldest beyond the cap, and scrolls
// to the newest line.
func (o *LogsOverlay) Append(m logging.Msg) {
	o.entries = append(o.entries, panels.LogEntry{Level: m.Level, Message: m.Message})
	if len(o.entries) > logsOverlayMaxLines {
		o.entries = o.entries[len(o.entries)-logsOverlayMaxLines:]
	}
	o.refresh()
	o.panel.GotoBottom()
}

// SetSize stores the overlay dimensions and re-lays the panel.
func (o *LogsOverlay) SetSize(w, h int) {
	o.w, o.h = w, h
	o.refresh()
}

// Update forwards scroll keys to the underlying viewport while visible.
func (o *LogsOverlay) Update(msg tea.Msg) tea.Cmd {
	if !o.visible {
		return nil
	}
	vp, cmd := o.panel.Viewport().Update(msg)
	*o.panel.Viewport() = vp
	return cmd
}

// View renders the logs when visible, empty otherwise.
func (o *LogsOverlay) View() string {
	if !o.visible {
		return ""
	}
	return o.panel.View(true)
}

// Visible reports whether the overlay is shown.
func (o *LogsOverlay) Visible() bool { return o.visible }

// Toggle flips visibility.
func (o *LogsOverlay) Toggle() { o.visible = !o.visible }

// refresh pushes the current buffer + dimensions into the panel.
func (o *LogsOverlay) refresh() {
	if o.w == 0 || o.h == 0 {
		return
	}
	o.panel.Update(o.entries, o.w, o.h)
}
