package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
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

func (m *Model) renderConversationView(run *RunInfo) string {
	if m.stateStore == nil {
		return "No state store attached."
	}

	res, err := m.stateStore.GetResult(context.Background(), run.RunID)
	if err != nil {
		return fmt.Sprintf("Failed to load result: %v", err)
	}

	if len(res.Messages) == 0 {
		return "No conversation recorded."
	}

	if m.selectedTurnIdx < 0 {
		m.selectedTurnIdx = 0
	}
	if m.selectedTurnIdx >= len(res.Messages) {
		m.selectedTurnIdx = len(res.Messages) - 1
	}

	m.ensureConversationTable(res)
	m.updateConversationTable(res)
	m.updateConversationDetail(res)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSky))
	title := titleStyle.Render("🧭 Conversation")

	list := m.convTable.View()
	detail := m.convDetail.View()

	content := lipgloss.JoinHorizontal(lipgloss.Top, list, strings.Repeat(" ", conversationPanelGap), detail)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorLightBlue)).
		Padding(conversationPanelPadding, conversationPanelHorizontal).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, content))
}

func (m *Model) renderTurnDetail(res *statestore.RunResult, idx int) string {
	msg := res.Messages[idx]

	lines := []string{
		fmt.Sprintf("Turn: %d", idx+1),
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

	msgContent := msg.GetContent()
	if msgContent != "" {
		lines = append(lines, "", "Message:", msgContent)
	}

	if len(msg.Validations) > 0 {
		lines = append(lines, "", "Validations:")
		for _, v := range msg.Validations {
			status := "PASS"
			if !v.Passed {
				status = "FAIL"
			}
			lines = append(lines, fmt.Sprintf("  • [%s] %s", status, v.ValidatorType))
		}
	}

	width := m.width - conversationListWidth - conversationPanelGap - conversationDetailWidthPad
	if width < conversationDetailMinWidth {
		width = conversationDetailMinWidth
	}

	height := m.height - conversationTableHeightPad
	if height < conversationDetailMinHeight {
		height = conversationDetailMinHeight
	}

	content := strings.Join(lines, "\n")
	m.convDetail.Width = width
	m.convDetail.Height = height
	m.convDetail.SetContent(content)
	m.convDetailReady = true
	return m.convDetail.View()
}

func (m *Model) ensureConversationTable(res *statestore.RunResult) {
	if m.convTableReady && m.lastConvRunID == res.RunID {
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

	m.convTable = t
	m.convTableReady = true
	m.convFocus = focusConversationTurns
	m.convTable.Focus()
	m.lastConvRunID = res.RunID
}

func (m *Model) updateConversationTable(res *statestore.RunResult) {
	if !m.convTableReady {
		return
	}

	height := m.height - conversationTableHeightPad
	if height < conversationTableMinHeight {
		height = conversationTableMinHeight
	}

	m.convTable.SetHeight(height)
	m.convTable.SetWidth(conversationListWidth)

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
	m.convTable.SetRows(rows)
	if m.selectedTurnIdx >= len(rows) {
		m.selectedTurnIdx = len(rows) - 1
	}
	if m.selectedTurnIdx < 0 {
		m.selectedTurnIdx = 0
	}
	m.convTable.GotoBottom()
	m.convTable.SetCursor(m.selectedTurnIdx)
}

func (m *Model) updateConversationDetail(res *statestore.RunResult) {
	if len(res.Messages) == 0 {
		return
	}
	if m.selectedTurnIdx < 0 {
		m.selectedTurnIdx = 0
	}
	if m.selectedTurnIdx >= len(res.Messages) {
		m.selectedTurnIdx = len(res.Messages) - 1
	}
	m.renderTurnDetail(res, m.selectedTurnIdx)
}
