// Package panels provides reusable panel components for the TUI.
package panels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/layout"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

// View renders the conversation panel.
func (c *ConversationPanel) View() string {
	if c.res == nil {
		return "No conversation available."
	}

	// Always show two-panel layout, even with no messages
	if len(c.res.Messages) == 0 {
		return c.buildEmptyConversationView()
	}

	c.updateTable(c.res)
	c.updateDetail(c.res)

	title := c.buildTitle()
	tableView := c.buildTableView()
	detailView := c.buildDetailView()

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		tableView,
		strings.Repeat(" ", conversationPanelGap),
		detailView,
	)

	return c.composeView(title, content)
}

// composeView stacks the title, an optional audio meter, and the two-pane
// content using the layout engine. The audio meter is an optional element that
// drops out cleanly when there is no active audio (no manual append/join math).
func (c *ConversationPanel) composeView(title, content string) string {
	meter := renderAudioMeters(c.audioUserLevel, c.audioAgentLevel, c.audioActive)
	tree := layout.VSplit(
		layout.Pane("title"),
		layout.Optional(meter != "", layout.Pane("audio")),
		layout.Pane("content"),
	)
	body := layout.RenderTree(tree, map[string]string{
		"title":   title,
		"audio":   meter,
		"content": content,
	})
	return lipgloss.NewStyle().
		Padding(conversationPanelPadding, conversationPanelHorizontal).
		Render(body)
}

// buildEmptyConversationView renders the two-panel layout with empty table and waiting message
func (c *ConversationPanel) buildEmptyConversationView() string {
	title := c.buildTitle()

	// Share the focus/active-aware border colors with the populated view so the
	// empty startup state dims correctly when the panel is inactive (e.g. the
	// chat input box holds focus).
	tableBorderColor, detailBorderColor := c.getBorderColors()

	emptyTableContent := c.table.View()
	tableView := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tableBorderColor).
		Render(emptyTableContent)

	width := c.width - conversationListWidth - conversationPanelGap - conversationDetailWidthPad
	if width < conversationDetailMinWidth {
		width = conversationDetailMinWidth
	}

	height := c.height - conversationTableHeightPad
	if height < conversationDetailMinHeight {
		height = conversationDetailMinHeight
	}

	waitingMessage := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorLightGray)).
		Italic(true).
		Width(width).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Render("Waiting for conversation to start...")

	detailView := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(detailBorderColor).
		Width(width).
		Height(height).
		Render(waitingMessage)

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		tableView,
		strings.Repeat(" ", conversationPanelGap),
		detailView,
	)

	return c.composeView(title, content)
}

// renderAudioMeter formats a single 8-cell level bar from a 0.0–1.0 level.
func renderAudioMeter(label string, level float32) string {
	clamped := level
	if clamped < 0 {
		clamped = 0
	}
	if clamped > 1 {
		clamped = 1
	}
	filled := int(float32(audioMeterCells) * clamped)
	if filled > audioMeterCells {
		filled = audioMeterCells
	}
	bar := strings.Repeat(audioMeterFilled, filled) +
		strings.Repeat(audioMeterEmpty, audioMeterCells-filled)
	const percentScale = 100.0
	return fmt.Sprintf("  %-5s [%s] %3.0f%%", label, bar, clamped*percentScale)
}

// renderAudioMeters builds the two-line "user / agent" level meter block.
// Empty string when no audio data has arrived yet.
func renderAudioMeters(userLevel, agentLevel float32, active bool) string {
	if !active {
		return ""
	}
	return renderAudioMeter("user", userLevel) + "\n" +
		renderAudioMeter("agent", agentLevel)
}

func (c *ConversationPanel) buildTitle() string {
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

	return titleStyle.Render(titleText)
}

func (c *ConversationPanel) getBorderColors() (tableBorderColor, detailBorderColor lipgloss.Color) {
	tableBorderColor = theme.BorderColorUnfocused()
	detailBorderColor = theme.BorderColorUnfocused()

	// When the panel is not the active widget (the chat input holds focus),
	// keep both borders dim so focus reads correctly.
	if c.inactive {
		return
	}

	switch c.focus {
	case focusConversationTurns:
		tableBorderColor = theme.BorderColorFocused()
	case focusConversationDetail:
		detailBorderColor = theme.BorderColorFocused()
	}

	return
}

func (c *ConversationPanel) buildTableView() string {
	tableBorderColor, _ := c.getBorderColors()

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tableBorderColor).
		Render(c.table.View())
}

func (c *ConversationPanel) buildDetailView() string {
	_, detailBorderColor := c.getBorderColors()
	detailContent := c.detail.View()

	if c.detailReady {
		detailContent = c.addScrollIndicators(detailContent)
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(detailBorderColor).
		Render(detailContent)
}

func (c *ConversationPanel) addScrollIndicators(content string) string {
	isScrollable := c.detail.TotalLineCount() > c.detail.Height
	if !isScrollable {
		return content
	}

	scrollPercent := c.detail.ScrollPercent()
	viewLines := strings.Split(content, "\n")
	if len(viewLines) == 0 {
		return content
	}

	viewportWidth := c.calculateViewportWidth()
	indicatorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorLightGray)).
		Italic(true)

	// Add top indicator if not at top
	if scrollPercent > scrollPercentThreshold {
		topIndicator := indicatorStyle.Render(fmt.Sprintf("[↑ %.0f%%]", scrollPercent*scrollPercentMultiplier))
		viewLines[0] = lipgloss.NewStyle().
			Width(viewportWidth).
			Align(lipgloss.Right).
			Render(topIndicator)
	}

	// Add bottom indicator if not at bottom
	if scrollPercent < 0.99 && len(viewLines) > 1 {
		bottomIndicator := indicatorStyle.Render(fmt.Sprintf("[↓ %.0f%%]", scrollPercent*scrollPercentMultiplier))
		viewLines[len(viewLines)-1] = lipgloss.NewStyle().
			Width(viewportWidth).
			Align(lipgloss.Right).
			Render(bottomIndicator)
	}

	return strings.Join(viewLines, "\n")
}

