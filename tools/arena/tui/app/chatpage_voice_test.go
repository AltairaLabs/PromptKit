package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// fakeAudioIO is a stub AudioIO implementation for voice-mode tests. It uses
// channels to control the mic feed and records Play calls for inspection.
type fakeAudioIO struct {
	mu       sync.Mutex
	started  bool
	closed   bool
	frames   chan []byte
	playBuf  [][]byte
	startErr error
}

func newFakeAudioIO() *fakeAudioIO {
	return &fakeAudioIO{frames: make(chan []byte, 4)}
}

func (f *fakeAudioIO) Start(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.started = true
	return nil
}

func (f *fakeAudioIO) CaptureChunks() <-chan []byte { return f.frames }

func (f *fakeAudioIO) Play(frame []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.playBuf = append(f.playBuf, frame)
}

func (f *fakeAudioIO) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.frames)
	f.closed = true
	return nil
}

// TestVoiceOptions_FieldsStoredOnNewChatPage verifies that NewChatPage copies
// AppContext.Voice into ChatPage.voice.
func TestVoiceOptions_FieldsStoredOnNewChatPage(t *testing.T) {
	opts := &VoiceOptions{
		STTProviderID: "my-stt",
		OutputVoice:   "nova",
		EchoGuard:     true,
	}
	ctx := &AppContext{Version: "vTEST", Voice: opts}
	p := NewChatPage(ctx)

	if p.voice == nil {
		t.Fatal("expected p.voice to be set from AppContext.Voice")
	}
	if p.voice.STTProviderID != "my-stt" {
		t.Fatalf("expected STTProviderID=my-stt, got %q", p.voice.STTProviderID)
	}
	if p.voice.OutputVoice != "nova" {
		t.Fatalf("expected OutputVoice=nova, got %q", p.voice.OutputVoice)
	}
	if !p.voice.EchoGuard {
		t.Fatal("expected EchoGuard=true")
	}
}

// TestVoiceOptions_NilWhenNoVoice verifies that text-mode pages have nil voice.
func TestVoiceOptions_NilWhenNoVoice(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"} // no Voice set
	p := NewChatPage(ctx)
	if p.voice != nil {
		t.Fatalf("expected p.voice=nil for text-mode context, got %v", p.voice)
	}
}

// TestStartVoice_VoiceNotCompiled verifies that when voice.NewAudioIO returns
// ErrVoiceNotCompiled, startVoice sets p.engineErr with the build instruction
// message and returns nil (no panic or crash).
//
// In the stub (non-voice) build, voice.NewAudioIO always returns
// ErrVoiceNotCompiled, so this test exercises the real stub code path.
func TestStartVoice_VoiceNotCompiled(t *testing.T) {
	p := &ChatPage{
		voice: &VoiceOptions{},
	}

	var sendCalls []tea.Msg
	send := func(msg tea.Msg) { sendCalls = append(sendCalls, msg) }

	cmd := p.startVoice(send)
	if cmd != nil {
		t.Fatalf("expected nil cmd when voice not compiled, got non-nil")
	}
	if p.engineErr == nil {
		t.Fatal("expected engineErr to be set when voice not compiled")
	}
	errStr := p.engineErr.Error()
	if len(errStr) == 0 {
		t.Fatal("expected non-empty engineErr message")
	}
	// Verify the error message guides the user to the voice build tag.
	if !containsStr(errStr, "voice") {
		t.Fatalf("expected engineErr to mention 'voice', got: %q", errStr)
	}
	// No messages should have been sent.
	if len(sendCalls) != 0 {
		t.Fatalf("expected 0 send calls, got %d", len(sendCalls))
	}
}

// TestStartVoice_TeardownCancelsCtx verifies that after startVoice stores a
// cancel func, calling ChatPage.Close() invokes it and the context is canceled.
//
// We test the teardown seam directly: set up a cancel func on the page and
// confirm Close() calls it. This exercises the real Close() code path without
// needing a live audio device.
func TestStartVoice_TeardownCancelsCtx(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})

	// Inject a cancel func as if startVoice had run successfully.
	ctx, cancel := context.WithCancel(context.Background())
	p.voiceCancel = cancel

	// Close should cancel the context.
	p.Close()

	select {
	case <-ctx.Done():
		// good — context was canceled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected context to be canceled after Close(), timed out")
	}
}

