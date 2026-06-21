package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
)

// inputHeight is the number of terminal lines reserved for the text input.
const inputHeight = 3

// inputPadding is the horizontal padding subtracted from the terminal width
// when sizing the text input widget.
const inputPadding = 4

// keyNameEnter is the key string for the Enter key in string-based key comparisons.
const keyNameEnter = "enter"

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
// using panels.ConversationPanel driven by tui.EventAdapter messages.
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
	panelHeight := m.height - inputHeight - 1 // 1 for status line
	if panelHeight < 1 {
		panelHeight = 1
	}
	m.panel.SetDimensions(m.width, panelHeight)
}

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
			panelHeight := m.height - inputHeight - 1
			if panelHeight < 1 {
				panelHeight = 1
			}
			m.panel.SetDimensions(m.width, panelHeight)
		}
		m.input.Width = m.width - inputPadding
		return m, nil

	case tea.KeyMsg:
		if v.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		cmd := m.handleKey(v)
		return m, cmd

	case tui.MessageCreatedMsg:
		m.handleMessageCreated(&v)
		return m, nil

	case tui.MessageUpdatedMsg:
		m.handleMessageUpdated(&v)
		return m, nil

	case tui.ConversationStartedMsg:
		m.handleConversationStarted(&v)
		return m, nil

	case chatStreamDoneMsg:
		m.handleStreamDone()
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
		m.input.Blur()
		return m.sendCmd(text)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

// handleMessageCreated appends the message to the panel when it belongs to this session.
func (m *chatModel) handleMessageCreated(v *tui.MessageCreatedMsg) {
	if m.session == nil || v.ConversationID != m.session.ConversationID() {
		return
	}
	msg := messageFromCreatedMsg(v)
	m.panel.AppendMessage(&msg)
}

// handleMessageUpdated updates message metadata in the panel when it belongs to this session.
func (m *chatModel) handleMessageUpdated(v *tui.MessageUpdatedMsg) {
	if m.session == nil || v.ConversationID != m.session.ConversationID() {
		return
	}
	cost := types.CostInfo{
		InputTokens:  v.InputTokens,
		OutputTokens: v.OutputTokens,
		TotalCost:    v.TotalCost,
	}
	m.panel.UpdateMessageMetadata(v.Index, v.LatencyMs, cost)
}

// handleConversationStarted prepends the system prompt when it belongs to this session.
func (m *chatModel) handleConversationStarted(v *tui.ConversationStartedMsg) {
	if m.session == nil || v.ConversationID != m.session.ConversationID() {
		return
	}
	systemMsg := &types.Message{Role: "system", Content: v.SystemPrompt}
	m.panel.PrependSystemPrompt(systemMsg)
}

// sendCmd drains the stream channel; rendering happens via event bus → EventAdapter.
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

func (m *chatModel) handleStreamDone() {
	m.busy = false
	m.input.Focus()
}

// View renders the current state.
func (m *chatModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("error: %v\n\n(press ctrl+c to quit)", m.err)
	}
	switch m.state {
	case stateSelectAgent:
		return m.renderPicker("Select an agent:", agentLabels(m.agents))
	case stateSelectProvider:
		return m.renderPicker("Select a provider:", m.engine.ProviderIDs())
	case stateEnterVars:
		return fmt.Sprintf("Enter value for required variable %q:\n\n%s",
			m.required[m.varIdx], m.input.View())
	case stateEvalToggle:
		return "Run evals each turn for live scores? [y/N]"
	case stateChat:
		return m.chatView()
	}
	return ""
}

func (m *chatModel) chatView() string {
	parts := []string{m.panel.View(), m.input.View()}
	if m.statusLine != "" {
		parts = append(parts, m.statusLine)
	}
	return strings.Join(parts, "\n")
}

func (m *chatModel) renderPicker(title string, items []string) string {
	var b strings.Builder
	b.WriteString(title + "\n\n")
	for i, it := range items {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, it)
	}
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

// messageFromCreatedMsg builds a types.Message from a tui.MessageCreatedMsg.
func messageFromCreatedMsg(msg *tui.MessageCreatedMsg) types.Message {
	m := types.Message{
		Role:    msg.Role,
		Content: msg.Content,
	}
	if len(msg.ToolCalls) > 0 {
		tcs := make([]types.MessageToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			// events.MessageToolCall.Args is a string; types.MessageToolCall.Args
			// is json.RawMessage — cast directly since a JSON string is valid RawMessage.
			tcs[i] = types.MessageToolCall{
				ID:   tc.ID,
				Name: tc.Name,
				Args: json.RawMessage(tc.Args),
			}
		}
		m.ToolCalls = tcs
	}
	if msg.ToolResult != nil {
		tr := types.NewTextToolResult(msg.ToolResult.ID, msg.ToolResult.Name, "")
		// Carry over the parts from the MessageToolResult.
		if len(msg.ToolResult.Parts) > 0 {
			tr.Parts = msg.ToolResult.Parts
		}
		m.ToolResult = &tr
	}
	return m
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

	bus := events.NewEventBus()
	eng.SetEventBus(bus, engine.WithMessageEvents())
	defer bus.Close()

	m := newChatModel(eng)
	program := tea.NewProgram(m, tea.WithAltScreen())
	adapter := tui.NewEventAdapter(program)
	adapter.Subscribe(bus)

	_, runErr := program.Run()
	return runErr
}
