package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

// chatSetupState tracks which step of the interactive setup flow ChatPage is in.
type chatSetupState int

const (
	chatStateSelectAgent    chatSetupState = iota
	chatStateSelectProvider                // populated when multiple providers are configured
	chatStateEnterVars
	chatStateEvalToggle
	chatStateChat
)

// chatStreamDoneMsg signals that the assistant stream has ended.
type chatStreamDoneMsg struct{}

// chatEvalMsg carries eval results from a post-turn scoring run.
type chatEvalMsg struct {
	results []evals.EvalResult
	err     error
}

// chatErrMsg carries a non-fatal error to display.
type chatErrMsg struct{ err error }

// voiceEndedMsg signals the voice driver goroutine exited — the voice session is
// over (idle timeout, pipeline error, or mic close). The UI reflects this so the
// console no longer looks hung with a dead mic meter. A nil err is a clean end.
type voiceEndedMsg struct{ err error }

// Voice-mode status-line labels shown during a turn.
const (
	voiceStatusListening = "🎧 listening…"
	voiceStatusThinking  = "💭 thinking…"
)

// Key label constants used by footer helpers.
const (
	chatKeyNameEnter = "enter"
	chatKeyLabelEsc  = "esc"
	chatKeyLabelSel  = "select"
	chatKeyLabelScrl = "↑/↓"
	chatKeyLabelArrs = "←/→"
	chatKeyLabelQuit = "quit"
	chatKeyLabelTab  = "tab"
	chatKeyLabelLogs = "logs"
	chatKeyCtrlL     = "ctrl+l"
)

// Layout and sizing constants for the chat view.
const (
	// chatInputHeight is the number of terminal lines reserved for the text input.
	chatInputHeight = 3
	// chatInputPadding is the horizontal padding subtracted from terminal width
	// when sizing the text input widget.
	chatInputPadding = 4
	// chatFooterHeight is the number of lines the footer occupies.
	chatFooterHeight = 1
	// chatInputBorderChars is the horizontal space a rounded border adds (one column
	// each side), subtracted so the bordered input box spans the terminal width.
	chatInputBorderChars = 2
	// chatMinErrorWidth is the floor for the fatal-error view width on tiny terminals.
	chatMinErrorWidth = 20
	// chatMaxErrorLineLen bounds a sanitized error line so a provider's full HTTP body
	// cannot flood the status line.
	chatMaxErrorLineLen = 200
)

// chatMaxInt returns the larger of two ints.
func chatMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ChatPage is a hub Page that drives the interactive chat console. It owns
// the setup flow (agent / provider / variable selection) and the live chat
// panel driven from the state store after each turn.
//
// Implements Page and Activatable.
type ChatPage struct {
	ctx     *AppContext
	engine  *engine.Engine
	session *engine.InteractiveSession
	panel   *panels.ConversationPanel
	input   textinput.Model

	state        chatSetupState
	agents       []engine.AgentInfo
	taskType     string
	provider     string
	vars         map[string]string
	required     []string
	varIdx       int
	runEvals     bool
	busy         bool
	panelFocused bool
	width        int
	height       int
	engineErr    error
	statusLine   string

	// voice-mode fields (nil voice = text mode)
	voice       *VoiceOptions
	voiceCancel context.CancelFunc
	micLevel    float32
	agentLevel  float32
	send        func(tea.Msg)               // stored in Activate for use by voice goroutine
	voiceStore  *statestore.ArenaStateStore // state store owned by the voice driver
	voiceConvID string                      // conversation ID used by the voice driver

	logs     *LogsOverlay // toggleable in-chat runtime log view (ctrl+l)
	composer *Composer    // input affordance: text box or mic indicator
}

// NewChatPage constructs a ChatPage bound to the given AppContext.
// Activate must be called before Init to wire the engine.
func NewChatPage(ctx *AppContext) *ChatPage {
	ti := textinput.New()
	ti.Prompt = "> "
	composer := NewTextComposer()
	if ctx.Voice != nil {
		composer = NewSpeechComposer()
	}
	return &ChatPage{
		ctx:      ctx,
		panel:    panels.NewConversationPanel(),
		input:    ti,
		vars:     map[string]string{},
		voice:    ctx.Voice,
		logs:     NewLogsOverlay(),
		composer: composer,
	}
}

