package pages

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// varInputPadding is the horizontal margin subtracted from the terminal width
// when sizing the variable-entry text input.
const varInputPadding = 4

// keyEnter is the keyboard string for the "enter" (return) key used in eval
// toggle and provider / agent pickers.
const keyEnter = "enter"

type chatState int

const (
	stateSelectAgent    chatState = iota
	stateSelectProvider           // populated when multiple providers are configured
	stateEnterVars
	stateEvalToggle
	stateChat
)

// AgentOption describes one selectable agent returned by the engine. It mirrors
// engine.AgentInfo so the pages package does not import engine directly.
type AgentOption struct {
	TaskType    string
	Description string
}

// ChatSession is the interface the page calls on an interactive session.
// engine.InteractiveSession satisfies it.
type ChatSession interface {
	SendUserMessage(ctx context.Context, text string) (<-chan turnexecutors.MessageStreamChunk, error)
	Messages(ctx context.Context) ([]types.Message, error)
	Cost(ctx context.Context) (types.CostInfo, error)
	RunEvals(ctx context.Context) ([]evals.EvalResult, error)
}

// ChatSessionOptions mirrors engine.InteractiveSessionOptions.
type ChatSessionOptions struct {
	ProviderID string
	TaskType   string
	Variables  map[string]string
	RunEvals   bool
}

// ChatEngine is the interface the page calls on the engine.
// *engine.Engine satisfies it once wrapped by an adapter.
type ChatEngine interface {
	Agents() []AgentOption
	ProviderIDs() []string
	MissingRequiredVars(taskType string, provided map[string]string) ([]string, error)
	HasConfigEvals() bool
	NewChatSession(opts ChatSessionOptions) (ChatSession, error)
}

// chatChunkMsg carries one streaming chunk plus the channel to continue reading from.
type chatChunkMsg struct {
	chunk turnexecutors.MessageStreamChunk
	ch    <-chan turnexecutors.MessageStreamChunk
}

// chatStreamDoneMsg signals that the assistant stream has ended.
type chatStreamDoneMsg struct{}

// chatErrMsg carries a non-fatal error to display in the page.
type chatErrMsg struct{ err error }

// chatEvalMsg carries eval results after a completed turn.
type chatEvalMsg struct{ results []evals.EvalResult }

// InteractiveChatPage owns the setup flow and the live chat.
type InteractiveChatPage struct {
	eng    ChatEngine
	width  int
	height int

	state    chatState
	agents   []AgentOption
	provider string
	taskType string
	vars     map[string]string
	required []string
	varIdx   int
	runEvals bool

	varInput textinput.Model
	panel    *panels.InteractiveChatPanel
	session  ChatSession
	err      error
}

// NewInteractiveChatPage builds the page for the given engine adapter.
func NewInteractiveChatPage(eng ChatEngine) *InteractiveChatPage {
	ti := textinput.New()
	ti.Prompt = "> "
	return &InteractiveChatPage{
		eng:      eng,
		vars:     map[string]string{},
		varInput: ti,
		panel:    panels.NewInteractiveChatPanel(),
	}
}

// Init resolves the first setup step. A single agent + single provider are
// auto-selected so simple configs land directly on the variable prompt or chat.
func (pg *InteractiveChatPage) Init() tea.Cmd {
	pg.agents = pg.eng.Agents()
	switch {
	case len(pg.agents) == 0:
		pg.err = fmt.Errorf("config declares no agents (prompt_configs)")
		return nil
	case len(pg.agents) == 1:
		pg.taskType = pg.agents[0].TaskType
		return pg.afterAgentSelected()
	default:
		pg.state = stateSelectAgent
		return nil
	}
}

func (pg *InteractiveChatPage) afterAgentSelected() tea.Cmd {
	providerIDs := pg.eng.ProviderIDs()
	switch {
	case len(providerIDs) == 0:
		pg.err = fmt.Errorf("config declares no providers")
		return nil
	case len(providerIDs) == 1:
		pg.provider = providerIDs[0]
		return pg.afterProviderSelected()
	default:
		pg.state = stateSelectProvider
		return nil
	}
}

