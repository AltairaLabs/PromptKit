package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

// chatEvalMsg carries eval results from a post-turn scoring run.
type chatEvalMsg struct {
	results []evals.EvalResult
	err     error
}

// inputHeight is the number of terminal lines reserved for the text input.
const inputHeight = 3

// inputPadding is the horizontal padding subtracted from the terminal width
// when sizing the text input widget.
const inputPadding = 4

// keyNameEnter is the key string for the Enter key in string-based key comparisons.
const keyNameEnter = "enter"

// Key binding label constants used by footer helpers.
const (
	keyLabelEsc    = "esc"
	keyLabelSelect = "select"
	keyLabelScroll = "↑/↓"
	keyLabelQuit   = "quit"
)

type chatSetupState int

const (
	stateSelectAgent    chatSetupState = iota
	stateSelectProvider                // populated when multiple providers are configured
	stateEnterVars
	stateEvalToggle
	stateChat
)

// chatErrMsg carries a non-fatal error to display.
type chatErrMsg struct{ err error }

// chatStreamDoneMsg signals that the assistant stream has ended.
type chatStreamDoneMsg struct{}

// chatModel is a tea.Model that drives the interactive chat console. It owns
// the setup flow (agent / provider / variable selection) and the live chat
// using panels.ConversationPanel driven from the state store after each turn.
type chatModel struct {
	engine   *engine.Engine
	session  *engine.InteractiveSession
	panel    *panels.ConversationPanel
	input    textinput.Model
	state    chatSetupState
	agents   []engine.AgentInfo
	taskType string
	provider string
	vars     map[string]string
	required []string
	varIdx   int
	runEvals bool
	busy     bool
	width    int
	height   int
	err      error

	statusLine string
}

// newChatModel constructs an idle chatModel bound to the given engine.
func newChatModel(eng *engine.Engine) *chatModel {
	ti := textinput.New()
	ti.Prompt = "> "
	return &chatModel{
		engine: eng,
		panel:  panels.NewConversationPanel(),
		input:  ti,
		vars:   map[string]string{},
	}
}

// initPanel hydrates the ConversationPanel with the current session so it can
// start rendering live messages.
func (m *chatModel) initPanel() {
	if m.session == nil {
		return
	}
	res := &statestore.RunResult{RunID: m.session.ConversationID()}
	m.panel.SetData(m.session.ConversationID(), "", m.provider, res)
	panelHeight := m.height - inputHeight - footerHeight - 1 // 1 for status line
	if panelHeight < 1 {
		panelHeight = 1
	}
	m.panel.SetDimensions(m.width, panelHeight)
}

// footerHeight is the number of lines the footer occupies.
const footerHeight = 1

// Init resolves the first setup step, auto-selecting when there is only one
// agent or provider so simple configs drop straight into the variable prompt.
func (m *chatModel) Init() tea.Cmd {
	m.agents = m.engine.Agents()
	switch {
	case len(m.agents) == 0:
		m.err = fmt.Errorf("config declares no agents (prompt_configs)")
		return nil
	case len(m.agents) == 1:
		m.taskType = m.agents[0].TaskType
		return m.afterAgentSelected()
	default:
		m.state = stateSelectAgent
		return nil
	}
}

func (m *chatModel) afterAgentSelected() tea.Cmd {
	providerIDs := m.engine.ProviderIDs()
	switch {
	case len(providerIDs) == 0:
		m.err = fmt.Errorf("config declares no providers")
		return nil
	case len(providerIDs) == 1:
		m.provider = providerIDs[0]
		return m.afterProviderSelected()
	default:
		m.state = stateSelectProvider
		return nil
	}
}

func (m *chatModel) afterProviderSelected() tea.Cmd {
	missing, err := m.engine.MissingRequiredVars(m.taskType, m.vars)
	if err != nil {
		m.err = err
		return nil
	}
	m.required = missing
	if len(missing) > 0 {
		m.state = stateEnterVars
		m.varIdx = 0
		m.input.Placeholder = missing[0]
		m.input.Focus()
		return textinput.Blink
	}
	return m.afterVarsEntered()
}

func (m *chatModel) afterVarsEntered() tea.Cmd {
	if m.engine.HasConfigEvals() {
		m.state = stateEvalToggle
		return nil
	}
	return m.startSession(false)
}

