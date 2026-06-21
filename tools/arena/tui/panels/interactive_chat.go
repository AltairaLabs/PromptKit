package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// roleAssistant is the canonical role string for assistant messages.
const roleAssistant = "assistant"

// InteractiveChatPanel renders a live conversation transcript above a text
// input. Messages are rendered the same way as a scenario run: user/assistant
// text, tool calls, tool results, and guardrail validations.
type InteractiveChatPanel struct {
	width    int
	height   int
	viewport viewport.Model
	textarea textarea.Model
	messages []types.Message
	cost     types.CostInfo
	evals    []evals.EvalResult
	busy     bool
}

// NewInteractiveChatPanel constructs a panel with a focused input.
func NewInteractiveChatPanel() *InteractiveChatPanel {
	ta := textarea.New()
	ta.Placeholder = "Type a message and press Enter (Shift+Enter for newline)..."
	ta.Focus()
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	vp := viewport.New(0, 0)
	return &InteractiveChatPanel{viewport: vp, textarea: ta}
}

// SetDimensions resizes the panel; the input takes a fixed height, the
// transcript takes the rest.
func (p *InteractiveChatPanel) SetDimensions(width, height int) {
	p.width = width
	p.height = height
	const inputHeight = 3
	const footerHeight = 1
	p.textarea.SetWidth(width)
	p.textarea.SetHeight(inputHeight)
	vpHeight := height - inputHeight - footerHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	p.viewport.Width = width
	p.viewport.Height = vpHeight
	p.refresh()
}

// SetMessages replaces the transcript content.
func (p *InteractiveChatPanel) SetMessages(msgs []types.Message) {
	p.messages = msgs
	p.refresh()
}

// SetCost updates the running cost footer.
func (p *InteractiveChatPanel) SetCost(c *types.CostInfo) {
	if c != nil {
		p.cost = *c
	}
	p.refresh()
}

// SetEvals updates the eval-scores footer line.
func (p *InteractiveChatPanel) SetEvals(results []evals.EvalResult) { p.evals = results; p.refresh() }

// InputValue returns the current draft text.
func (p *InteractiveChatPanel) InputValue() string { return p.textarea.Value() }

// ClearInput empties the input box.
func (p *InteractiveChatPanel) ClearInput() { p.textarea.Reset() }

// SetBusy toggles input editability while a turn is streaming.
func (p *InteractiveChatPanel) SetBusy(busy bool) {
	p.busy = busy
	if busy {
		p.textarea.Blur()
	} else {
		p.textarea.Focus()
	}
	p.refresh()
}

// Update forwards key/resize events to the input (when not busy) and viewport.
func (p *InteractiveChatPanel) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	if !p.busy {
		var cmd tea.Cmd
		p.textarea, cmd = p.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}
	var vpCmd tea.Cmd
	p.viewport, vpCmd = p.viewport.Update(msg)
	cmds = append(cmds, vpCmd)
	return tea.Batch(cmds...)
}

// View renders transcript + footer + input.
func (p *InteractiveChatPanel) View() string {
	return strings.Join([]string{
		p.viewport.View(),
		p.footer(),
		p.textarea.View(),
	}, "\n")
}

func (p *InteractiveChatPanel) refresh() {
	p.viewport.SetContent(p.renderTranscript())
	p.viewport.GotoBottom()
}

func (p *InteractiveChatPanel) renderTranscript() string {
	var b strings.Builder
	for i := range p.messages {
		b.WriteString(renderChatMessage(&p.messages[i]))
		b.WriteString("\n")
	}
	if p.busy {
		b.WriteString(roleAssistant + ": ...\n")
	}
	return b.String()
}

func (p *InteractiveChatPanel) footer() string {
	parts := []string{fmt.Sprintf("cost: $%.4f", p.cost.TotalCost)}
	for i := range p.evals {
		score := 0.0
		if p.evals[i].Score != nil {
			score = *p.evals[i].Score
		}
		parts = append(parts, fmt.Sprintf("%s=%.2f", p.evals[i].Type, score))
	}
	return strings.Join(parts, "  |  ")
}

// renderChatMessage formats one message: role-prefixed text, tool calls, tool
// result, and guardrail validations — mirroring a scenario-run transcript.
func renderChatMessage(msg *types.Message) string {
	var b strings.Builder
	switch {
	case msg.ToolResult != nil:
		fmt.Fprintf(&b, "tool[%s] result: %s", msg.ToolResult.Name, msg.ToolResult.GetTextContent())
	case msg.Role == roleAssistant && len(msg.ToolCalls) > 0 && msg.GetContent() == "":
		names := make([]string, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			names = append(names, tc.Name)
		}
		fmt.Fprintf(&b, "assistant -> tool calls: %s", strings.Join(names, ", "))
	default:
		fmt.Fprintf(&b, "%s: %s", msg.Role, msg.GetContent())
		for _, tc := range msg.ToolCalls {
			fmt.Fprintf(&b, "\n  -> calls %s", tc.Name)
		}
	}
	for i := range msg.Validations {
		fmt.Fprintf(&b, "\n  ! guardrail: %s", msg.Validations[i].ValidatorType)
	}
	return b.String()
}
