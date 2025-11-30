package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
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
)

// ConversationPane encapsulates the conversation view state (table + detail).
type ConversationPane struct {
	focus       conversationFocus
	table       table.Model
	tableReady  bool
	detail      viewport.Model
	detailReady bool

	selectedTurnIdx int
	lastRunID       string
	width           int
	height          int
}

// NewConversationPane creates an empty conversation pane with defaults.
func NewConversationPane() ConversationPane {
	return ConversationPane{
		focus:           focusConversationTurns,
		selectedTurnIdx: 0,
	}
}

// Reset clears state, used when leaving the conversation view.
func (c *ConversationPane) Reset() {
	c.tableReady = false
	c.detailReady = false
	c.selectedTurnIdx = 0
	c.lastRunID = ""
	c.table = table.Model{}
	c.detail = viewport.Model{}
	c.focus = focusConversationTurns
}

// SetDimensions sets layout constraints.
func (c *ConversationPane) SetDimensions(width, height int) {
	c.width = width
	c.height = height
}

// SetData hydrates the pane with a run and result.
func (c *ConversationPane) SetData(runID string, res *statestore.RunResult) {
	if res == nil {
		c.Reset()
		return
	}

	c.ensureTable(runID)
	c.updateTable(res)
	c.updateDetail(res)
}

func (c *ConversationPane) ensureTable(runID string) {
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
		BorderForeground(lipgloss.Color(colorIndigo)).
		Bold(true)
	style.Selected = style.Selected.
		Foreground(lipgloss.Color(colorWhite)).
		Background(lipgloss.Color(colorIndigo)).
		Bold(true)
	t.SetStyles(style)

	c.table = t
	c.tableReady = true
	c.focus = focusConversationTurns
	c.table.Focus()
	c.lastRunID = runID
}

// Update handles key/scroll input for the conversation pane.
func (c *ConversationPane) Update(msg tea.Msg) (ConversationPane, tea.Cmd) {
	if !c.tableReady && !c.detailReady {
		return *c, nil
	}

	if km, ok := msg.(tea.KeyMsg); ok && km.Type == tea.KeyTab {
		if c.focus == focusConversationTurns {
			c.focus = focusConversationDetail
			c.table.Blur()
		} else {
			c.focus = focusConversationTurns
			c.table.Focus()
		}
		return *c, nil
	}

	if c.focus == focusConversationTurns && c.tableReady {
		var cmd tea.Cmd
		c.table, cmd = c.table.Update(msg)
		c.selectedTurnIdx = c.table.Cursor()
		return *c, cmd
	}

	if c.focus == focusConversationDetail && c.detailReady {
		var cmd tea.Cmd
		c.detail, cmd = c.detail.Update(msg)
		return *c, cmd
	}

	return *c, nil
}

// View renders the conversation pane.
func (c *ConversationPane) View(res *statestore.RunResult) string {
	if res == nil {
		return "No conversation available."
	}

	if len(res.Messages) == 0 {
		return "No conversation recorded."
	}

	c.updateTable(res)
	c.updateDetail(res)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSky))
	title := titleStyle.Render("🧭 Conversation")

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		c.table.View(),
		strings.Repeat(" ", conversationPanelGap),
		c.detail.View(),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorLightBlue)).
		Padding(conversationPanelPadding, conversationPanelHorizontal).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, content))
}

func (c *ConversationPane) updateTable(res *statestore.RunResult) {
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
		snippet := truncateString(msg.GetContent(), conversationSnippetMaxLength)
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

func (c *ConversationPane) updateDetail(res *statestore.RunResult) {
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
	lines := []string{
		fmt.Sprintf("Turn: %d", c.selectedTurnIdx+1),
		fmt.Sprintf("Role: %s", msg.Role),
	}
	if len(msg.ToolCalls) > 0 {
		lines = append(lines, fmt.Sprintf("Tool Calls: %d", len(msg.ToolCalls)))
	}
	if msg.ToolResult != nil && msg.ToolResult.Name != "" {
		lines = append(lines, fmt.Sprintf("Tool Result: %s", msg.ToolResult.Name))
	}
	if msg.ToolResult != nil && msg.ToolResult.Error != "" {
		lines = append(lines, fmt.Sprintf("Error: %s", msg.ToolResult.Error))
	}

	c.appendContentLines(&lines, &msg)
	c.appendValidationLines(&lines, &msg)

	width := c.width - conversationListWidth - conversationPanelGap - conversationDetailWidthPad
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

func (c *ConversationPane) appendContentLines(lines *[]string, msg *types.Message) {
	msgContent := msg.GetContent()
	if msgContent != "" {
		*lines = append(*lines, "", "Message:", msgContent)
	}
}

func (c *ConversationPane) appendValidationLines(lines *[]string, msg *types.Message) {
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