func (pg *InteractiveChatPage) afterProviderSelected() tea.Cmd {
	missing, err := pg.eng.MissingRequiredVars(pg.taskType, pg.vars)
	if err != nil {
		pg.err = err
		return nil
	}
	pg.required = missing
	if len(missing) > 0 {
		pg.state = stateEnterVars
		pg.varIdx = 0
		pg.varInput.Placeholder = missing[0]
		pg.varInput.Focus()
		return textinput.Blink
	}
	return pg.afterVarsEntered()
}

func (pg *InteractiveChatPage) afterVarsEntered() tea.Cmd {
	if pg.eng.HasConfigEvals() {
		pg.state = stateEvalToggle
		return nil
	}
	return pg.startSession(pg.vars, false)
}

// startSession creates the ChatSession and switches to chat state.
func (pg *InteractiveChatPage) startSession(vars map[string]string, runEvals bool) tea.Cmd {
	sess, err := pg.eng.NewChatSession(ChatSessionOptions{
		ProviderID: pg.provider,
		TaskType:   pg.taskType,
		Variables:  vars,
		RunEvals:   runEvals,
	})
	if err != nil {
		pg.err = err
		return nil
	}
	pg.session = sess
	pg.runEvals = runEvals
	pg.state = stateChat
	return nil
}

// SetDimensions resizes child widgets.
func (pg *InteractiveChatPage) SetDimensions(width, height int) {
	pg.width, pg.height = width, height
	pg.panel.SetDimensions(width, height)
	pg.varInput.Width = width - varInputPadding
}

// Update routes input by state.
func (pg *InteractiveChatPage) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case chatChunkMsg:
		return pg.handleChunk(&m)
	case chatStreamDoneMsg:
		return pg.handleStreamDone()
	case chatEvalMsg:
		pg.panel.SetEvals(m.results)
		return nil
	case chatErrMsg:
		pg.err = m.err
		pg.panel.SetBusy(false)
		return nil
	case tea.KeyMsg:
		return pg.handleKey(m)
	}
	if pg.state == stateChat {
		return pg.panel.Update(msg)
	}
	return nil
}

func (pg *InteractiveChatPage) handleKey(m tea.KeyMsg) tea.Cmd {
	switch pg.state {
	case stateSelectAgent:
		return pg.handleAgentKey(m)
	case stateSelectProvider:
		return pg.handleProviderKey(m)
	case stateEnterVars:
		return pg.handleVarKey(m)
	case stateEvalToggle:
		return pg.handleEvalToggleKey(m)
	case stateChat:
		return pg.handleChatKey(m)
	}
	return nil
}

func (pg *InteractiveChatPage) handleAgentKey(m tea.KeyMsg) tea.Cmd {
	if idx, ok := digitIndex(m.String(), len(pg.agents)); ok {
		pg.taskType = pg.agents[idx].TaskType
		return pg.afterAgentSelected()
	}
	return nil
}

func (pg *InteractiveChatPage) handleProviderKey(m tea.KeyMsg) tea.Cmd {
	providerIDs := pg.eng.ProviderIDs()
	if idx, ok := digitIndex(m.String(), len(providerIDs)); ok {
		pg.provider = providerIDs[idx]
		return pg.afterProviderSelected()
	}
	return nil
}

func (pg *InteractiveChatPage) handleVarKey(m tea.KeyMsg) tea.Cmd {
	if m.Type == tea.KeyEnter {
		pg.vars[pg.required[pg.varIdx]] = pg.varInput.Value()
		pg.varInput.Reset()
		pg.varIdx++
		if pg.varIdx >= len(pg.required) {
			return pg.afterVarsEntered()
		}
		pg.varInput.Placeholder = pg.required[pg.varIdx]
		return nil
	}
	var cmd tea.Cmd
	pg.varInput, cmd = pg.varInput.Update(m)
	return cmd
}

func (pg *InteractiveChatPage) handleEvalToggleKey(m tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(m.String()) {
	case "y":
		return pg.startSession(pg.vars, true)
	case "n", keyEnter:
		return pg.startSession(pg.vars, false)
	}
	return nil
}

func (pg *InteractiveChatPage) handleChatKey(m tea.KeyMsg) tea.Cmd {
	if m.Type == tea.KeyEnter && strings.TrimSpace(pg.panel.InputValue()) != "" {
		text := pg.panel.InputValue()
		pg.panel.ClearInput()
		pg.panel.SetBusy(true)
		return pg.sendCmd(text)
	}
	return pg.panel.Update(m)
}