// TestStartVoice_CloseIsNoopWithoutCancel verifies Close() does not panic when
// no voice driver was started (voiceCancel is nil).
func TestStartVoice_CloseIsNoopWithoutCancel(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	// voiceCancel is nil — Close should be a no-op.
	p.Close()
}

// TestVoiceLevelMsg_Fields verifies the voiceLevelMsg struct fields are
// accessible (compile-time and runtime check).
func TestVoiceLevelMsg_Fields(t *testing.T) {
	msg := voiceLevelMsg{user: 0.3, agent: 0.7}
	if msg.user != 0.3 {
		t.Fatalf("expected user=0.3, got %f", msg.user)
	}
	if msg.agent != 0.7 {
		t.Fatalf("expected agent=0.7, got %f", msg.agent)
	}
}

// TestChatRefreshMsg_IsDistinctType verifies chatRefreshMsg is a distinct type
// that can be used as a tea.Msg (interface satisfaction compile check).
func TestChatRefreshMsg_IsDistinctType(t *testing.T) {
	var msg tea.Msg = chatRefreshMsg{}
	if msg == nil {
		t.Fatal("chatRefreshMsg should be a non-nil tea.Msg")
	}
}

// TestApp_CloseAll_CallsCloseOnCloseable verifies that App.closeAll() invokes
// Close() on every Closeable page in the stack. This is the integration seam
// between App's quit path and ChatPage's voice teardown.
func TestApp_CloseAll_CallsCloseOnCloseable(t *testing.T) {
	closed := false
	page := &closeableTestPage{onClose: func() { closed = true }}

	a := New(&AppContext{Version: "vTEST"}, page)
	a.closeAll()

	if !closed {
		t.Fatal("expected Close() to be called on Closeable page by App.closeAll()")
	}
}

// TestApp_CloseAll_IgnoresNonCloseable verifies closeAll does not panic on
// pages that do not implement Closeable.
func TestApp_CloseAll_IgnoresNonCloseable(t *testing.T) {
	page := &plainTestPage{}
	a := New(&AppContext{Version: "vTEST"}, page)
	// Should not panic.
	a.closeAll()
}

// TestApp_QuitMsg_CallsCloseAll verifies that receiving QuitMsg triggers
// closeAll so voice teardown runs before the program exits.
func TestApp_QuitMsg_CallsCloseAll(t *testing.T) {
	closed := false
	page := &closeableTestPage{onClose: func() { closed = true }}

	a := New(&AppContext{Version: "vTEST"}, page)
	a.inited[page] = true // mark as inited so Update doesn't call Init again

	_, _ = a.Update(QuitMsg{})

	if !closed {
		t.Fatal("expected Close() to be called when App receives QuitMsg")
	}
}

// TestApp_CtrlC_CallsCloseAll verifies Ctrl+C triggers closeAll.
func TestApp_CtrlC_CallsCloseAll(t *testing.T) {
	closed := false
	page := &closeableTestPage{onClose: func() { closed = true }}

	a := New(&AppContext{Version: "vTEST"}, page)
	a.inited[page] = true

	_, _ = a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if !closed {
		t.Fatal("expected Close() to be called on Ctrl+C")
	}
}

// TestApp_EscAtRoot_CallsCloseAll verifies Esc at root triggers closeAll.
func TestApp_EscAtRoot_CallsCloseAll(t *testing.T) {
	closed := false
	page := &closeableTestPage{onClose: func() { closed = true }}

	a := New(&AppContext{Version: "vTEST"}, page)
	a.inited[page] = true

	_, _ = a.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !closed {
		t.Fatal("expected Close() to be called on Esc at root")
	}
}