// startSession creates the InteractiveSession and switches to stateChat.
func (m *chatModel) startSession(runEvals bool) tea.Cmd {
	sess, err := m.engine.NewInteractiveSession(engine.InteractiveSessionOptions{
		ProviderID: m.provider,
		TaskType:   m.taskType,
		Variables:  m.vars,
		RunEvals:   runEvals,
	})
	if err != nil {
		m.err = err
		return nil
	}
	m.session = sess
	m.runEvals = runEvals
	m.state = stateChat
	m.initPanel()
	m.input.Focus()
	return textinput.Blink
}

// Update routes messages by type and current state.
func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		if m.state == stateChat {
			panelHeight := m.height - inputHeight - footerHeight - 1
			if panelHeight < 1 {
				panelHeight = 1
			}
			m.panel.SetDimensions(m.width, panelHeight)
		}
		m.input.Width = m.width - inputPadding
		return m, nil

	case tea.KeyMsg:
		// Global exit bindings take priority over all state-specific handling.
		if v.Type == tea.KeyCtrlC || v.Type == tea.KeyEsc {
			return m, tea.Quit
		}
		cmd := m.handleKey(v)
		return m, cmd

	case chatStreamDoneMsg:
		cmd := m.handleStreamDone()
		return m, cmd

	case chatEvalMsg:
		if v.err != nil {
			m.statusLine = "evals: error: " + v.err.Error()
		} else {
			m.statusLine = formatEvalScores(v.results)
		}
		return m, nil

	case chatErrMsg:
		m.err = v.err
		m.busy = false
		m.input.Focus()
		return m, nil
	}

	if m.state == stateChat {
		return m, m.panel.Update(msg)
	}
	return m, nil
}

func (m *chatModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch m.state {
	case stateSelectAgent:
		return m.handleAgentKey(msg)
	case stateSelectProvider:
		return m.handleProviderKey(msg)
	case stateEnterVars:
		return m.handleVarKey(msg)
	case stateEvalToggle:
		return m.handleEvalToggleKey(msg)
	case stateChat:
		return m.handleChatKey(msg)
	}
	return nil
}

func (m *chatModel) handleAgentKey(msg tea.KeyMsg) tea.Cmd {
	if idx, ok := digitIndex(msg.String(), len(m.agents)); ok {
		m.taskType = m.agents[idx].TaskType
		return m.afterAgentSelected()
	}
	return nil
}

func (m *chatModel) handleProviderKey(msg tea.KeyMsg) tea.Cmd {
	ids := m.engine.ProviderIDs()
	if idx, ok := digitIndex(msg.String(), len(ids)); ok {
		m.provider = ids[idx]
		return m.afterProviderSelected()
	}
	return nil
}

func (m *chatModel) handleVarKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyEnter {
		m.vars[m.required[m.varIdx]] = m.input.Value()
		m.input.Reset()
		m.varIdx++
		if m.varIdx >= len(m.required) {
			return m.afterVarsEntered()
		}
		m.input.Placeholder = m.required[m.varIdx]
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m *chatModel) handleEvalToggleKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "y":
		return m.startSession(true)
	case "n", keyNameEnter:
		return m.startSession(false)
	}
	return nil
}

