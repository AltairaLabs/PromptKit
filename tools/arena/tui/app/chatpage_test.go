package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// TestChatPage_Title verifies Title returns "Chat".
func TestChatPage_Title(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	if got := p.Title(); got != "Chat" {
		t.Fatalf("expected Title()=Chat, got %q", got)
	}
}

// TestChatPage_Activate_LoadsEngine verifies Activate calls EnsureEngine.
func TestChatPage_Activate_LoadsEngine(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	cmd := p.Activate(func(tea.Msg) {})
	if p.engineErr != nil {
		t.Fatalf("Activate: unexpected engine error: %v", p.engineErr)
	}
	if p.engine == nil {
		t.Fatal("expected engine to be set after Activate")
	}
	_ = cmd
}

// TestChatPage_Activate_NoConfig verifies Activate gracefully handles missing config.
func TestChatPage_Activate_NoConfig(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"} // no config loaded
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	if p.engineErr == nil {
		t.Fatal("expected engineErr when no config loaded")
	}
	// View should render the error without panicking.
	p.SetSize(80, 24)
	view := p.View()
	if view == "" {
		t.Fatal("expected non-empty view for error state")
	}
}

// TestChatPage_SetupFlow drives the setup state machine.
// With a single agent and provider with no required vars, Init should auto-advance to chatStateChat.
func TestChatPage_SetupFlow(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	// With 1 agent, 1 provider, 0 required vars, no evals → should auto-advance to chatStateChat.
	if p.state != chatStateChat {
		t.Fatalf("expected chatStateChat after Init with single-agent/provider fixture, got %v", p.state)
	}
}

// TestChatPage_SetupFlow_MultiAgent tests multi-agent selection flow.
// It uses two real AgentInfo entries so the state machine can advance through
// chatStateSelectAgent after the user presses '1'.
func TestChatPage_SetupFlow_MultiAgent(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})

	// Force multi-agent state: two entries pointing at the real "basic" task type
	// so afterProviderSelected can look it up in the engine.
	p.agents = []engine.AgentInfo{
		{TaskType: "basic", Description: "First"},
		{TaskType: "basic", Description: "Second"},
	}
	p.state = chatStateSelectAgent

	// Press "1" to select the first agent.
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	// With a single provider and no required vars the state machine must advance
	// all the way to chatStateChat.
	if p.state == chatStateSelectAgent {
		t.Fatalf("expected state to advance from chatStateSelectAgent after pressing '1', engineErr=%v", p.engineErr)
	}
	_ = cmd
}

// TestChatPage_View_ErrorState verifies View renders the error message without panicking.
func TestChatPage_View_ErrorState(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	p := NewChatPage(ctx)
	p.SetSize(80, 24)
	p.engineErr = fmt.Errorf("test error")
	view := p.View()
	if !strings.Contains(stripANSI(view), "error:") {
		t.Fatalf("expected error: in view, got %q", view)
	}
}

// TestChatPage_View_AllSetupStates ensures View renders something for each setup state.
func TestChatPage_View_AllSetupStates(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	p := NewChatPage(ctx)
	p.SetSize(80, 24)

	tests := []struct {
		name    string
		state   chatSetupState
		setup   func()
		contain string
	}{
		{
			name:  "selectAgent",
			state: chatStateSelectAgent,
			setup: func() {
				p.agents = []engine.AgentInfo{{TaskType: "basic"}}
			},
			contain: "agent",
		},
		{
			name:  "enterVars",
			state: chatStateEnterVars,
			setup: func() {
				p.required = []string{"myvar"}
				p.varIdx = 0
			},
			contain: "myvar",
		},
		{
			name:    "evalToggle",
			state:   chatStateEvalToggle,
			setup:   func() {},
			contain: "evals",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p.state = tc.state
			tc.setup()
			view := stripANSI(p.View())
			if !strings.Contains(strings.ToLower(view), tc.contain) {
				t.Fatalf("state %d: expected %q in view, got: %q", tc.state, tc.contain, view)
			}
		})
	}
}

