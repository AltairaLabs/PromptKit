// Package panels provides reusable panel components for the TUI.
package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

type conversationFocus int

const (
	focusConversationTurns conversationFocus = iota
	focusConversationDetail
)

const (
	// audioMeterCells is the width of each level bar in cells.
	audioMeterCells = 16
	// audioMeterFilled is the rune used for filled cells in the level bar.
	audioMeterFilled = "█"
	// audioMeterEmpty is the rune used for unfilled cells in the level bar — a
	// visible glyph rather than a space so the empty bar's boundary stays
	// readable when the level is 0.
	audioMeterEmpty = "░"
)

const (
	// conversationListWidth is the fallback turns-list width before the panel
	// has been sized; once sized, listWidth() scales it to the panel width.
	conversationListWidth = 40
	// conversationListPct is the share of the panel width the turns list takes
	// (the detail pane gets the rest), so the list grows on wide terminals.
	conversationListPct          = 38
	conversationPctDenominator   = 100
	conversationListMinWidth     = 24
	conversationPanelPadding     = 1
	conversationPanelGap         = 1
	conversationPanelHorizontal  = 2
	conversationSnippetMaxLength = 60
	conversationDetailWidthPad   = 6
	conversationDetailMinWidth   = 40
	conversationTableHeightPad   = 6
	conversationTableMinHeight   = 6
	conversationDetailMinHeight  = 6
	conversationColNumWidth      = 4
	conversationColRoleWidth     = 10
	conversationContentPadding   = 20
	scrollPercentThreshold       = 0.01
	scrollPercentMultiplier      = 100
	markdownExtraWidthPadding    = 4
)

// ConversationPanel encapsulates the conversation view state (table + detail).
type ConversationPanel struct {
	focus       conversationFocus
	table       table.Model
	tableReady  bool
	detail      viewport.Model
	detailReady bool

	selectedTurnIdx int
	lastRunID       string
	width           int
	height          int

	scenario string
	provider string
	res      *statestore.RunResult

	// inactive dims both sub-pane borders so an external owner (e.g. the chat
	// input box) can hold the visible focus. The zero value is active/focused,
	// preserving the run dashboard's behavior; only the chat sets it.
	inactive bool

	// Cache for glamour rendering
	renderer         *glamour.TermRenderer
	renderedCache    map[int]string // map[turnIndex]renderedContent
	lastContentWidth int

	// Audio level meter state. audioActive gates rendering — meters are
	// only drawn when at least one AudioLevelMsg has been received.
	audioUserLevel  float32
	audioAgentLevel float32
	audioActive     bool
}

// NewConversationPanel creates an empty conversation panel with defaults.
func NewConversationPanel() *ConversationPanel {
	return &ConversationPanel{
		focus:           focusConversationTurns,
		selectedTurnIdx: 0,
		renderedCache:   make(map[int]string),
	}
}

// Reset clears state, used when leaving the conversation view.
func (c *ConversationPanel) Reset() {
	c.tableReady = false
	c.detailReady = false
	c.selectedTurnIdx = 0
	c.lastRunID = ""
	c.table = table.Model{}
	c.detail = viewport.Model{}
	c.focus = focusConversationTurns
	c.renderedCache = make(map[int]string)
	c.renderer = nil
	c.lastContentWidth = 0
	c.audioUserLevel = 0
	c.audioAgentLevel = 0
	c.audioActive = false
}

// SetDimensions sets layout constraints.
func (c *ConversationPanel) SetDimensions(width, height int) {
	c.width = width
	c.height = height
}

// SetActive controls whether the panel renders with a focused border. Owners
// that embed the panel alongside another focusable widget (e.g. the chat input
// box) call SetActive(false) when the other widget holds focus, so only one
// region looks focused at a time. Defaults to active.
func (c *ConversationPanel) SetActive(active bool) {
	c.inactive = !active
}

// SetAudioLevels updates the audio level meter state. Once active is true,
// the meter is rendered in the conversation panel until Reset is called.
func (c *ConversationPanel) SetAudioLevels(userLevel, agentLevel float32, active bool) {
	c.audioUserLevel = userLevel
	c.audioAgentLevel = agentLevel
	c.audioActive = active
}

// SetData hydrates the panel with a run and result.
func (c *ConversationPanel) SetData(runID, scenario, provider string, res *statestore.RunResult) {
	if res == nil {
		c.tableReady = false
		c.detailReady = false
		c.selectedTurnIdx = 0
		c.lastRunID = ""
		c.table = table.Model{}
		c.detail = viewport.Model{}
		c.focus = focusConversationTurns
		c.res = nil
		return
	}

	c.res = res
	c.ensureTable(runID)
	c.updateTable(res)
	c.updateDetail(res)
	c.scenario = scenario
	c.provider = provider
}