func (m *chatModel) handleChatKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyEnter && strings.TrimSpace(m.input.Value()) != "" && !m.busy {
		text := m.input.Value()
		m.input.Reset()
		m.busy = true
		m.statusLine = "assistant is responding…"
		m.input.Blur()
		return m.sendCmd(text)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

// sendCmd drains the stream channel; rendering happens via state store after the turn ends.
func (m *chatModel) sendCmd(text string) tea.Cmd {
	sess := m.session
	return func() tea.Msg {
		ch, err := sess.SendUserMessage(context.Background(), text)
		if err != nil {
			return chatErrMsg{err: err}
		}
		for range ch {
		}
		return chatStreamDoneMsg{}
	}
}

// handleStreamDone is called when the assistant stream finishes. It fetches the
// full transcript from the state store and replaces the panel content entirely,
// preventing any duplication from earlier event-driven appends.
func (m *chatModel) handleStreamDone() tea.Cmd {
	m.busy = false
	m.input.Focus()

	// Refresh panel from the state store — single source of truth.
	if m.session != nil {
		msgs, err := m.session.Messages(context.Background())
		if err == nil {
			res := &statestore.RunResult{
				RunID:    m.session.ConversationID(),
				Messages: msgs,
			}
			m.panel.SetData(m.session.ConversationID(), "", m.provider, res)
		}
	}

	// Clear the "responding" status unless the eval run will overwrite it.
	if !m.runEvals {
		m.statusLine = ""
	}

	if m.runEvals {
		sess := m.session
		return func() tea.Msg {
			results, err := sess.RunEvals(context.Background())
			return chatEvalMsg{results: results, err: err}
		}
	}
	return nil
}

// View renders the current state.
func (m *chatModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("error: %v\n\n(press ctrl+c to quit)", m.err)
	}
	switch m.state {
	case stateSelectAgent:
		return m.renderPickerWithFooter("Select an agent:", agentLabels(m.agents), setupBindings())
	case stateSelectProvider:
		return m.renderPickerWithFooter("Select a provider:", m.engine.ProviderIDs(), setupBindings())
	case stateEnterVars:
		footer := views.NewHeaderFooterView(m.width).RenderFooter(setupBindings())
		return fmt.Sprintf("Enter value for required variable %q:\n\n%s\n%s",
			m.required[m.varIdx], m.input.View(), footer)
	case stateEvalToggle:
		footer := views.NewHeaderFooterView(m.width).RenderFooter(setupBindings())
		return "Run evals each turn for live scores? [y/N]\n" + footer
	case stateChat:
		return m.chatView()
	}
	return ""
}

// setupBindings returns key hints for the setup flow states.
func setupBindings() []views.KeyBinding {
	return []views.KeyBinding{
		{Keys: "1-9", Description: keyLabelSelect},
		{Keys: keyNameEnter, Description: "confirm"},
		{Keys: keyLabelEsc, Description: keyLabelQuit},
	}
}

// chatBindings returns key hints for the active chat state.
func chatBindings() []views.KeyBinding {
	return []views.KeyBinding{
		{Keys: keyNameEnter, Description: "send"},
		{Keys: keyLabelScroll, Description: "scroll"},
		{Keys: keyLabelEsc + "/ctrl+c", Description: keyLabelQuit},
	}
}

func (m *chatModel) chatView() string {
	footer := views.NewHeaderFooterView(m.width).RenderFooter(chatBindings())
	parts := []string{m.panel.View(), m.input.View()}
	if m.statusLine != "" {
		parts = append(parts, m.statusLine)
	}
	parts = append(parts, footer)
	return strings.Join(parts, "\n")
}

func (m *chatModel) renderPickerWithFooter(title string, items []string, bindings []views.KeyBinding) string {
	var b strings.Builder
	b.WriteString(title + "\n\n")
	for i, it := range items {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, it)
	}
	footer := views.NewHeaderFooterView(m.width).RenderFooter(bindings)
	b.WriteString("\n" + footer)
	return b.String()
}

// agentLabels returns display strings for the agent picker.
func agentLabels(agents []engine.AgentInfo) []string {
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

// formatEvalScores formats a slice of EvalResults as a short status line.
// Returns an empty string when there are no scoreable results.
func formatEvalScores(results []evals.EvalResult) string {
	var parts []string
	for i := range results {
		if results[i].Score == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%.2f", results[i].Type, *results[i].Score))
	}
	if len(parts) == 0 {
		return ""
	}
	return "evals: " + strings.Join(parts, " ")
}

// runChat is the cobra RunE handler for the `chat` command.
func runChat(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	useMock, _ := cmd.Flags().GetBool("mock-provider")
	mockConfig, _ := cmd.Flags().GetString("mock-config")

	eng, err := engine.NewEngineFromConfigFile(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	defer func() { _ = eng.Close() }()

	if useMock {
		if err := eng.EnableMockProviderMode(mockConfig); err != nil {
			return fmt.Errorf("enable mock provider: %w", err)
		}
	}

	m := newChatModel(eng)
	program := tea.NewProgram(m, tea.WithAltScreen())

	_, runErr := program.Run()
	return runErr
}