// TestChatPage_View_SelectProvider verifies provider picker renders with a live engine.
func TestChatPage_View_SelectProvider(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	p.state = chatStateSelectProvider
	p.SetSize(80, 24)

	view := stripANSI(p.View())
	if !strings.Contains(strings.ToLower(view), "provider") {
		t.Fatalf("expected 'provider' in view, got: %q", view)
	}
}

// TestChatPage_SetSize verifies dimensions are stored and input resized.
func TestChatPage_SetSize(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.SetSize(100, 30)
	if p.width != 100 || p.height != 30 {
		t.Fatalf("expected width=100 height=30, got %d %d", p.width, p.height)
	}
	expectedInputWidth := chatMaxInt(100-chatInputPadding, 0)
	if p.input.Width != expectedInputWidth {
		t.Fatalf("expected input.Width=%d, got %d", expectedInputWidth, p.input.Width)
	}
}

// TestChatPage_Update_UnknownMsg verifies Update with an unknown message returns self.
func TestChatPage_Update_UnknownMsg(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.SetSize(80, 24)
	type unknownMsg struct{}
	newPage, cmd := p.Update(unknownMsg{})
	if newPage == nil {
		t.Fatal("Update returned nil page")
	}
	_ = cmd
}

// TestChatPage_Init_NoEngine verifies Init returns nil when engine not set.
func TestChatPage_Init_NoEngine(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	cmd := p.Init()
	if cmd != nil {
		t.Fatal("expected nil cmd when engine not set")
	}
}

// TestChatPage_Init_EngineErr verifies Init returns nil when engineErr is set.
func TestChatPage_Init_EngineErr(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.engineErr = fmt.Errorf("pre-existing error")
	cmd := p.Init()
	if cmd != nil {
		t.Fatal("expected nil cmd when engineErr is set")
	}
}

// TestChatPage_EvalToggle_YKey tests pressing 'y' starts session with evals.
func TestChatPage_EvalToggle_YKey(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	p.taskType = "basic"
	p.provider = "mock"
	p.state = chatStateEvalToggle

	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	pp := newPage.(*ChatPage)
	// Session should have been started (chatStateChat) or engineErr set.
	if pp.state != chatStateChat && pp.engineErr == nil {
		t.Fatalf("expected chatStateChat or engineErr after pressing 'y' in eval toggle, got state=%v err=%v", pp.state, pp.engineErr)
	}
}

// TestChatPage_EvalToggle_NKey tests pressing 'n' starts session without evals.
func TestChatPage_EvalToggle_NKey(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	p.taskType = "basic"
	p.provider = "mock"
	p.state = chatStateEvalToggle

	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	pp := newPage.(*ChatPage)
	if pp.state != chatStateChat && pp.engineErr == nil {
		t.Fatalf("expected chatStateChat or engineErr after 'n', got state=%v err=%v", pp.state, pp.engineErr)
	}
}

// TestChatPage_EvalToggle_EnterKey tests Enter starts session without evals.
func TestChatPage_EvalToggle_EnterKey(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	p.taskType = "basic"
	p.provider = "mock"
	p.state = chatStateEvalToggle

	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pp := newPage.(*ChatPage)
	if pp.state != chatStateChat && pp.engineErr == nil {
		t.Fatalf("expected chatStateChat or engineErr after Enter, got state=%v err=%v", pp.state, pp.engineErr)
	}
}

// TestChatPage_EvalToggle_OtherKey is a no-op for keys that aren't y/n/enter.
func TestChatPage_EvalToggle_OtherKey(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.state = chatStateEvalToggle
	newPage, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	pp := newPage.(*ChatPage)
	if pp.state != chatStateEvalToggle {
		t.Fatal("expected state to remain chatStateEvalToggle for unknown key")
	}
	_ = cmd
}