// Title implements Page.
func (p *ChatPage) Title() string { return "Chat" }

// Close implements Closeable. It cancels any running voice driver so the mic
// and pipeline shut down cleanly when the user quits.
func (p *ChatPage) Close() {
	if p.voiceCancel != nil {
		p.voiceCancel()
	}
}

// Activate implements Activatable. It is called by App before Init.
// It calls EnsureEngine so Init can proceed. The send handle is stored
// so startVoice can push messages into the bubbletea loop from goroutines.
func (p *ChatPage) Activate(send func(tea.Msg)) tea.Cmd {
	p.send = send
	eng, err := p.ctx.EnsureEngine()
	if err != nil {
		p.engineErr = err
		return nil
	}
	p.engine = eng
	return nil
}

// Init implements Page. It resolves the first setup step, auto-selecting when
// there is only one agent or provider so simple configs drop straight into chat.
func (p *ChatPage) Init() tea.Cmd {
	if p.engineErr != nil || p.engine == nil {
		return nil
	}
	p.agents = p.engine.Agents()
	switch {
	case len(p.agents) == 0:
		p.engineErr = fmt.Errorf("config declares no agents (prompt_configs)")
		return nil
	case len(p.agents) == 1:
		p.taskType = p.agents[0].TaskType
		return p.afterAgentSelected()
	default:
		p.state = chatStateSelectAgent
		return nil
	}
}

func (p *ChatPage) afterAgentSelected() tea.Cmd {
	providerIDs := p.engine.ProviderIDs()
	switch {
	case len(providerIDs) == 0:
		p.engineErr = fmt.Errorf("config declares no providers")
		return nil
	case len(providerIDs) == 1:
		p.provider = providerIDs[0]
		return p.afterProviderSelected()
	default:
		p.state = chatStateSelectProvider
		return nil
	}
}

func (p *ChatPage) afterProviderSelected() tea.Cmd {
	missing, err := p.engine.MissingRequiredVars(p.taskType, p.vars)
	if err != nil {
		p.engineErr = err
		return nil
	}
	p.required = missing
	if len(missing) > 0 {
		p.state = chatStateEnterVars
		p.varIdx = 0
		p.input.Placeholder = missing[0]
		p.input.Focus()
		return textinput.Blink
	}
	return p.afterVarsEntered()
}

func (p *ChatPage) afterVarsEntered() tea.Cmd {
	if p.engine.HasConfigEvals() {
		p.state = chatStateEvalToggle
		return nil
	}
	return p.startSession(false)
}

// startSession creates the InteractiveSession and switches to chatStateChat.
// In voice mode, the driver is launched instead of focusing the text input.
func (p *ChatPage) startSession(runEvals bool) tea.Cmd {
	sess, err := p.engine.NewInteractiveSession(engine.InteractiveSessionOptions{
		ProviderID: p.provider,
		TaskType:   p.taskType,
		Variables:  p.vars,
		RunEvals:   runEvals,
	})
	if err != nil {
		p.engineErr = err
		return nil
	}
	p.session = sess
	p.runEvals = runEvals
	p.state = chatStateChat
	p.initPanel()
	if p.voice != nil {
		// Voice mode: start the audio driver instead of focusing the text input.
		// Show an initial status so the console isn't blank while the first turn
		// spins up (mic, VAD calibration, first STT/LLM/TTS calls).
		p.statusLine = voiceStatusListening
		return p.startVoice(p.send)
	}
	p.input.Focus()
	return textinput.Blink
}

// initPanel hydrates the ConversationPanel with the current session so it can
// start rendering live messages.
func (p *ChatPage) initPanel() {
	if p.session == nil {
		return
	}
	res := &statestore.RunResult{RunID: p.session.ConversationID()}
	p.panel.SetData(p.session.ConversationID(), "", p.provider, res)
	// The input box holds focus at chat start, so the conversation renders dim.
	p.panel.SetActive(false)
	panelH := p.height - chatInputHeight - chatFooterHeight - 1 // 1 for status line
	if panelH < 1 {
		panelH = 1
	}
	p.panel.SetDimensions(p.width, panelH)
}