func (c *ConversationPanel) calculateViewportWidth() int {
	viewportWidth := c.width - conversationListWidth - conversationPanelGap - conversationDetailWidthPad
	if viewportWidth < conversationDetailMinWidth {
		viewportWidth = conversationDetailMinWidth
	}
	return viewportWidth
}

func (c *ConversationPanel) renderMarkdown(content string) string {
	if content == "" {
		return ""
	}

	// Calculate available width
	contentWidth := c.width - conversationListWidth - conversationPanelGap -
		conversationDetailWidthPad - markdownExtraWidthPadding
	if contentWidth < conversationDetailMinWidth {
		contentWidth = conversationDetailMinWidth
	}

	// Check cache
	if cached, ok := c.renderedCache[c.selectedTurnIdx]; ok && c.lastContentWidth == contentWidth {
		return cached
	}

	// Create or recreate renderer if width changed
	if c.renderer == nil || c.lastContentWidth != contentWidth {
		r, err := glamour.NewTermRenderer(
			glamour.WithStylePath("dark"),
			glamour.WithWordWrap(contentWidth),
		)
		if err == nil {
			c.renderer = r
			c.lastContentWidth = contentWidth
			c.renderedCache = make(map[int]string)
		}
	}

	// Try to render with glamour
	if c.renderer != nil {
		renderedContent, err := c.renderer.Render(content)
		if err == nil {
			renderedContent = strings.TrimSpace(renderedContent)
			c.renderedCache[c.selectedTurnIdx] = renderedContent
			return renderedContent
		}
	}

	// Fallback to plain content
	return content
}

func (c *ConversationPanel) appendToolCallsMarkdown(md *strings.Builder, msg *types.Message) {
	if len(msg.ToolCalls) == 0 {
		return
	}
	fmt.Fprintf(md, "## 🔧 Tool Calls (%d)\n\n", len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		fmt.Fprintf(md, "### %d. %s\n\n", i+1, tc.Name)
		if len(tc.Args) > 0 {
			md.WriteString("```json\n")
			md.WriteString(formatJSON(string(tc.Args)))
			md.WriteString("\n```\n\n")
		}
	}
}

func (c *ConversationPanel) appendToolResultMarkdown(md *strings.Builder, msg *types.Message) {
	if msg.ToolResult == nil || msg.ToolResult.Name == "" {
		return
	}

	fmt.Fprintf(md, "## ✅ Tool Result: %s\n\n", msg.ToolResult.Name)
	if textContent := msg.ToolResult.GetTextContent(); textContent != "" {
		md.WriteString("```json\n")
		md.WriteString(formatJSON(textContent))
		md.WriteString("\n```\n\n")
	}
	if msg.ToolResult.Error != "" {
		fmt.Fprintf(md, "**Error:** %s\n\n", msg.ToolResult.Error)
	}
}

func (c *ConversationPanel) appendContentMarkdown(md *strings.Builder, msg *types.Message) {
	msgContent := msg.GetContent()
	if msgContent == "" {
		return
	}

	// Skip content section for tool result messages to avoid duplication
	// (tool result content is already shown in the Tool Result section)
	if msg.ToolResult != nil {
		return
	}

	md.WriteString("## 💬 Message\n\n")
	md.WriteString(msgContent)
	md.WriteString("\n\n")
}

func (c *ConversationPanel) appendValidationsMarkdown(md *strings.Builder, msg *types.Message) {
	if len(msg.Validations) == 0 {
		return
	}
	md.WriteString("## 🔍 Validations\n\n")
	for _, v := range msg.Validations {
		if v.Passed {
			fmt.Fprintf(md, "- ✅ **PASS** - %s\n", v.ValidatorType)
		} else {
			fmt.Fprintf(md, "- ❌ **FAIL** - %s\n", v.ValidatorType)
		}
	}
	md.WriteString("\n")
}

func formatJSON(raw string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(raw), "", "  "); err == nil {
		return buf.String()
	}
	return raw
}

func (c *ConversationPanel) renderDetailHeader(res *statestore.RunResult, idx int, msg *types.Message) string {
	// Build header parts dynamically
	parts := []string{
		fmt.Sprintf("Turn %d", idx+1),
		fmt.Sprintf("Role: %s", msg.Role),
	}

	// Add persona for self-play user turns
	if msg.Role == "user" && res.PersonaID != "" {
		parts = append(parts, fmt.Sprintf("🤖 %s", res.PersonaID))
	}

	// Add per-message metrics if available
	if msg.CostInfo != nil {
		totalTokens := msg.CostInfo.InputTokens + msg.CostInfo.OutputTokens
		parts = append(parts,
			fmt.Sprintf("Tokens: %d→%d (%d total)", msg.CostInfo.InputTokens, msg.CostInfo.OutputTokens, totalTokens),
			fmt.Sprintf("Cost: $%.4f", msg.CostInfo.TotalCost))
	}

	// Add latency if available
	if msg.LatencyMs > 0 {
		latency := time.Duration(msg.LatencyMs) * time.Millisecond
		parts = append(parts, fmt.Sprintf("Duration: %s", theme.FormatDuration(latency)))
	}

	header := strings.Join(parts, " • ")

	return lipgloss.NewStyle().
		Foreground(theme.BorderColorFocused()).
		Bold(true).
		Render(header)
}