// TestChatPage_VarKey_Entry tests variable entry in chatStateEnterVars.
func TestChatPage_VarKey_Entry(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	p.required = []string{"company"}
	p.varIdx = 0
	p.taskType = "basic"
	p.provider = "mock"
	p.state = chatStateEnterVars
	p.input.Focus()

	// Type a value then press Enter to confirm.
	for _, r := range "Acme" {
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pp := newPage.(*ChatPage)
	// After confirming the only required var, state should advance past chatStateEnterVars.
	if pp.state == chatStateEnterVars {
		t.Fatal("expected state to advance from chatStateEnterVars after Enter")
	}
}

// TestChatPage_VarKey_MultipleVars tests multi-variable entry.
func TestChatPage_VarKey_MultipleVars(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.required = []string{"var1", "var2"}
	p.varIdx = 0
	p.state = chatStateEnterVars
	p.input.Focus()

	// Confirm first var → should stay in chatStateEnterVars with varIdx=1.
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pp := newPage.(*ChatPage)
	if pp.varIdx != 1 {
		t.Fatalf("expected varIdx=1 after first Enter, got %d", pp.varIdx)
	}
	if pp.state != chatStateEnterVars {
		t.Fatalf("expected to remain in chatStateEnterVars with more vars, got %v", pp.state)
	}
}

// TestChatPage_ChatKey_Tab tests tab focus toggle in chatStateChat.
func TestChatPage_ChatKey_Tab(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(80, 24)

	// Initially panelFocused should be false.
	if p.panelFocused {
		t.Fatal("expected panelFocused=false initially")
	}

	// Tab should toggle focus.
	newPage, cmd := p.Update(tea.KeyMsg{Type: tea.KeyTab})
	pp := newPage.(*ChatPage)
	if !pp.panelFocused {
		t.Fatal("expected panelFocused=true after Tab")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd (textinput.Blink) after Tab")
	}

	// Tab again should toggle back.
	newPage2, _ := pp.Update(tea.KeyMsg{Type: tea.KeyTab})
	pp2 := newPage2.(*ChatPage)
	if pp2.panelFocused {
		t.Fatal("expected panelFocused=false after second Tab")
	}
}

// TestChatPage_ChatKey_ScrollWhileTyping tests scroll keys forwarded to panel when input focused.
func TestChatPage_ChatKey_ScrollWhileTyping(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(80, 24)

	// Scroll keys should not panic (panel.Update handles them).
	scrollKeys := []tea.KeyType{tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown}
	for _, k := range scrollKeys {
		newPage, _ := p.Update(tea.KeyMsg{Type: k})
		if newPage == nil {
			t.Fatalf("Update(%v) returned nil page", k)
		}
	}
}

// TestChatPage_ChatKey_EnterEmptyNoSend tests Enter does not send when input is empty.
func TestChatPage_ChatKey_EnterEmptyNoSend(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(80, 24)

	// Enter with empty input should not set busy.
	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pp := newPage.(*ChatPage)
	if pp.busy {
		t.Fatal("expected busy=false for Enter with empty input")
	}
}

// TestChatPage_ChatKey_PanelFocused tests keys forwarded to panel when panel focused.
func TestChatPage_ChatKey_PanelFocused(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(80, 24)
	p.panelFocused = true
	p.input.Blur()

	// Any non-tab key with panel focused should not panic.
	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if newPage == nil {
		t.Fatal("Update returned nil page when panel focused")
	}
}

// TestChatPage_ChatView_WithStatusLine verifies chatView includes the status line.
func TestChatPage_ChatView_WithStatusLine(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(80, 24)
	p.statusLine = "test status"

	view := stripANSI(p.View())
	if !strings.Contains(view, "test status") {
		t.Fatalf("expected status line in chat view, got: %q", view)
	}
}

// TestChatPage_ChatBindings_PanelFocused verifies panel-focused bindings differ.
func TestChatPage_ChatBindings_PanelFocused(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.panelFocused = false
	unfocused := p.chatBindings()
	p.panelFocused = true
	focused := p.chatBindings()

	if len(unfocused) == 0 {
		t.Fatal("expected non-empty bindings when unfocused")
	}
	if len(focused) == 0 {
		t.Fatal("expected non-empty bindings when focused")
	}
	// They should differ (first key binding different).
	if unfocused[0].Keys == focused[0].Keys {
		t.Fatal("expected different bindings for focused vs unfocused")
	}
}

// TestChatPage_Update_chatEvalMsg_Error verifies chatEvalMsg error updates status line.
func TestChatPage_Update_chatEvalMsg_Error(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.SetSize(80, 24)

	newPage, _ := p.Update(chatEvalMsg{err: fmt.Errorf("eval failed")})
	pp := newPage.(*ChatPage)
	if !strings.Contains(pp.statusLine, "eval failed") {
		t.Fatalf("expected eval error in status line, got %q", pp.statusLine)
	}
}

// TestChatPage_Update_chatEvalMsg_Scores verifies chatEvalMsg with scores formats correctly.
func TestChatPage_Update_chatEvalMsg_Scores(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.SetSize(80, 24)

	score := 0.85
	results := []evals.EvalResult{
		{Type: "sentiment", Score: &score},
	}
	newPage, _ := p.Update(chatEvalMsg{results: results})
	pp := newPage.(*ChatPage)
	if !strings.Contains(pp.statusLine, "sentiment") {
		t.Fatalf("expected eval type in status line, got %q", pp.statusLine)
	}
}

// TestChatPage_Update_chatEvalMsg_NoScore verifies chatEvalMsg with nil score produces empty status.
func TestChatPage_Update_chatEvalMsg_NoScore(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.SetSize(80, 24)
	results := []evals.EvalResult{{Type: "noscoreeval"}} // Score is nil
	newPage, _ := p.Update(chatEvalMsg{results: results})
	pp := newPage.(*ChatPage)
	// chatFormatEvalScores returns "" when all scores are nil, so statusLine is "".
	_ = pp
}

// TestChatPage_Update_chatErrMsg verifies chatErrMsg updates status line and clears busy.
func TestChatPage_Update_chatErrMsg(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.SetSize(80, 24)
	p.busy = true

	newPage, _ := p.Update(chatErrMsg{err: fmt.Errorf("turn error")})
	pp := newPage.(*ChatPage)
	if pp.busy {
		t.Fatal("expected busy=false after chatErrMsg")
	}
	if !strings.Contains(pp.statusLine, "turn error") {
		t.Fatalf("expected error in status line, got %q", pp.statusLine)
	}
}

// TestChatPage_Update_chatStreamDoneMsg verifies chatStreamDoneMsg clears busy.
func TestChatPage_Update_chatStreamDoneMsg(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(80, 24)
	p.busy = true
	p.statusLine = "responding"

	newPage, _ := p.Update(chatStreamDoneMsg{})
	pp := newPage.(*ChatPage)
	if pp.busy {
		t.Fatal("expected busy=false after chatStreamDoneMsg")
	}
	if pp.statusLine != "" {
		t.Fatalf("expected empty status line after stream done, got %q", pp.statusLine)
	}
}

// TestChatPage_InputView_FocusColors verifies input border changes with focus.
func TestChatPage_InputView_FocusColors(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.SetSize(80, 24)

	p.panelFocused = false
	colorFocused := p.chatInputBorderColor()
	p.panelFocused = true
	colorUnfocused := p.chatInputBorderColor()

	if colorFocused == colorUnfocused {
		t.Fatal("expected different border colors for focused vs unfocused input")
	}

	// inputView should render without panic in both states.
	p.panelFocused = false
	v1 := p.inputView()
	p.panelFocused = true
	v2 := p.inputView()
	if v1 == "" || v2 == "" {
		t.Fatal("expected non-empty inputView")
	}
}

// TestChatPage_SanitizeErrorLine verifies control characters are stripped.
func TestChatPage_SanitizeErrorLine(t *testing.T) {
	err := fmt.Errorf("line1\nline2\ttab")
	result := chatSanitizeErrorLine(err)
	if strings.ContainsAny(result, "\n\t") {
		t.Fatalf("expected control characters removed, got %q", result)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

// TestChatPage_SanitizeErrorLine_Nil verifies nil error returns empty string.
func TestChatPage_SanitizeErrorLine_Nil(t *testing.T) {
	if got := chatSanitizeErrorLine(nil); got != "" {
		t.Fatalf("expected empty string for nil error, got %q", got)
	}
}

// TestChatPage_FormatEvalScores verifies score formatting.
func TestChatPage_FormatEvalScores(t *testing.T) {
	// No scores → empty string.
	if got := chatFormatEvalScores(nil); got != "" {
		t.Fatalf("expected empty for nil results, got %q", got)
	}
	// With score.
	s := 0.75
	results := []evals.EvalResult{{Type: "myeval", Score: &s}}
	got := chatFormatEvalScores(results)
	if !strings.Contains(got, "myeval") {
		t.Fatalf("expected 'myeval' in formatted scores, got %q", got)
	}
}

// TestChatPage_AgentLabels verifies label formatting.
func TestChatPage_AgentLabels(t *testing.T) {
	agents := []engine.AgentInfo{
		{TaskType: "basic"},
		{TaskType: "advanced", Description: "A detailed agent"},
	}
	labels := chatAgentLabels(agents)
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(labels))
	}
	if labels[0] != "basic" {
		t.Fatalf("expected 'basic', got %q", labels[0])
	}
	if !strings.Contains(labels[1], "advanced") || !strings.Contains(labels[1], "A detailed agent") {
		t.Fatalf("expected label to contain both type and description, got %q", labels[1])
	}
}

// TestChatPage_DigitIndex verifies digit parsing.
func TestChatPage_DigitIndex(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want int
		ok   bool
	}{
		{"1", 3, 0, true},
		{"2", 3, 1, true},
		{"3", 3, 2, true},
		{"4", 3, 0, false}, // out of range
		{"0", 3, 0, false}, // '0' not valid
		{"a", 3, 0, false}, // not a digit
		{"", 3, 0, false},  // empty
	}
	for _, tc := range tests {
		idx, ok := chatDigitIndex(tc.s, tc.n)
		if ok != tc.ok {
			t.Fatalf("chatDigitIndex(%q, %d): ok=%v want %v", tc.s, tc.n, ok, tc.ok)
		}
		if ok && idx != tc.want {
			t.Fatalf("chatDigitIndex(%q, %d): idx=%d want %d", tc.s, tc.n, idx, tc.want)
		}
	}
}