// HasSystemPrompt checks if the conversation already has a system prompt message
func (c *ConversationPanel) HasSystemPrompt() bool {
	if c.res == nil || len(c.res.Messages) == 0 {
		return false
	}
	return c.res.Messages[0].Role == "system"
}

// PrependSystemPrompt adds a system prompt as the first message in the conversation
func (c *ConversationPanel) PrependSystemPrompt(msg *types.Message) {
	if c.res == nil {
		return
	}
	// Prepend by creating new slice with system message first
	c.res.Messages = append([]types.Message{*msg}, c.res.Messages...)
	// Invalidate render cache
	c.renderedCache = make(map[int]string)
	// Rebuild table
	c.updateTable(c.res)
	// Adjust selected index since we prepended
	if c.selectedTurnIdx >= 0 {
		c.selectedTurnIdx++
	}
	c.updateDetail(c.res)
}

// AppendMessage adds a new message to the conversation in real-time
func (c *ConversationPanel) AppendMessage(msg *types.Message) {
	if c.res == nil {
		return
	}
	c.res.Messages = append(c.res.Messages, *msg)
	// Invalidate render cache since we added a message
	c.renderedCache = make(map[int]string)
	// Rebuild table to show the new message
	c.updateTable(c.res)
	// Update detail view if we're viewing the last message
	if c.selectedTurnIdx == len(c.res.Messages)-2 {
		// Auto-advance to new message if we were viewing the previous last message
		c.selectedTurnIdx = len(c.res.Messages) - 1
	}
	c.updateDetail(c.res)
}

// SelectedTurnIdx returns the index of the currently selected message, or 0 if
// no message is selected.
func (c *ConversationPanel) SelectedTurnIdx() int {
	return c.selectedTurnIdx
}

// MessageCount returns the number of messages currently held (including any
// prepended system prompt), or zero when no data has been loaded.
func (c *ConversationPanel) MessageCount() int {
	if c.res == nil {
		return 0
	}
	return len(c.res.Messages)
}

// SelectLast moves the selection to the last message and refreshes the detail
// pane. It is a no-op when the panel has no data or the table is not yet ready.
func (c *ConversationPanel) SelectLast() {
	if c.res == nil || len(c.res.Messages) == 0 || !c.tableReady {
		return
	}
	c.selectedTurnIdx = len(c.res.Messages) - 1
	c.table.SetCursor(c.selectedTurnIdx)
	c.updateDetail(c.res)
}

// UpdateMessageMetadata updates cost/latency for a message
func (c *ConversationPanel) UpdateMessageMetadata(index int, latencyMs int64, costInfo types.CostInfo) {
	if c.res == nil || index < 0 || index >= len(c.res.Messages) {
		return
	}
	c.res.Messages[index].LatencyMs = latencyMs
	c.res.Messages[index].CostInfo = &costInfo
	// Invalidate cache for this specific message
	delete(c.renderedCache, index)
}

// listWidth returns the responsive turns-list width: a share of the panel
// width, clamped so the detail pane keeps its minimum and the list stays
// readable. This lets the turns list grow to fill a wide terminal instead of
// staying at a fixed width.
func (c *ConversationPanel) listWidth() int {
	if c.width <= 0 {
		return conversationListWidth
	}
	w := c.width * conversationListPct / conversationPctDenominator
	if maxW := c.width - conversationDetailMinWidth - conversationPanelGap - conversationDetailWidthPad; w > maxW {
		w = maxW
	}
	if w < conversationListMinWidth {
		w = conversationListMinWidth
	}
	return w
}

func (c *ConversationPanel) ensureTable(runID string) {
	if c.tableReady && c.lastRunID == runID {
		return
	}

	columns := []table.Column{
		{Title: "#", Width: conversationColNumWidth},
		{Title: "Role", Width: conversationColRoleWidth},
		{Title: "Content", Width: conversationListWidth - conversationContentPadding},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
	)

	style := table.DefaultStyles()
	style.Header = style.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(theme.BorderColorFocused()).
		Bold(true)
	style.Selected = style.Selected.
		Foreground(lipgloss.Color(theme.ColorWhite)).
		Background(theme.BorderColorFocused()).
		Bold(true)
	t.SetStyles(style)

	c.table = t
	c.tableReady = true
	c.focus = focusConversationTurns
	c.table.Focus()
	c.lastRunID = runID
}

// Update handles key/scroll input for the conversation panel.
func (c *ConversationPanel) Update(msg tea.Msg) tea.Cmd {
	if !c.tableReady && !c.detailReady {
		return nil
	}

	if cmd := c.handleKeyMsg(msg); cmd != nil {
		return cmd
	}

	return c.updateFocusedPanel(msg)
}

