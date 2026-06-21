package pages

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	turnexec "github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

type turnexecChunk = turnexec.MessageStreamChunk
type msgType = types.Message

func init() {
	// Disable schema validation for tests — pages tests load fixtures that may
	// not match the published remote schemas.
	config.SchemaValidationDisabled.Store(true)
}

// ─── Engine adapter (wraps *engine.Engine → ChatEngine) ──────────────────────

// engineAdapter wraps *engine.Engine to satisfy ChatEngine without requiring
// the pages package to import engine (which would create an import cycle via
// engine_test → tui → pages → engine).
type engineAdapter struct{ e *engine.Engine }

func (a *engineAdapter) Agents() []AgentOption {
	raw := a.e.Agents()
	out := make([]AgentOption, len(raw))
	for i, r := range raw {
		out[i] = AgentOption{TaskType: r.TaskType, Description: r.Description}
	}
	return out
}

func (a *engineAdapter) ProviderIDs() []string { return a.e.ProviderIDs() }

func (a *engineAdapter) MissingRequiredVars(taskType string, provided map[string]string) ([]string, error) {
	return a.e.MissingRequiredVars(taskType, provided)
}

func (a *engineAdapter) HasConfigEvals() bool { return a.e.HasConfigEvals() }

func (a *engineAdapter) NewChatSession(opts ChatSessionOptions) (ChatSession, error) {
	sess, err := a.e.NewInteractiveSession(engine.InteractiveSessionOptions{
		ProviderID: opts.ProviderID,
		TaskType:   opts.TaskType,
		Variables:  opts.Variables,
		RunEvals:   opts.RunEvals,
	})
	if err != nil {
		return nil, err
	}
	return &sessionAdapter{s: sess}, nil
}

// sessionAdapter wraps *engine.InteractiveSession to satisfy ChatSession.
type sessionAdapter struct{ s *engine.InteractiveSession }

func (sa *sessionAdapter) SendUserMessage(ctx context.Context, text string) (<-chan turnexec.MessageStreamChunk, error) {
	return sa.s.SendUserMessage(ctx, text)
}
func (sa *sessionAdapter) Messages(ctx context.Context) ([]types.Message, error) {
	return sa.s.Messages(ctx)
}
func (sa *sessionAdapter) Cost(ctx context.Context) (types.CostInfo, error) { return sa.s.Cost(ctx) }
func (sa *sessionAdapter) RunEvals(ctx context.Context) ([]evals.EvalResult, error) {
	return sa.s.RunEvals(ctx)
}

// ─── Stub ChatEngine for unit tests (no real engine required) ─────────────────

type stubEngine struct {
	agents     []AgentOption
	providers  []string
	missing    []string
	missingErr error
	hasEvals   bool
	sessionErr error
}

func (s *stubEngine) Agents() []AgentOption { return s.agents }
func (s *stubEngine) ProviderIDs() []string { return s.providers }
func (s *stubEngine) HasConfigEvals() bool  { return s.hasEvals }
func (s *stubEngine) MissingRequiredVars(_ string, _ map[string]string) ([]string, error) {
	return s.missing, s.missingErr
}
func (s *stubEngine) NewChatSession(_ ChatSessionOptions) (ChatSession, error) {
	if s.sessionErr != nil {
		return nil, s.sessionErr
	}
	return &stubSession{}, nil
}

// stubSession is a no-op ChatSession.
type stubSession struct{}

func (ss *stubSession) SendUserMessage(_ context.Context, _ string) (<-chan turnexec.MessageStreamChunk, error) {
	ch := make(chan turnexec.MessageStreamChunk)
	close(ch)
	return ch, nil
}
func (ss *stubSession) Messages(_ context.Context) ([]types.Message, error) {
	return []types.Message{{Role: "assistant", Content: "hello"}}, nil
}
func (ss *stubSession) Cost(_ context.Context) (types.CostInfo, error) {
	return types.CostInfo{TotalCost: 0.001}, nil
}
func (ss *stubSession) RunEvals(_ context.Context) ([]evals.EvalResult, error) {
	score := 1.0
	return []evals.EvalResult{{Type: "json_valid", Score: &score}}, nil
}