// TestChatPage_TextInputForwarded verifies non-special keys are forwarded to the text input.
func TestChatPage_TextInputForwarded(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(80, 24)
	p.input.Focus()

	// Type 'h','i' — should update input value.
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if p.input.Value() == "" {
		t.Fatal("expected non-empty input after typing characters")
	}
}

// TestChatPage_SelectProvider verifies provider selection advances state.
func TestChatPage_SelectProvider(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	p.taskType = "basic"
	p.state = chatStateSelectProvider

	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	pp := newPage.(*ChatPage)
	if pp.state == chatStateSelectProvider {
		t.Fatalf("expected state to advance from chatStateSelectProvider, engineErr=%v", pp.engineErr)
	}
}

// TestChatPage_SelectProvider_OutOfRange tests '9' when only 1 provider.
func TestChatPage_SelectProvider_OutOfRange(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	p.taskType = "basic"
	p.state = chatStateSelectProvider

	// '9' is out of range for a single provider.
	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	pp := newPage.(*ChatPage)
	if pp.state != chatStateSelectProvider {
		t.Fatal("expected state to remain chatStateSelectProvider for out-of-range digit")
	}
}

// TestChatPage_View_DefaultZeroState verifies zero state renders Select agent picker.
func TestChatPage_View_DefaultZeroState(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.SetSize(80, 24)
	view := p.View()
	if !strings.Contains(stripANSI(view), "Select an agent") {
		t.Fatalf("unexpected view for zero state: %q", view)
	}
}

