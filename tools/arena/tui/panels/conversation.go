// Package panels provides reusable panel components for the TUI.
package panels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	conversationListWidth        = 40
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
	conversationArgsIndent       = 4
	conversationResultIndent     = 2
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

	// Cache for glamour rendering
	renderer         *glamour.TermRenderer
	renderedCache    map[int]string // map[turnIndex]renderedContent
	lastContentWidth int
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

// AppendMessage adds a new message to the conversation in real-time
func (c *ConversationPanel) AppendMessage(msg *types.Message) {
	if c.res == nil {
		return
	}
	c.res.Messages = append(c.res.Messages, *msg)
	// Invalidate render cache since we added a message
	c.renderedCache = make(map[int]string)
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

	title := c.buildTitle()
	tableView := c.buildTableView()
	detailView := c.buildDetailView()

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		tableView,
		strings.Repeat(" ", conversationPanelGap),
		detailView,
	)

	return lipgloss.NewStyle().
		Padding(conversationPanelPadding, conversationPanelHorizontal).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, content))
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
	if msg.ToolResult.Content != "" {
		md.WriteString("```json\n")
		md.WriteString(formatJSON(msg.ToolResult.Content))
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