// SetSize implements Page.
func (p *ChatPage) SetSize(w, h int) {
	p.width, p.height = w, h
	p.input.Width = chatMaxInt(w-chatInputPadding, 0)
	p.logs.SetSize(w, h)
	p.composer.SetWidth(w)
	if p.state == chatStateChat {
		panelH := p.height - chatInputHeight - chatFooterHeight - 1
		if panelH < 1 {
			panelH = 1
		}
		p.panel.SetDimensions(w, panelH)
	}
}

// Update implements Page. Routes messages by type and current state.
// NOTE: Esc/Ctrl+C are handled globally by App — do not handle them here.
// NOTE: WindowSizeMsg is routed by App via SetSize — do not handle it here.
func (p *ChatPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	// Buffer runtime logs and toggle the in-chat log overlay with ctrl+l
	// (ctrl+l avoids colliding with text typed into the input). While visible,
	// the overlay consumes scroll keys.
	if lm, ok := msg.(logging.Msg); ok {
		p.logs.Append(lm)
		return p, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok && km.Type == tea.KeyCtrlL {
		p.logs.Toggle()
		return p, nil
	}
	if p.logs.Visible() {
		if km, ok := msg.(tea.KeyMsg); ok {
			return p, p.logs.Update(km)
		}
	}

	switch v := msg.(type) {
	case tea.KeyMsg:
		cmd := p.handleKey(v)
		return p, cmd

	case chatStreamDoneMsg:
		cmd := p.handleStreamDone()
		return p, cmd

	case chatEvalMsg:
		if v.err != nil {
			p.statusLine = "evals: error: " + v.err.Error()
		} else {
			p.statusLine = chatFormatEvalScores(v.results)
		}
		return p, nil

	case chatErrMsg:
		// In-chat turn errors are recoverable: surface them inline and keep the
		// session alive so the user can retry. Sanitized so a provider's HTTP body
		// can't corrupt the TUI.
		p.busy = false
		p.input.Focus()
		p.statusLine = "⚠ " + chatSanitizeErrorLine(v.err)
		return p, nil
	}

	// Voice-mode level and refresh messages.
	switch v := msg.(type) {
	case voiceLevelMsg:
		p.micLevel = v.user
		p.agentLevel = v.agent
		// Update the panel's built-in audio meter so it renders the live levels.
		p.panel.SetAudioLevels(v.user, v.agent, true)
		return p, nil
	case chatRefreshMsg:
		// A message.created event arrived from the voice pipeline; reload the
		// conversation panel from the voice state store (single source of truth).
		p.refreshVoicePanel()
		return p, nil
	case voiceEndedMsg:
		// The voice driver exited. Reflect it so the console doesn't look hung:
		// freeze the audio meter and show an ended status.
		p.panel.SetAudioLevels(0, 0, false)
		if v.err != nil {
			p.statusLine = "🛑 interactive session ended: " + chatSanitizeErrorLine(v.err)
		} else {
			p.statusLine = "🛑 interactive session ended (idle timeout or mic closed) — press q to exit"
		}
		return p, nil
	}

	if p.state == chatStateChat {
		return p, p.panel.Update(msg)
	}
	return p, nil
}

func (p *ChatPage) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch p.state {
	case chatStateSelectAgent:
		return p.handleAgentKey(msg)
	case chatStateSelectProvider:
		return p.handleProviderKey(msg)
	case chatStateEnterVars:
		return p.handleVarKey(msg)
	case chatStateEvalToggle:
		return p.handleEvalToggleKey(msg)
	case chatStateChat:
		return p.handleChatKey(msg)
	}
	return nil
}

func (p *ChatPage) handleAgentKey(msg tea.KeyMsg) tea.Cmd {
	if idx, ok := chatDigitIndex(msg.String(), len(p.agents)); ok {
		p.taskType = p.agents[idx].TaskType
		return p.afterAgentSelected()
	}
	return nil
}