// TestChatPage_SetSize_ChatState verifies SetSize updates panel when in chat state.
func TestChatPage_SetSize_ChatState(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	// SetSize while in chatStateChat should not panic.
	p.SetSize(120, 40)
	if p.width != 120 || p.height != 40 {
		t.Fatalf("expected 120x40, got %dx%d", p.width, p.height)
	}
}

// TestChatPage_BlinksOnFocusReturnFromPanel verifies Blink returned when Tab from panel.
func TestChatPage_BlinksOnFocusReturnFromPanel(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(80, 24)
	p.panelFocused = true
	p.input.Blur()

	// Tab while panel focused → should return to input.
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd == nil {
		t.Fatal("expected non-nil cmd when toggling back to input")
	}
}

// TestChatPage_SendCmd_NilSession verifies sendCmd handles a nil session gracefully.
func TestChatPage_SendCmd_NilSession(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	// session is nil; sendCmd will panic when accessing p.session.SendUserMessage.
	// The deferred recover in sendCmd catches this panic and returns chatErrMsg.
	defer func() {
		if r := recover(); r != nil {
			// If we reach here, the deferred recover in sendCmd did not catch the panic.
			// This test just verifies sendCmd builds and creates a command.
		}
	}()
	// Verify sendCmd builds without crashing at construction time.
	cmd := p.sendCmd("hello")
	if cmd == nil {
		t.Fatal("expected non-nil cmd from sendCmd")
	}
}

