package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
)

// TestChatPage_LogsToggle verifies runtime logs are buffered and revealed in
// chat via the ctrl+l overlay (so logs are inspectable without corrupting the
// chat screen).
func TestChatPage_LogsToggle(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.state = chatStateChat
	p.SetSize(100, 30)

	p.Update(logging.Msg{Level: "INFO", Message: "engine call started"})
	require.NotContains(t, p.View(), "engine call started", "logs hidden until toggled")

	p.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	require.Contains(t, p.View(), "engine call started", "logs visible after ctrl+l")

	p.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	require.NotContains(t, p.View(), "engine call started", "logs hidden after second ctrl+l")
}