func (p *ChatPage) handleProviderKey(msg tea.KeyMsg) tea.Cmd {
	ids := p.engine.ProviderIDs()
	if idx, ok := chatDigitIndex(msg.String(), len(ids)); ok {
		p.provider = ids[idx]
		return p.afterProviderSelected()
	}
	return nil
}

func (p *ChatPage) handleVarKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyEnter {
		p.vars[p.required[p.varIdx]] = p.input.Value()
		p.input.Reset()
		p.varIdx++
		if p.varIdx >= len(p.required) {
			return p.afterVarsEntered()
		}
		p.input.Placeholder = p.required[p.varIdx]
		return nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

func (p *ChatPage) handleEvalToggleKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "y":
		return p.startSession(true)
	case "n", chatKeyNameEnter:
		return p.startSession(false)
	}
	return nil
}

func (p *ChatPage) handleChatKey(msg tea.KeyMsg) tea.Cmd {
	// Tab toggles focus between the panel and the input (or just the panel in
	// voice mode — there is no text input to return to).
	if msg.Type == tea.KeyTab {
		return p.handleChatTab()
	}

	// When the conversation panel has focus, forward all keys to the panel.
	if p.panelFocused {
		return p.panel.Update(msg)
	}

	// In voice mode there is no text input: only allow scroll keys to reach
	// the panel. All other keys (including Enter) are intentionally ignored —
	// the mic is the input channel.
	if p.voice != nil {
		switch msg.Type { //nolint:exhaustive // only scroll keys forwarded in voice mode
		case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
			return p.panel.Update(msg)
		}
		return nil
	}

	// Input is focused. Send on Enter (when not empty and not busy).
	if msg.Type == tea.KeyEnter && strings.TrimSpace(p.input.Value()) != "" && !p.busy {
		text := p.input.Value()
		p.input.Reset()
		p.busy = true
		p.statusLine = "assistant is responding…"
		p.input.Blur()
		return p.sendCmd(text)
	}

	// p.input is a single-line textinput — it does not consume vertical keys.
	// Forward up/down/pgup/pgdn to the panel so users can scroll while typing.
	switch msg.Type { //nolint:exhaustive // remaining cases are handled by the input below
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
		return p.panel.Update(msg)
	}

	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

// handleChatTab toggles focus between the conversation panel and the text
// input. In voice mode there is no text input, so only the panel focus flips.
func (p *ChatPage) handleChatTab() tea.Cmd {
	p.panelFocused = !p.panelFocused
	p.panel.SetActive(p.panelFocused)
	if p.voice != nil {
		return nil
	}
	if p.panelFocused {
		p.input.Blur()
	} else {
		p.input.Focus()
	}
	return textinput.Blink
}

// sendCmd drains the stream channel; rendering happens via state store after the turn ends.
func (p *ChatPage) sendCmd(text string) tea.Cmd {
	sess := p.session
	return func() (msg tea.Msg) {
		// A provider panic must never tear down the terminal — convert it to a
		// recoverable in-chat error.
		defer func() {
			if r := recover(); r != nil {
				msg = chatErrMsg{err: fmt.Errorf("provider call panicked: %v", r)}
			}
		}()
		ch, err := sess.SendUserMessage(context.Background(), text)
		if err != nil {
			return chatErrMsg{err: err}
		}
		// Drain the stream, surfacing the first error rather than dropping it.
		for chunk := range ch {
			if chunk.Error != nil {
				return chatErrMsg{err: chunk.Error}
			}
		}
		return chatStreamDoneMsg{}
	}
}

// refreshVoicePanel reloads the conversation panel from the voice state store.
// It mirrors handleStreamDone's panel.SetData pattern but reads from voiceStore
// instead of the text session, so that voice turns appear live as they arrive.
func (p *ChatPage) refreshVoicePanel() {
	if p.voiceStore == nil || p.voiceConvID == "" {
		return
	}
	state, err := p.voiceStore.Load(context.Background(), p.voiceConvID)
	if err != nil {
		return
	}
	res := &statestore.RunResult{
		RunID:    p.voiceConvID,
		Messages: state.Messages,
	}
	p.panel.SetData(p.voiceConvID, "", p.provider, res)
	p.panel.SelectLast()

	// Reflect where we are in the turn so the STT→LLM→TTS lag isn't dead air: a
	// user transcript with no reply yet means we're waiting on the model.
	if n := len(state.Messages); n > 0 {
		switch state.Messages[n-1].Role {
		case "user":
			p.statusLine = voiceStatusThinking
		case "assistant":
			p.statusLine = voiceStatusListening
		}
	}
}

// handleStreamDone is called when the assistant stream finishes. It fetches the
// full transcript from the state store and replaces the panel content entirely,
// preventing any duplication from earlier event-driven appends.
func (p *ChatPage) handleStreamDone() tea.Cmd {
	p.busy = false
	p.panelFocused = false
	p.input.Focus()

	// Refresh panel from the state store — single source of truth.
	if p.session != nil {
		msgs, err := p.session.Messages(context.Background())
		if err == nil {
			res := &statestore.RunResult{
				RunID:    p.session.ConversationID(),
				Messages: msgs,
			}
			p.panel.SetData(p.session.ConversationID(), "", p.provider, res)
			p.panel.SelectLast()
		}
	}

	// Clear the "responding" status unless the eval run will overwrite it.
	if !p.runEvals {
		p.statusLine = ""
	}

	if p.runEvals {
		sess := p.session
		return func() tea.Msg {
			results, err := sess.RunEvals(context.Background())
			return chatEvalMsg{results: results, err: err}
		}
	}
	return nil
}

// View implements Page. Renders the current state.
func (p *ChatPage) View() string {
	if p.engineErr != nil {
		// Fatal setup error: render sanitized + width-bounded so a long provider
		// body can't overflow and corrupt the terminal.
		body := lipgloss.NewStyle().
			Width(chatMaxInt(p.width-chatInputBorderChars, chatMinErrorWidth)).
			Render("error: " + chatSanitizeErrorLine(p.engineErr))
		return body + "\n\n(press ctrl+c to quit)"
	}
	switch p.state {
	case chatStateSelectAgent:
		return p.renderPickerWithFooter("Select an agent:", chatAgentLabels(p.agents), chatSetupBindings())
	case chatStateSelectProvider:
		return p.renderPickerWithFooter("Select a provider:", p.engine.ProviderIDs(), chatSetupBindings())
	case chatStateEnterVars:
		footer := views.NewHeaderFooterView(p.width).RenderFooter(chatSetupBindings())
		return fmt.Sprintf("Enter value for required variable %q:\n\n%s\n%s",
			p.required[p.varIdx], p.input.View(), footer)
	case chatStateEvalToggle:
		footer := views.NewHeaderFooterView(p.width).RenderFooter(chatSetupBindings())
		return "Run evals each turn for live scores? [y/N]\n" + footer
	case chatStateChat:
		return p.chatView()
	}
	return ""
}

// chatSetupBindings returns key hints for the setup flow states.
func chatSetupBindings() []views.KeyBinding {
	return []views.KeyBinding{
		{Keys: "1-9", Description: chatKeyLabelSel},
		{Keys: chatKeyNameEnter, Description: "confirm"},
		{Keys: chatKeyLabelEsc, Description: chatKeyLabelQuit},
	}
}

// chatBindings returns focus-aware key hints.
func (p *ChatPage) chatBindings() []views.KeyBinding {
	// Voice mode: no text input, mic is the input channel.
	if p.voice != nil {
		return []views.KeyBinding{
			{Keys: "🎤 listening", Description: ""},
			{Keys: chatKeyLabelTab, Description: "focus convo"},
			{Keys: chatKeyCtrlL, Description: chatKeyLabelLogs},
			{Keys: chatKeyLabelEsc + "/ctrl+c", Description: chatKeyLabelQuit},
		}
	}
	if p.panelFocused {
		return []views.KeyBinding{
			{Keys: chatKeyLabelScrl, Description: "turns"},
			{Keys: chatKeyLabelArrs, Description: "turns/detail"},
			{Keys: chatKeyLabelTab, Description: "back to input"},
			{Keys: chatKeyCtrlL, Description: chatKeyLabelLogs},
			{Keys: chatKeyLabelEsc + "/ctrl+c", Description: chatKeyLabelQuit},
		}
	}
	return []views.KeyBinding{
		{Keys: chatKeyNameEnter, Description: "send"},
		{Keys: chatKeyLabelScrl, Description: "scroll"},
		{Keys: chatKeyLabelTab, Description: "focus conversation"},
		{Keys: chatKeyCtrlL, Description: chatKeyLabelLogs},
		{Keys: chatKeyLabelEsc + "/ctrl+c", Description: chatKeyLabelQuit},
	}
}

func (p *ChatPage) chatView() string {
	return views.RenderWithChrome(
		views.ChromeConfig{
			Width:       p.width,
			Height:      p.height,
			Title:       p.chatTitle(),
			KeyBindings: p.chatBindings(),
		},
		func(contentHeight int) string {
			// When the logs overlay is toggled on it replaces the conversation
			// body so runtime logs are inspectable without leaving the chat.
			if p.logs.Visible() {
				p.logs.SetSize(p.width, contentHeight)
				return p.logs.View()
			}

			// Size the conversation panel to fill the body minus the composer
			// (and optional status line).
			composerH := chatInputHeight
			if p.voice != nil {
				composerH = 1
			}
			statusH := 0
			if p.statusLine != "" {
				statusH = 1
			}
			panelH := contentHeight - composerH - statusH
			if panelH < 1 {
				panelH = 1
			}
			p.panel.SetDimensions(p.width, panelH)

			var parts []string
			parts = append(parts, p.panel.View())
			// The Composer renders the mic indicator (voice) or the bordered
			// text box (typed). In voice mode the panel renders the live audio
			// meter via SetAudioLevels. Input focus = panel not focused.
			p.composer.SetSpeech(p.voice != nil)
			parts = append(parts, p.composer.View(p.input.View(), !p.panelFocused))
			if p.statusLine != "" {
				parts = append(parts, p.statusLine)
			}
			return strings.Join(parts, "\n")
		},
	)
}

// chatTitle is the page-context line shown in the chrome header.
func (p *ChatPage) chatTitle() string {
	label := "Chat"
	if p.voice != nil {
		label = "Interactive session"
	}
	if p.taskType != "" && p.provider != "" {
		return fmt.Sprintf("%s · %s / %s", label, p.taskType, p.provider)
	}
	return label
}

func (p *ChatPage) renderPickerWithFooter(title string, items []string, bindings []views.KeyBinding) string {
	var b strings.Builder
	b.WriteString(title + "\n\n")
	for i, it := range items {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, it)
	}
	footer := views.NewHeaderFooterView(p.width).RenderFooter(bindings)
	b.WriteString("\n" + footer)
	return b.String()
}