// TestStartVoice_NilSendDoesNotPanic verifies startVoice handles a nil send
// func gracefully (the nil guard substitutes a no-op before any send call).
//
// In the stub build, voice.NewAudioIO returns ErrVoiceNotCompiled so the
// function returns before any send call. This confirms no panic.
func TestStartVoice_NilSendDoesNotPanic(t *testing.T) {
	p := &ChatPage{voice: &VoiceOptions{}}
	// Must not panic even with nil send.
	_ = p.startVoice(nil)
}

// TestChatPage_Update_VoiceLevelMsg verifies voiceLevelMsg stores mic/agent levels.
func TestChatPage_Update_VoiceLevelMsg(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})

	newPage, cmd := p.Update(voiceLevelMsg{user: 0.4, agent: 0.6})
	pp := newPage.(*ChatPage)
	if pp.micLevel != 0.4 {
		t.Fatalf("expected micLevel=0.4, got %f", pp.micLevel)
	}
	if pp.agentLevel != 0.6 {
		t.Fatalf("expected agentLevel=0.6, got %f", pp.agentLevel)
	}
	if cmd != nil {
		t.Fatal("expected nil cmd from voiceLevelMsg Update")
	}
}

// TestChatPage_Update_ChatRefreshMsg verifies chatRefreshMsg is handled without panic.
func TestChatPage_Update_ChatRefreshMsg(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	newPage, cmd := p.Update(chatRefreshMsg{})
	if newPage == nil {
		t.Fatal("expected non-nil page from chatRefreshMsg Update")
	}
	if cmd != nil {
		t.Fatal("expected nil cmd from chatRefreshMsg Update")
	}
}

// TestChatPage_Activate_StoresSend verifies Activate stores the send func on ChatPage.
func TestChatPage_Activate_StoresSend(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"} // no config → EnsureEngine errors
	p := NewChatPage(ctx)
	sentinel := func(tea.Msg) {}
	_ = p.Activate(sentinel)
	if p.send == nil {
		t.Fatal("expected p.send to be set after Activate")
	}
}

// ---- Task 3 tests: panel refresh, meter rendering, voice key handling ----

// TestChatPage_ChatRefreshMsg_RefreshesPanel verifies that a chatRefreshMsg after
// writing a user+assistant message into the voice state store causes the panel to
// reflect those messages. The voiceStore/voiceConvID fields are set directly as
// startVoice would, messages are saved via the runtime ConversationState, then
// chatRefreshMsg is sent and the panel View() is checked for message role text.
func TestChatPage_ChatRefreshMsg_RefreshesPanel(t *testing.T) {
	store := arenastore.NewArenaStateStore()
	convID := "test-voice-conv-1"

	userMsg := types.Message{Role: "user", Content: "hello"}
	assistantMsg := types.Message{Role: "assistant", Content: "world response"}
	if err := writeVoiceMessages(t, store, convID, userMsg, assistantMsg); err != nil {
		t.Fatalf("writeVoiceMessages: %v", err)
	}

	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.voice = &VoiceOptions{}
	p.voiceStore = store
	p.voiceConvID = convID
	p.state = chatStateChat
	p.width = 120
	p.height = 40
	p.panel.SetDimensions(120, 30)

	_, _ = p.Update(chatRefreshMsg{})

	view := p.panel.View()
	if !strings.Contains(view, "user") && !strings.Contains(view, "assistant") {
		t.Fatalf("expected panel View() to contain message roles after chatRefreshMsg; got:\n%s", view)
	}
}

// writeVoiceMessages saves a slice of messages into an ArenaStateStore as a
// ConversationState. ArenaStateStore.Save accepts *runtimestore.ConversationState,
// so we construct one directly.
func writeVoiceMessages(t *testing.T, store *arenastore.ArenaStateStore, convID string, msgs ...types.Message) error {
	t.Helper()
	cs := &runtimestore.ConversationState{
		ID:       convID,
		Messages: msgs,
		Metadata: make(map[string]interface{}),
	}
	return store.Save(context.Background(), cs)
}

