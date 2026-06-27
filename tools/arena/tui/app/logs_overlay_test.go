package app

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
)

// TestLogsOverlay_AppendAndCap verifies appended logs render newest-first-visible
// and that the buffer is capped at logsOverlayMaxLines (oldest trimmed).
func TestLogsOverlay_AppendAndCap(t *testing.T) {
	o := NewLogsOverlay()
	o.SetSize(80, 24)
	for i := 0; i < 150; i++ {
		o.Append(logging.Msg{Level: "INFO", Message: fmt.Sprintf("line-%d", i)})
	}
	o.Toggle() // make visible

	v := o.View()
	require.Contains(t, v, "line-149", "newest line should be visible")
	require.NotContains(t, v, "line-0\n", "oldest line should be trimmed")
}

// TestLogsOverlay_HiddenRendersEmpty verifies a hidden overlay renders nothing.
func TestLogsOverlay_HiddenRendersEmpty(t *testing.T) {
	o := NewLogsOverlay()
	o.SetSize(80, 24)
	o.Append(logging.Msg{Level: "INFO", Message: "hello"})
	require.Empty(t, o.View(), "hidden overlay must render empty")
}

// TestLogsOverlay_UpdateAndVisible covers scroll forwarding (only while
// visible) and the Visible accessor.
func TestLogsOverlay_UpdateAndVisible(t *testing.T) {
	o := NewLogsOverlay()
	o.SetSize(80, 24)
	for i := 0; i < 50; i++ {
		o.Append(logging.Msg{Level: "INFO", Message: fmt.Sprintf("line-%d", i)})
	}

	// Hidden: Update is a no-op and reports not visible.
	require.False(t, o.Visible())
	require.Nil(t, o.Update(tea.KeyMsg{Type: tea.KeyPgUp}))

	// Visible: Update forwards to the viewport without panicking.
	o.Toggle()
	require.True(t, o.Visible())
	require.NotPanics(t, func() { o.Update(tea.KeyMsg{Type: tea.KeyPgUp}) })
}

// TestLogsOverlay_NoSizeNoRender verifies appending before SetSize doesn't
// panic and renders nothing meaningful (zero-dimension guard).
func TestLogsOverlay_NoSizeNoRender(t *testing.T) {
	o := NewLogsOverlay()
	require.NotPanics(t, func() { o.Append(logging.Msg{Level: "INFO", Message: "early"}) })
	o.Toggle()
	require.NotPanics(t, func() { _ = o.View() })
}

// TestGoldenLogsOverlay snapshots the overlay with a fixed set of log lines.
func TestGoldenLogsOverlay(t *testing.T) {
	for _, sz := range goldenAppSizes {
		t.Run(sz.name, func(t *testing.T) {
			o := NewLogsOverlay()
			o.SetSize(sz.w, sz.h)
			o.Append(logging.Msg{Level: "INFO", Message: "run started"})
			o.Append(logging.Msg{Level: "WARN", Message: "retrying provider call"})
			o.Append(logging.Msg{Level: "ERROR", Message: "timeout after 30s"})
			o.Toggle()
			out := stripANSI(o.View())
			teatest.RequireEqualOutput(t, []byte(out))
		})
	}
}