// sendCmd kicks off a turn and returns a command that begins reading the stream.
func (pg *InteractiveChatPage) sendCmd(text string) tea.Cmd {
	sess := pg.session
	return func() tea.Msg {
		ch, err := sess.SendUserMessage(context.Background(), text)
		if err != nil {
			return chatErrMsg{err: err}
		}
		chunk, ok := <-ch
		if !ok {
			return chatStreamDoneMsg{}
		}
		return chatChunkMsg{chunk: chunk, ch: ch}
	}
}

func (pg *InteractiveChatPage) handleChunk(m *chatChunkMsg) tea.Cmd {
	if m.chunk.Error != nil {
		pg.err = m.chunk.Error
		pg.panel.SetBusy(false)
		return nil
	}
	if len(m.chunk.Messages) > 0 {
		pg.panel.SetMessages(m.chunk.Messages)
	}
	if m.ch == nil {
		return nil
	}
	ch := m.ch
	return func() tea.Msg {
		chunk, ok := <-ch
		if !ok {
			return chatStreamDoneMsg{}
		}
		return chatChunkMsg{chunk: chunk, ch: ch}
	}
}

func (pg *InteractiveChatPage) handleStreamDone() tea.Cmd {
	pg.panel.SetBusy(false)
	ctx := context.Background()
	if msgs, err := pg.session.Messages(ctx); err == nil {
		pg.panel.SetMessages(msgs)
	}
	if cost, err := pg.session.Cost(ctx); err == nil {
		// SetCost takes a pointer — pass &cost.
		pg.panel.SetCost(&cost)
	}
	if !pg.runEvals {
		return nil
	}
	sess := pg.session
	return func() tea.Msg {
		results, err := sess.RunEvals(context.Background())
		if err != nil {
			return chatErrMsg{err: err}
		}
		return chatEvalMsg{results: results}
	}
}

// View renders the active state.
func (pg *InteractiveChatPage) View() string {
	if pg.err != nil {
		return fmt.Sprintf("error: %v\n\n(press esc to go back)", pg.err)
	}
	switch pg.state {
	case stateSelectAgent:
		return pg.renderPicker("Select an agent:", agentOptionLabels(pg.agents))
	case stateSelectProvider:
		return pg.renderPicker("Select a provider:", pg.eng.ProviderIDs())
	case stateEnterVars:
		return fmt.Sprintf("Enter value for required variable %q:\n\n%s",
			pg.required[pg.varIdx], pg.varInput.View())
	case stateEvalToggle:
		return "Run evals each turn for live scores? [y/N]"
	case stateChat:
		return pg.panel.View()
	}
	return ""
}

func (pg *InteractiveChatPage) renderPicker(title string, items []string) string {
	var b strings.Builder
	b.WriteString(title + "\n\n")
	for i, it := range items {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, it)
	}
	return b.String()
}

func agentOptionLabels(agents []AgentOption) []string {
	out := make([]string, len(agents))
	for i := range agents {
		out[i] = agents[i].TaskType
		if agents[i].Description != "" {
			out[i] += " — " + agents[i].Description
		}
	}
	return out
}

// digitIndex parses "1".."9" into a zero-based index within [0,n).
func digitIndex(s string, n int) (int, bool) {
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return 0, false
	}
	idx := int(s[0] - '1')
	if idx >= n {
		return 0, false
	}
	return idx, true
}

// enterChatForTest is a test seam to jump straight into chat with a live session.
// It must live in production code so that the unexported startSession method is
// reachable from _test.go files in the same package. TBHelper avoids importing
// "testing" into the production binary.
//
//nolint:unused // called exclusively from _test.go files in the same package
func (pg *InteractiveChatPage) enterChatForTest(tb TBHelper, vars map[string]string, runEvals bool) {
	tb.Helper()
	_ = pg.startSession(vars, runEvals)
	if pg.err != nil {
		type fatalf interface{ Fatalf(string, ...any) }
		if f, ok := tb.(fatalf); ok {
			f.Fatalf("startSession failed: %v", pg.err)
		}
	}
}

// TBHelper avoids importing testing into production code; tests pass *testing.T.
// It is referenced by enterChatForTest above.
type TBHelper interface{ Helper() }
