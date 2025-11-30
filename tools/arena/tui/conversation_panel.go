package tui

import (
	"context"
	"fmt"
	"strings"

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

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSky))
	title := titleStyle.Render("🧭 Conversation")

	list := m.renderTurnList(res, m.selectedTurnIdx)
	detail := m.renderTurnDetail(res, m.selectedTurnIdx)

	content := lipgloss.JoinHorizontal(lipgloss.Top, list, strings.Repeat(" ", conversationPanelGap), detail)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorLightBlue)).
		Padding(conversationPanelPadding, conversationPanelHorizontal).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, content))
}

func (m *Model) renderTurnList(res *statestore.RunResult, selectedIdx int) string {
	rows := make([]string, 0, len(res.Messages))
	for i := range res.Messages {
		msg := &res.Messages[i]
		snippet := truncateString(msg.GetContent(), conversationSnippetMaxLength)
		line := fmt.Sprintf("%2d. %-9s %s", i+1, msg.Role, snippet)
		if i == selectedIdx {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorWhite)).
				Background(lipgloss.Color(colorIndigo)).
				Padding(0, 1).
				Render(line)
		}
		rows = append(rows, line)
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorGray)).
		Width(conversationListWidth).
		Padding(1, 1).
		Render(strings.Join(rows, "\n"))
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

	content := msg.GetContent()
	if content != "" {
		lines = append(lines, "", "Message:", content)
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

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorGray)).
		Width(width).
		Padding(1, 1).
		Render(strings.Join(lines, "\n"))
}