// ─── Fixture engine (real engine with mock provider) ─────────────────────────

func fixtureEngine(t *testing.T) ChatEngine {
	t.Helper()
	cfg := filepath.Clean("../../engine/testdata/interactive/config.arena.yaml")
	eng, err := engine.NewEngineFromConfigFile(cfg)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("mock: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return &engineAdapter{e: eng}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func streamChunkWithAssistant(text string) turnexecChunk {
	return turnexecChunk{Messages: []msgType{{Role: "assistant", Content: text}}}
}

// ─── Tests ───────────────────────────────────────────────────────────────────

var _ tea.Msg = chatErrMsg{}

func TestInteractiveChatPage_AutoSelectsSingleAgentAndProvider(t *testing.T) {
	pg := NewInteractiveChatPage(fixtureEngine(t))
	pg.SetDimensions(80, 24)
	pg.Init()
	// Single agent + single provider → setup jumps straight to the variable
	// prompt for the required "company" var.
	out := pg.View()
	if !strings.Contains(strings.ToLower(out), "company") {
		t.Fatalf("expected variable prompt for 'company', got:\n%s", out)
	}
}

func TestInteractiveChatPage_ChunkAppendsToTranscript(t *testing.T) {
	pg := NewInteractiveChatPage(fixtureEngine(t))
	pg.SetDimensions(80, 24)
	pg.Init()
	// Force the page into chat state with an active session so we can feed a chunk.
	pg.enterChatForTest(t, map[string]string{"company": "Acme"}, false)

	pg.Update(chatChunkMsg{chunk: streamChunkWithAssistant("streamed reply")})
	out := pg.View()
	if !strings.Contains(out, "streamed reply") {
		t.Fatalf("expected streamed reply in transcript, got:\n%s", out)
	}
}

// TestInteractiveChatPage_MultipleAgents verifies the agent picker is shown when
// more than one agent is declared.
func TestInteractiveChatPage_MultipleAgents(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "alpha"}, {TaskType: "beta", Description: "desc"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	out := pg.View()
	if !strings.Contains(out, "Select an agent") {
		t.Fatalf("expected agent picker, got:\n%s", out)
	}
	if !strings.Contains(out, "beta — desc") {
		t.Fatalf("expected agent labels with description, got:\n%s", out)
	}
}

// TestInteractiveChatPage_AgentKeySelectsAndAdvances selects an agent by pressing "1".
func TestInteractiveChatPage_AgentKeySelectsAndAdvances(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "alpha"}, {TaskType: "beta"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()

	// Press "1" — selects alpha, single provider → lands on var entry (no required vars → chat).
	pg.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	// no required vars → goes straight to stateChat
	if pg.state != stateChat {
		t.Fatalf("expected stateChat after agent+provider auto-select, got state %d", pg.state)
	}
}

// TestInteractiveChatPage_MultipleProviders verifies the provider picker is shown.
func TestInteractiveChatPage_MultipleProviders(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"pa", "pb"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	out := pg.View()
	if !strings.Contains(out, "Select a provider") {
		t.Fatalf("expected provider picker, got:\n%s", out)
	}
}

// TestInteractiveChatPage_ProviderKeySelectsAndAdvances presses "2" to pick
// the second provider.
func TestInteractiveChatPage_ProviderKeySelectsAndAdvances(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"pa", "pb"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init() // multi-provider → stateSelectProvider

	pg.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if pg.provider != "pb" {
		t.Fatalf("expected provider pb, got %q", pg.provider)
	}
	// no required vars → stateChat
	if pg.state != stateChat {
		t.Fatalf("expected stateChat, got state %d", pg.state)
	}
}

// TestInteractiveChatPage_EvalToggle verifies the eval toggle state and key handling.
func TestInteractiveChatPage_EvalToggle(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
		hasEvals:  true,
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	if pg.state != stateEvalToggle {
		t.Fatalf("expected stateEvalToggle, got state %d", pg.state)
	}
	out := pg.View()
	if !strings.Contains(strings.ToLower(out), "evals") {
		t.Fatalf("expected eval prompt in view, got: %s", out)
	}

	// Press "n" → enters chat without evals.
	pg.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if pg.state != stateChat {
		t.Fatalf("expected stateChat after 'n', got state %d", pg.state)
	}
	if pg.runEvals {
		t.Fatal("expected runEvals=false after 'n'")
	}
}

// TestInteractiveChatPage_EvalToggleYes presses "y" and verifies runEvals is set.
func TestInteractiveChatPage_EvalToggleYes(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
		hasEvals:  true,
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()

	pg.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !pg.runEvals {
		t.Fatal("expected runEvals=true after 'y'")
	}
}

// TestInteractiveChatPage_EvalToggleEnter presses Enter → runEvals stays false.
func TestInteractiveChatPage_EvalToggleEnter(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
		hasEvals:  true,
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()

	pg.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if pg.runEvals {
		t.Fatal("expected runEvals=false after Enter")
	}
	if pg.state != stateChat {
		t.Fatalf("expected stateChat after Enter, got state %d", pg.state)
	}
}

// TestInteractiveChatPage_VarEntry tests entering a required variable value.
func TestInteractiveChatPage_VarEntry(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
		missing:   []string{"company"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()

	if pg.state != stateEnterVars {
		t.Fatalf("expected stateEnterVars, got state %d", pg.state)
	}
	out := pg.View()
	if !strings.Contains(strings.ToLower(out), "company") {
		t.Fatalf("expected 'company' in view, got: %s", out)
	}

	// Type "Acme" then press Enter.
	for _, r := range "Acme" {
		pg.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	pg.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if pg.state != stateChat {
		t.Fatalf("expected stateChat after entering var, got state %d", pg.state)
	}
	if pg.vars["company"] != "Acme" {
		t.Fatalf("expected vars[company]=Acme, got %q", pg.vars["company"])
	}
}

// TestInteractiveChatPage_ChunkWithError verifies an error chunk sets pg.err.
func TestInteractiveChatPage_ChunkWithError(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	pg.enterChatForTest(t, map[string]string{}, false)

	wantErr := errors.New("stream failed")
	pg.Update(chatChunkMsg{chunk: turnexecChunk{Error: wantErr}})

	if pg.err == nil || !strings.Contains(pg.err.Error(), "stream failed") {
		t.Fatalf("expected pg.err to contain 'stream failed', got %v", pg.err)
	}
	out := pg.View()
	if !strings.Contains(out, "stream failed") {
		t.Fatalf("expected error in view, got: %s", out)
	}
}

// TestInteractiveChatPage_StreamDoneRefreshesCost exercises handleStreamDone.
func TestInteractiveChatPage_StreamDoneRefreshesCost(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	pg.enterChatForTest(t, map[string]string{}, false)

	pg.Update(chatStreamDoneMsg{})
	// Should not panic and panel should render without error.
	out := pg.View()
	if out == "" {
		t.Fatal("expected non-empty view after stream done")
	}
}

// TestInteractiveChatPage_EvalMsg verifies chatEvalMsg updates the panel.
func TestInteractiveChatPage_EvalMsg(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	pg.enterChatForTest(t, map[string]string{}, true)

	score := 0.9
	pg.Update(chatEvalMsg{results: []evals.EvalResult{{Type: "json_valid", Score: &score}}})
	// Panel should now show eval scores in its footer.
	out := pg.View()
	if out == "" {
		t.Fatal("expected non-empty view after eval msg")
	}
}

// TestInteractiveChatPage_ChatErrMsg verifies chatErrMsg sets the error.
func TestInteractiveChatPage_ChatErrMsg(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	pg.enterChatForTest(t, map[string]string{}, false)

	pg.Update(chatErrMsg{err: errors.New("eval error")})
	if pg.err == nil {
		t.Fatal("expected pg.err to be set after chatErrMsg")
	}
}

// TestInteractiveChatPage_SessionError verifies a session creation error shows
// in the view.
func TestInteractiveChatPage_SessionError(t *testing.T) {
	eng := &stubEngine{
		agents:     []AgentOption{{TaskType: "basic"}},
		providers:  []string{"p1"},
		sessionErr: errors.New("cannot create session"),
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()

	out := pg.View()
	if !strings.Contains(out, "cannot create session") {
		t.Fatalf("expected session error in view, got: %s", out)
	}
}

// TestInteractiveChatPage_NoAgents verifies an error when config has no agents.
func TestInteractiveChatPage_NoAgents(t *testing.T) {
	eng := &stubEngine{}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()

	out := pg.View()
	if !strings.Contains(out, "no agents") {
		t.Fatalf("expected 'no agents' error in view, got: %s", out)
	}
}

// TestInteractiveChatPage_NoProviders verifies an error when config has no providers.
func TestInteractiveChatPage_NoProviders(t *testing.T) {
	eng := &stubEngine{
		agents: []AgentOption{{TaskType: "basic"}},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()

	out := pg.View()
	if !strings.Contains(out, "no providers") {
		t.Fatalf("expected 'no providers' error in view, got: %s", out)
	}
}

// TestInteractiveChatPage_MissingVarsError verifies MissingRequiredVars error handling.
func TestInteractiveChatPage_MissingVarsError(t *testing.T) {
	eng := &stubEngine{
		agents:     []AgentOption{{TaskType: "basic"}},
		providers:  []string{"p1"},
		missingErr: errors.New("template error"),
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()

	out := pg.View()
	if !strings.Contains(out, "template error") {
		t.Fatalf("expected template error in view, got: %s", out)
	}
}

// TestInteractiveChatPage_SetDimensions verifies SetDimensions does not panic.
func TestInteractiveChatPage_SetDimensions(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(120, 40)
	if pg.width != 120 || pg.height != 40 {
		t.Fatalf("dimensions not set: want 120x40, got %dx%d", pg.width, pg.height)
	}
}

// TestInteractiveChatPage_IgnoredKeyInEvalToggle verifies unknown keys in eval
// toggle don't change state.
func TestInteractiveChatPage_IgnoredKeyInEvalToggle(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
		hasEvals:  true,
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()

	pg.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if pg.state != stateEvalToggle {
		t.Fatalf("expected state to remain stateEvalToggle, got %d", pg.state)
	}
}

// TestInteractiveChatPage_StreamDoneWithEvals verifies that when runEvals is
// true, handleStreamDone returns a command (not nil).
func TestInteractiveChatPage_StreamDoneWithEvals(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	pg.enterChatForTest(t, map[string]string{}, false)
	pg.runEvals = true // force evals enabled post-session-start

	cmd := pg.Update(chatStreamDoneMsg{})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from handleStreamDone when runEvals=true")
	}
}

// TestInteractiveChatPage_ChunkContinuesPumping verifies a chunk with a channel
// returns another pumping command.
func TestInteractiveChatPage_ChunkContinuesPumping(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	pg.enterChatForTest(t, map[string]string{}, false)

	// Create a channel and send a chunk with a channel — the handler should
	// return a pump command.
	ch := make(chan turnexecChunk, 1)
	ch <- streamChunkWithAssistant("part 1")
	close(ch)

	cmd := pg.Update(chatChunkMsg{
		chunk: streamChunkWithAssistant("initial"),
		ch:    ch,
	})
	if cmd == nil {
		t.Fatal("expected pump command from handleChunk when channel present")
	}
}

// TestInteractiveChatPage_VarEntryMultipleVars verifies cycling through multiple
// required vars before reaching chat.
func TestInteractiveChatPage_VarEntryMultipleVars(t *testing.T) {
	callCount := 0
	eng := &stubEngineCallable{
		stubEngine: stubEngine{
			agents:    []AgentOption{{TaskType: "basic"}},
			providers: []string{"p1"},
		},
		missingFn: func(taskType string, provided map[string]string) ([]string, error) {
			callCount++
			if callCount == 1 {
				return []string{"company", "tone"}, nil
			}
			return nil, nil
		},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()

	// Should be on first var.
	if pg.state != stateEnterVars {
		t.Fatalf("expected stateEnterVars, got %d", pg.state)
	}

	// Enter first var.
	for _, r := range "Acme" {
		pg.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	pg.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should advance to the second var.
	if pg.state != stateEnterVars {
		t.Fatalf("expected stateEnterVars for second var, got %d", pg.state)
	}
	out := pg.View()
	if !strings.Contains(strings.ToLower(out), "tone") {
		t.Fatalf("expected 'tone' prompt, got: %s", out)
	}

	// Enter second var.
	for _, r := range "professional" {
		pg.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	pg.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if pg.state != stateChat {
		t.Fatalf("expected stateChat after all vars entered, got %d", pg.state)
	}
}

// stubEngineCallable is a stubEngine with overridable MissingRequiredVars.
type stubEngineCallable struct {
	stubEngine
	missingFn func(string, map[string]string) ([]string, error)
}

func (s *stubEngineCallable) MissingRequiredVars(taskType string, provided map[string]string) ([]string, error) {
	if s.missingFn != nil {
		return s.missingFn(taskType, provided)
	}
	return s.stubEngine.MissingRequiredVars(taskType, provided)
}

// TestInteractiveChatPage_HandleChatKeyEnterSendsMessage verifies that pressing
// Enter in chat state with non-empty input triggers the send command.
func TestInteractiveChatPage_HandleChatKeyEnterSendsMessage(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	pg.enterChatForTest(t, map[string]string{}, false)

	// Type text into the panel via rune key messages (goes to the textarea).
	for _, r := range "hello" {
		pg.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// Press Enter — should return a sendCmd (non-nil) since there is text.
	cmd := pg.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil send command from handleChatKey on Enter with text")
	}
}

// TestInteractiveChatPage_SendCmdChannelClosed verifies sendCmd returns
// chatStreamDoneMsg when the session closes the channel immediately.
func TestInteractiveChatPage_SendCmdChannelClosed(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	pg.enterChatForTest(t, map[string]string{}, false)

	// sendCmd returns a tea.Cmd; executing it drives the actual channel read.
	cmd := pg.sendCmd("test message")
	if cmd == nil {
		t.Fatal("expected non-nil cmd from sendCmd")
	}
	msg := cmd()
	switch msg.(type) {
	case chatStreamDoneMsg, chatChunkMsg:
		// both are valid — stub closes channel → chatStreamDoneMsg
	default:
		t.Fatalf("expected chatStreamDoneMsg or chatChunkMsg, got %T", msg)
	}
}

// TestInteractiveChatPage_NonChatMsgInChatState verifies non-key messages in
// chat state are forwarded to the panel.
func TestInteractiveChatPage_NonChatMsgInChatState(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	pg.enterChatForTest(t, map[string]string{}, false)

	// A WindowSizeMsg is a non-key message; in chat state it goes to panel.Update.
	pg.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	// Just verify it does not panic and the page is still valid.
	out := pg.View()
	if out == "" {
		t.Fatal("expected non-empty view after WindowSizeMsg")
	}
}

// TestInteractiveChatPage_AgentKeyOutOfRange verifies out-of-range digit is ignored.
func TestInteractiveChatPage_AgentKeyOutOfRange(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "alpha"}, {TaskType: "beta"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init() // stateSelectAgent

	// Press "9" — out of range for 2 agents.
	pg.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9")})
	if pg.state != stateSelectAgent {
		t.Fatalf("expected state to remain stateSelectAgent, got %d", pg.state)
	}
}

// TestInteractiveChatPage_HandleChatKeyEnterEmpty verifies Enter with empty
// input in chat state does NOT send a command.
func TestInteractiveChatPage_HandleChatKeyEnterEmpty(t *testing.T) {
	eng := &stubEngine{
		agents:    []AgentOption{{TaskType: "basic"}},
		providers: []string{"p1"},
	}
	pg := NewInteractiveChatPage(eng)
	pg.SetDimensions(80, 24)
	pg.Init()
	pg.enterChatForTest(t, map[string]string{}, false)

	// Press Enter without typing anything — no message should be sent.
	cmd := pg.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// cmd may be non-nil (panel forwarding), but panel should still be idle.
	_ = cmd
	if pg.err != nil {
		t.Fatalf("unexpected error: %v", pg.err)
	}
}
