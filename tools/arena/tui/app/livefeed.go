package app

import (
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
)

// liveFeed applies streamed runtime events to a ConversationPanel during a
// live (in-progress) drill-in. It dedups message events by index against the
// snapshot already seeded into the panel, so the seed→tail boundary can't
// double-count an in-flight turn.
//
// seeded is the number of streamed-turn messages already present in the panel
// (e.g. len of the store snapshot used to seed it; 0 for an empty seed). A
// system prompt added via ConversationStarted is handled separately and does
// not participate in this index arithmetic.
type liveFeed struct {
	convID   string
	seeded   int
	appended int
}

func newLiveFeed(convID string, seeded int) *liveFeed {
	return &liveFeed{convID: convID, seeded: seeded}
}

// Apply routes msg into panel. It returns true when msg was consumed: an
// audio-level frame (always), or a message/conversation event for this feed's
// conversation. Events for other conversations return false so the host can
// handle them.
func (f *liveFeed) Apply(panel *panels.ConversationPanel, msg tea.Msg) bool {
	switch m := msg.(type) {
	case tui.AudioLevelMsg:
		panel.SetAudioLevels(m.UserLevel, m.AgentLevel, true)
		return true
	case tui.ConversationStartedMsg:
		if m.ConversationID != f.convID {
			return false
		}
		if !panel.HasSystemPrompt() {
			panel.PrependSystemPrompt(&types.Message{
				Role:      "system",
				Content:   m.SystemPrompt,
				Timestamp: m.Time,
			})
		}
		return true
	case tui.ReasoningDeltaMsg:
		// Non-content live thinking — accumulate transiently for this turn.
		panel.AppendLiveReasoning(m.Text)
		return true
	case tui.MessageCreatedMsg:
		if m.ConversationID != f.convID {
			return false
		}
		// The turn's message arrived — clear the transient thinking display.
		panel.ClearLiveReasoning()
		f.appendCreated(panel, &m)
		return true
	case tui.MessageUpdatedMsg:
		if m.ConversationID != f.convID {
			return false
		}
		panel.UpdateMessageMetadata(m.Index, m.LatencyMs, types.CostInfo{
			InputTokens:  m.InputTokens,
			OutputTokens: m.OutputTokens,
			TotalCost:    m.TotalCost,
		})
		return true
	}
	return false
}

// appendCreated converts a MessageCreatedMsg to a types.Message and appends it,
// skipping turns whose index is already covered by the seed or prior appends.
func (f *liveFeed) appendCreated(panel *panels.ConversationPanel, m *tui.MessageCreatedMsg) {
	if m.Index < f.seeded+f.appended {
		return // already have this turn (seed/tail overlap)
	}

	var toolCalls []types.MessageToolCall
	for _, tc := range m.ToolCalls {
		toolCalls = append(toolCalls, types.MessageToolCall{
			ID:   tc.ID,
			Name: tc.Name,
			Args: json.RawMessage(tc.Args),
		})
	}

	var toolResult *types.MessageToolResult
	if m.ToolResult != nil {
		parts := make([]types.ContentPart, len(m.ToolResult.Parts))
		copy(parts, m.ToolResult.Parts)
		toolResult = &types.MessageToolResult{
			ID:        m.ToolResult.ID,
			Name:      m.ToolResult.Name,
			Parts:     parts,
			Error:     m.ToolResult.Error,
			LatencyMs: m.ToolResult.LatencyMs,
		}
	}

	panel.AppendMessage(&types.Message{
		Role:       m.Role,
		Content:    m.Content,
		Timestamp:  m.Time,
		ToolCalls:  toolCalls,
		ToolResult: toolResult,
		Reasoning:  m.Reasoning,
	})
	f.appended = m.Index - f.seeded + 1
}