// chatAgentLabels returns display strings for the agent picker.
func chatAgentLabels(agents []engine.AgentInfo) []string {
	out := make([]string, len(agents))
	for i := range agents {
		out[i] = agents[i].TaskType
		if agents[i].Description != "" {
			out[i] += " — " + agents[i].Description
		}
	}
	return out
}

// chatDigitIndex parses "1".."9" into a zero-based index within [0,n).
func chatDigitIndex(s string, n int) (int, bool) {
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return 0, false
	}
	idx := int(s[0] - '1')
	if idx >= n {
		return 0, false
	}
	return idx, true
}

// chatFormatEvalScores formats a slice of EvalResults as a short status line.
func chatFormatEvalScores(results []evals.EvalResult) string {
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

// chatAnsiSeq matches ANSI SGR escape sequences, stripped from error text so it
// cannot corrupt the terminal.
var chatAnsiSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

// chatSanitizeErrorLine collapses an error into a single, control-character-free,
// length-bounded line safe to render in the TUI.
func chatSanitizeErrorLine(err error) string {
	if err == nil {
		return ""
	}
	s := chatAnsiSeq.ReplaceAllString(err.Error(), "")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			b.WriteRune(' ') // newlines/tabs/etc → space
			continue
		}
		b.WriteRune(r)
	}
	line := strings.Join(strings.Fields(b.String()), " ")
	if runes := []rune(line); len(runes) > chatMaxErrorLineLen {
		line = string(runes[:chatMaxErrorLineLen]) + "…"
	}
	return line
}