func (c *ConversationPanel) handleKeyMsg(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch km.String() {
	case "right", "l":
		if c.focus == focusConversationTurns {
			c.focus = focusConversationDetail
			c.table.Blur()
			return tea.Batch()
		}
	case "left", "h":
		if c.focus == focusConversationDetail {
			c.focus = focusConversationTurns
			c.table.Focus()
			return tea.Batch()
		}
	}
	return nil
}

func (c *ConversationPanel) updateFocusedPanel(msg tea.Msg) tea.Cmd {
	if c.focus == focusConversationTurns && c.tableReady {
		return c.updateTablePanel(msg)
	}

	if c.focus == focusConversationDetail && c.detailReady {
		var cmd tea.Cmd
		c.detail, cmd = c.detail.Update(msg)
		return cmd
	}

	return nil
}

func (c *ConversationPanel) updateTablePanel(msg tea.Msg) tea.Cmd {
	oldCursor := c.selectedTurnIdx
	var cmd tea.Cmd
	c.table, cmd = c.table.Update(msg)
	c.selectedTurnIdx = c.table.Cursor()

	// Reset viewport scroll position when cursor moves to a new message
	if oldCursor != c.selectedTurnIdx && c.detailReady {
		c.detail.GotoTop()
	}
	return cmd
}

// updateTable updates the conversation table with messages from result.
func (c *ConversationPanel) updateTable(res *statestore.RunResult) {
	if !c.tableReady {
		return
	}

	height := c.height - conversationTableHeightPad
	if height < conversationTableMinHeight {
		height = conversationTableMinHeight
	}

	lw := c.listWidth()
	c.table.SetHeight(height)
	c.table.SetWidth(lw)
	c.table.SetColumns([]table.Column{
		{Title: "#", Width: conversationColNumWidth},
		{Title: "Role", Width: conversationColRoleWidth},
		{Title: "Content", Width: lw - conversationColNumWidth - conversationColRoleWidth - conversationContentPadding},
	})

	rows := make([]table.Row, 0, len(res.Messages))
	for i := range res.Messages {
		msg := &res.Messages[i]
		var snippet string

		// Handle tool result messages - show tool name instead of JSON
		if msg.ToolResult != nil {
			toolName := msg.ToolResult.Name
			if toolName == "" {
				toolName = "unknown"
			}
			snippet = fmt.Sprintf("[%s result]", toolName)
		} else if msg.Role == "assistant" && len(msg.ToolCalls) > 0 && msg.GetContent() == "" {
			// Assistant message with only tool calls (no text content)
			snippet = fmt.Sprintf("[Tool Calls (%d)]", len(msg.ToolCalls))
		} else {
			// Regular message content
			snippet = theme.TruncateString(msg.GetContent(), conversationSnippetMaxLength)
		}

		rows = append(rows, table.Row{
			fmt.Sprintf("%d", i+1),
			msg.Role,
			snippet,
		})
	}
	c.table.SetRows(rows)
	if c.selectedTurnIdx >= len(rows) {
		c.selectedTurnIdx = len(rows) - 1
	}
	if c.selectedTurnIdx < 0 {
		c.selectedTurnIdx = 0
	}
	c.table.SetCursor(c.selectedTurnIdx)
}

// updateDetail updates the detail viewport with the currently selected turn.
func (c *ConversationPanel) updateDetail(res *statestore.RunResult) {
	if len(res.Messages) == 0 {
		return
	}
	if c.selectedTurnIdx < 0 {
		c.selectedTurnIdx = 0
	}
	if c.selectedTurnIdx >= len(res.Messages) {
		c.selectedTurnIdx = len(res.Messages) - 1
	}

	msg := res.Messages[c.selectedTurnIdx]
	lines := c.buildDetailLines(res, &msg)

	width := c.width - c.listWidth() - conversationPanelGap - conversationDetailWidthPad
	if width < conversationDetailMinWidth {
		width = conversationDetailMinWidth
	}

	height := c.height - conversationTableHeightPad
	if height < conversationDetailMinHeight {
		height = conversationDetailMinHeight
	}

	content := strings.Join(lines, "\n")
	c.detail.Width = width
	c.detail.Height = height
	c.detail.SetContent(content)
	c.detailReady = true
}

func (c *ConversationPanel) buildDetailLines(res *statestore.RunResult, msg *types.Message) []string {
	// Build markdown content
	var md strings.Builder

	// Add header (not markdown, keep styled)
	header := c.renderDetailHeader(res, c.selectedTurnIdx, msg)

	// Build markdown content for the body
	c.appendToolCallsMarkdown(&md, msg)
	c.appendToolResultMarkdown(&md, msg)
	c.appendContentMarkdown(&md, msg)
	c.appendValidationsMarkdown(&md, msg)

	// Render the markdown content
	renderedBody := c.renderMarkdown(md.String())

	// Combine header with rendered body
	lines := []string{header, ""}
	if renderedBody != "" {
		lines = append(lines, renderedBody)
	}
	return lines
}