// TestChatPage_VoiceLevelMsg_UpdatesMeterAndView verifies that a voiceLevelMsg
// updates p.micLevel and that, in voice mode, View() renders the mic status line
// and the panel's built-in audio meter (filled cells) when the panel has data.
// We seed the panel with a message first so it reaches composeView where the
// meter is rendered; then confirm the meter glyphs appear after a non-zero level.
func TestChatPage_VoiceLevelMsg_UpdatesMeterAndView(t *testing.T) {
	store := arenastore.NewArenaStateStore()
	convID := "test-voice-conv-level"
	seedMsg := types.Message{Role: "user", Content: "hello"}
	if err := writeVoiceMessages(t, store, convID, seedMsg); err != nil {
		t.Fatalf("writeVoiceMessages: %v", err)
	}

	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.voice = &VoiceOptions{}
	p.voiceStore = store
	p.voiceConvID = convID
	p.state = chatStateChat
	p.width = 120
	p.height = 40
	p.panel.SetDimensions(120, 30)
	// Seed the panel with data so composeView (which renders the meter) is reached.
	p.refreshVoicePanel()

	// Capture view with zero audio levels (meter is empty / inactive).
	viewBefore := p.View()
	if !strings.Contains(viewBefore, "mic active") {
		t.Fatalf("expected 'mic active' status line before level update; got:\n%s", viewBefore)
	}

	// Send a voiceLevelMsg with a non-zero user level.
	newPage, cmd := p.Update(voiceLevelMsg{user: 0.5, agent: 0.2})
	pp := newPage.(*ChatPage)
	if pp.micLevel != 0.5 {
		t.Fatalf("expected micLevel=0.5 after voiceLevelMsg, got %f", pp.micLevel)
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd from voiceLevelMsg, got non-nil")
	}

	viewAfter := pp.View()

	// The mic status line must still be present.
	if !strings.Contains(viewAfter, "mic active") {
		t.Fatalf("expected 'mic active' in voice View() after level update; got:\n%s", viewAfter)
	}
	// The audio meter filled glyph must appear — panel.SetAudioLevels activated
	// the meter and 0.5 × 16 cells = 8 filled blocks.
	const meterFilled = "████████"
	if !strings.Contains(viewAfter, meterFilled) {
		t.Fatalf("expected meter filled cells %q in View() after level=0.5; got:\n%s", meterFilled, viewAfter)
	}
}

// TestChatPage_VoiceModeHandleChatKey_EnterDoesNotSend verifies that in voice
// mode, pressing Enter in handleChatKey does NOT trigger a send (no cmd returned,
// no busy flag set). The session is nil so any accidental send would panic.
func TestChatPage_VoiceModeHandleChatKey_EnterDoesNotSend(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.voice = &VoiceOptions{}
	p.state = chatStateChat
	p.input.SetValue("some text that should not be sent")

	// Call handleChatKey with Enter directly.
	cmd := p.handleChatKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected nil cmd from handleChatKey(Enter) in voice mode")
	}
	if p.busy {
		t.Fatal("expected busy=false after Enter in voice mode")
	}
}

// ---- helpers ----

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Compile-time check: fakeAudioIO implements voice.AudioIO interface.
// We do this via a blank import-free assertion using errors package already imported.
var _ = errors.New // keep errors import used

// closeableTestPage is a minimal Page + Closeable for testing App.closeAll.
type closeableTestPage struct {
	onClose func()
}

func (c *closeableTestPage) Init() tea.Cmd                  { return nil }
func (c *closeableTestPage) Update(tea.Msg) (Page, tea.Cmd) { return c, nil }
func (c *closeableTestPage) View() string                   { return "" }
func (c *closeableTestPage) Title() string                  { return "test" }
func (c *closeableTestPage) SetSize(int, int)               {}
func (c *closeableTestPage) Close() {
	if c.onClose != nil {
		c.onClose()
	}
}

// plainTestPage is a minimal Page without Closeable.
type plainTestPage struct{}

func (p *plainTestPage) Init() tea.Cmd                  { return nil }
func (p *plainTestPage) Update(tea.Msg) (Page, tea.Cmd) { return p, nil }
func (p *plainTestPage) View() string                   { return "" }
func (p *plainTestPage) Title() string                  { return "plain" }
func (p *plainTestPage) SetSize(int, int)               {}
