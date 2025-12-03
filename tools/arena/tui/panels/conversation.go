// Package panels provides reusable panel components for the TUI.
package panels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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
	conversationListWidth        = 40
	conversationPanelPadding     = 1
	conversationPanelGap         = 2
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
	conversationArgsIndent       = 4
	conversationResultIndent     = 2
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
}

// NewConversationPanel creates an empty conversation panel with defaults.
func NewConversationPanel() *ConversationPanel {
	return &ConversationPanel{
		focus:           focusConversationTurns,
		selectedTurnIdx: 0,
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
}

// SetDimensions sets layout constraints.
func (c *ConversationPanel) SetDimensions(width, height int) {
	c.width = width
	c.height = height
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

	if km, ok := msg.(tea.KeyMsg); ok && km.Type == tea.KeyTab {
		if c.focus == focusConversationTurns {
			c.focus = focusConversationDetail
			c.table.Blur()
		} else {
			c.focus = focusConversationTurns
			c.table.Focus()
		}
		return nil
	}

	if c.focus == focusConversationTurns && c.tableReady {
		var cmd tea.Cmd
		c.table, cmd = c.table.Update(msg)
		c.selectedTurnIdx = c.table.Cursor()
		return cmd
	}

	if c.focus == focusConversationDetail && c.detailReady {
		var cmd tea.Cmd
		c.detail, cmd = c.detail.Update(msg)
		return cmd
	}

	return nil
}

// View renders the conversation panel.
func (c *ConversationPanel) View() string {
	if c.res == nil {
		return "No conversation available."
	}

	if len(c.res.Messages) == 0 {
		return "No conversation recorded."
	}

	c.updateTable(c.res)
	c.updateDetail(c.res)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.ColorSky))
	titleText := "🧭 Conversation"
	if c.scenario != "" || c.provider != "" {
		parts := []string{}
		if c.scenario != "" {
			parts = append(parts, c.scenario)
		}
		if c.provider != "" {
			parts = append(parts, c.provider)
		}
		titleText = fmt.Sprintf("%s • %s", titleText, strings.Join(parts, " / "))
	}
	title := titleStyle.Render(titleText)

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		c.table.View(),
		strings.Repeat(" ", conversationPanelGap),
		c.detail.View(),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.ColorLightBlue)).
		Padding(conversationPanelPadding, conversationPanelHorizontal).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, content))
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

	c.table.SetHeight(height)
	c.table.SetWidth(conversationListWidth)

	rows := make([]table.Row, 0, len(res.Messages))
	for i := range res.Messages {
		msg := &res.Messages[i]
		snippet := theme.TruncateString(msg.GetContent(), conversationSnippetMaxLength)
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

	width := c.width - conversationListWidth - conversationPanelGap - conversationDetailWidthPad
	if width < conversationDetailMinWidth {
		width = conversationDetailMinWidth
	}

	height := c.height - conversationTableHeightPad
	if height < conversationDetailMinHeight {
		height = conversationDetailMinHeight
	}

	footer := c.renderDetailFooter()
	content := strings.Join(append(lines, "", footer), "\n")
	c.detail.Width = width
	c.detail.Height = height
	c.detail.SetContent(content)
	c.detailReady = true
}

func (c *ConversationPanel) buildDetailLines(res *statestore.RunResult, msg *types.Message) []string {
	lines := []string{c.renderDetailHeader(res, c.selectedTurnIdx, msg), ""}
	c.appendToolCallLines(&lines, msg)
	c.appendToolResultLines(&lines, msg)

	c.appendContentLines(&lines, msg)
	c.appendValidationLines(&lines, msg)
	return lines
}

func (c *ConversationPanel) appendToolCallLines(lines *[]string, msg *types.Message) {
	if len(msg.ToolCalls) == 0 {
		return
	}
	*lines = append(*lines, fmt.Sprintf("Tool Calls: %d", len(msg.ToolCalls)))
	for _, tc := range msg.ToolCalls {
		*lines = append(*lines, fmt.Sprintf("  • %s", tc.Name))
		if len(tc.Args) > 0 {
			*lines = append(*lines, indentBlock(formatJSON(string(tc.Args)), conversationArgsIndent))
		}
	}
}

func (c *ConversationPanel) appendToolResultLines(lines *[]string, msg *types.Message) {
	if msg.ToolResult == nil || msg.ToolResult.Name == "" {
		return
	}

	*lines = append(*lines, fmt.Sprintf("Tool Result: %s", msg.ToolResult.Name))
	if msg.ToolResult.Content != "" {
		*lines = append(*lines, indentBlock(formatJSON(msg.ToolResult.Content), conversationResultIndent))
	}
	if msg.ToolResult.Error != "" {
		*lines = append(*lines, fmt.Sprintf("Error: %s", msg.ToolResult.Error))
	}
}

func (c *ConversationPanel) appendContentLines(lines *[]string, msg *types.Message) {
	msgContent := msg.GetContent()
	if msgContent != "" {
		*lines = append(*lines, "", "Message:", msgContent)
	}
}

func (c *ConversationPanel) appendValidationLines(lines *[]string, msg *types.Message) {
	if len(msg.Validations) == 0 {
		return
	}
	*lines = append(*lines, "", "Validations:")
	for _, v := range msg.Validations {
		status := "PASS"
		if !v.Passed {
			status = "FAIL"
		}
		*lines = append(*lines, fmt.Sprintf("  • [%s] %s", status, v.ValidatorType))
	}
}

func formatJSON(raw string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(raw), "", "  "); err == nil {
		return buf.String()
	}
	return raw
}

func indentBlock(text string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (c *ConversationPanel) renderDetailHeader(res *statestore.RunResult, idx int, msg *types.Message) string {
	totalTokens := res.Cost.InputTokens + res.Cost.OutputTokens
	header := fmt.Sprintf(
		"Turn %d • Role: %s • Tokens: %d/%d (total %d) • Cost: $%.4f • Duration: %s",
		idx+1,
		msg.Role,
		res.Cost.InputTokens,
		res.Cost.OutputTokens,
		totalTokens,
		res.Cost.TotalCost,
		theme.FormatDuration(res.Duration),
	)

	return lipgloss.NewStyle().
		Foreground(theme.BorderColorFocused()).
		Bold(true).
		Render(header)
}

func (c *ConversationPanel) renderDetailFooter() string {
	footer := "↑/↓ scroll • tab focus • esc back"
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorLightGray)).
		Render(footer)
}
