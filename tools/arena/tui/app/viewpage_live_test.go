package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
)

// TestConversationViewPage_LiveAppendsAndMeter verifies a live page appends a
// streamed message and renders the audio meter once levels arrive.
func TestConversationViewPage_LiveAppendsAndMeter(t *testing.T) {
	p := NewLiveConversationViewPage("run-1", "scn", "prov", nil)
	p.SetSize(100, 30)

	p.Update(tui.MessageCreatedMsg{ConversationID: "run-1", Index: 0, Role: "user", Content: "hi there"})
	p.Update(tui.AudioLevelMsg{UserLevel: 0.5, AgentLevel: 0.2})

	v := p.View()
	require.Contains(t, v, "hi there", "streamed message should render")
	require.Contains(t, v, "█", "audio meter bar should render once levels arrive")
}

// TestConversationViewPage_CompletionFlipsStatic verifies a RunCompletedMsg for
// this run stops live streaming (further events are no longer consumed by the
// feed).
func TestConversationViewPage_CompletionFlipsStatic(t *testing.T) {
	p := NewLiveConversationViewPage("run-1", "scn", "prov", nil)
	p.SetSize(100, 30)
	require.True(t, p.live)

	p.Update(tui.RunCompletedMsg{RunID: "run-1"})
	require.False(t, p.live, "completion should flip the page to static")
}

// TestConversationViewPage_StaticIgnoresLiveEvents verifies the non-live
// constructor does not stream events into the panel.
func TestConversationViewPage_StaticIgnoresLiveEvents(t *testing.T) {
	p := NewConversationViewPage("run-1", "scn", "prov", nil)
	p.SetSize(100, 30)
	require.False(t, p.live)
	// Should not panic and should not append (no live feed).
	require.NotPanics(t, func() {
		p.Update(tui.MessageCreatedMsg{ConversationID: "run-1", Index: 0, Role: "user", Content: "hi"})
	})
	require.NotContains(t, p.View(), "hi")
}

// TestConversationViewPage_LogsToggle verifies buffered logs surface when the
// 'L' overlay is toggled on.
func TestConversationViewPage_LogsToggle(t *testing.T) {
	p := NewLiveConversationViewPage("run-1", "scn", "prov", nil)
	p.SetSize(100, 30)

	p.Update(logging.Msg{Level: "INFO", Message: "engine call started"})
	require.NotContains(t, p.View(), "engine call started", "logs hidden until toggled")

	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	require.Contains(t, p.View(), "engine call started", "logs visible after toggle")
}

// TestGoldenLiveConversation snapshots a live drill-in after a streamed user
// turn and an audio frame.
func TestGoldenLiveConversation(t *testing.T) {
	for _, sz := range goldenAppSizes {
		t.Run(sz.name, func(t *testing.T) {
			p := NewLiveConversationViewPage("run-1", "checkout", "claude", nil)
			p.SetSize(sz.w, sz.h)
			p.Update(tui.ConversationStartedMsg{ConversationID: "run-1", SystemPrompt: "You are a helpful agent."})
			p.Update(tui.MessageCreatedMsg{ConversationID: "run-1", Index: 0, Role: "user", Content: "refund my order"})
			p.Update(tui.AudioLevelMsg{UserLevel: 0.5, AgentLevel: 0.25})
			out := stripANSI(p.View())
			// Normalize any trailing whitespace differences across terminals.
			out = strings.TrimRight(out, "\n")
			teatest.RequireEqualOutput(t, []byte(out))
		})
	}
}