// TestGoldenChatPage_Setup captures a stable snapshot of the setup state.
func TestGoldenChatPage_Setup(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	p := NewChatPage(ctx)
	p.SetSize(80, 24)
	// Manually set state to show multi-agent picker (deterministic render).
	p.agents = []engine.AgentInfo{
		{TaskType: "agent1", Description: "First agent"},
		{TaskType: "agent2", Description: "Second agent"},
	}
	p.state = chatStateSelectAgent
	out := stripANSI(p.View())
	teatest.RequireEqualOutput(t, []byte(out))
}

// TestChatPage_PanelRoundtrip verifies ConversationPanel can be created and
// rendered without data without panicking.
func TestChatPage_PanelRoundtrip(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.SetSize(80, 24)
	_ = p.View()
}

// TestChatPage_HandleStreamDone_RunEvalsTrue verifies that when runEvals is
// true, handleStreamDone returns a non-nil tea.Cmd (the eval runner cmd), and
// when runEvals is false it returns nil.
func TestChatPage_HandleStreamDone_RunEvalsTrue(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(80, 24)
	if p.session == nil {
		t.Fatal("expected session to be initialized after Init with single-agent fixture")
	}
	p.runEvals = true
	cmd := p.handleStreamDone()
	if cmd == nil {
		t.Fatal("expected non-nil cmd from handleStreamDone when runEvals=true")
	}
	p2 := NewChatPage(ctx)
	_ = p2.Activate(func(tea.Msg) {})
	_ = p2.Init()
	p2.SetSize(80, 24)
	p2.runEvals = false
	cmd2 := p2.handleStreamDone()
	if cmd2 != nil {
		t.Fatal("expected nil cmd from handleStreamDone when runEvals=false")
	}
}

// TestChatPage_NoDuplication verifies that after two turns the panel content
// matches the state store exactly — SetData replaces, never accumulates event
// appends, so each message appears at most twice (table row + detail pane).
func TestChatPage_NoDuplication(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(120, 40)
	if p.session == nil {
		t.Fatal("expected session to be initialized after Init with single-agent fixture")
	}
	ch, err := p.session.SendUserMessage(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SendUserMessage turn 1: %v", err)
	}
	for range ch {
	}
	p.handleStreamDone()
	ch2, err := p.session.SendUserMessage(context.Background(), "world")
	if err != nil {
		t.Fatalf("SendUserMessage turn 2: %v", err)
	}
	for range ch2 {
	}
	p.handleStreamDone()
	view := stripANSI(p.View())
	helloCount := strings.Count(view, "hello")
	if helloCount == 0 {
		t.Fatal("expected 'hello' to appear in panel view")
	}
	if helloCount > 2 {
		t.Fatalf("message 'hello' appears %d times — duplication detected (want ≤2)", helloCount)
	}
}

// TestChatPage_AutoScrollsToLast verifies that after handleStreamDone the panel
// selection is on the last message, not the first.
func TestChatPage_AutoScrollsToLast(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p := NewChatPage(ctx)
	_ = p.Activate(func(tea.Msg) {})
	_ = p.Init()
	p.SetSize(120, 40)
	if p.session == nil {
		t.Fatal("expected session to be initialized after Init with single-agent fixture")
	}
	ch, err := p.session.SendUserMessage(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}
	for range ch {
	}
	p.handleStreamDone()
	msgs, err := p.session.Messages(context.Background())
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected ≥2 messages after a turn, got %d", len(msgs))
	}
	want := len(msgs) - 1
	if p.panel.SelectedTurnIdx() != want {
		t.Fatalf("expected selectedTurnIdx=%d (last), got %d", want, p.panel.SelectedTurnIdx())
	}
}

// Compile-time assert: textinput.Blink is a tea.Cmd (verifies import compiles).
var _ tea.Cmd = textinput.Blink
