package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// guardedReply is a canned reply as runBeforeCallHooks (direction input) or an
// output block (direction output) records it.
func guardedReply(content, direction string) types.Message {
	return types.Message{
		Role: core.RoleAssistant, Content: content, FinishReason: types.FinishReasonSafety,
		Validations: []types.ValidationResult{{
			ValidatorType: "content_excludes",
			Details:       map[string]any{"direction": direction},
		}},
	}
}

func blockedInputReply(content string) types.Message {
	return guardedReply(content, hooks.DirectionInput)
}

func roleLines(msgs []types.Message) []string {
	out := make([]string, 0, len(msgs))
	for i := range msgs {
		out = append(out, msgs[i].Role+":"+msgs[i].GetContent())
	}
	return out
}

func toolResultMsg(name, content string) types.Message {
	tr := types.NewTextToolResult("call-"+name, name, content)
	return types.NewToolResultMessage(tr)
}

func largeToolResult(name string, wordCount int) types.Message {
	// Use realistic content with spaces so token counting works (~1.3 tokens/word)
	content := strings.Repeat("the quick brown fox jumps over the lazy dog ", wordCount/9+1)
	maxLen := wordCount * 6
	if maxLen > len(content) {
		maxLen = len(content)
	}
	return toolResultMsg(name, content[:maxLen])
}

// fakeClock simulates wall-clock time for the pacing and call-rate tests.
// now() is held in a field that callers can advance manually; sleep advances
// it by the requested duration unless onSleep overrides it. (A copy of
// internal/voice's: a stage test cannot use a voice test helper.)
type fakeClock struct {
	t time.Time
	// onSleep, if non-nil, is called for each sleep instead of advancing
	// time and returning nil.
	onSleep func(ctx context.Context, d time.Duration) error
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Unix(0, 0)}
}

func (f *fakeClock) now() time.Time { return f.t }

// idleProbe stands in for the pipeline's idle timer through the seam stages
// already use, the idle control ContextWithIdleReset puts on the context.
//
// The real timer races wall-clock time: a test that runs slow work against a
// 50ms window fails whenever the runner is loaded enough to delay a heartbeat.
// With the probe the test decides when an idle window opens and closes. The
// window held if a reset arrived while it was open, and lapsed (the timer
// would have fired) if none did.
type idleProbe struct {
	resets chan struct{}
}

// idleProbeTimeout is the idle window the probe reports. KeepIdleAlive paces
// its heartbeat at half of it, so a small one keeps passing tests fast.
const idleProbeTimeout = 2 * time.Millisecond

func newIdleProbe() *idleProbe {
	return &idleProbe{resets: make(chan struct{}, 1)}
}

// context returns ctx carrying the probe as its idle control.
func (p *idleProbe) context(ctx context.Context) context.Context {
	return core.ContextWithIdleReset(ctx, func() {
		select {
		case p.resets <- struct{}{}:
		default:
		}
	}, idleProbeTimeout)
}

func interruptedReply(content string) types.Message {
	return types.Message{Role: "assistant", Content: content, FinishReason: types.FinishReasonInterrupted}
}

// errStageBoom is the failure the test stages below report.
var errStageBoom = errors.New("stage boom")

func userElement(text string) core.StreamElement {
	return core.NewMessageElement(&types.Message{Role: "user", Content: text})
}

func errorElements(out <-chan core.StreamElement) []error {
	var errs []error
	for e := range out {
		if e.Error != nil {
			errs = append(errs, e.Error)
		}
	}
	return errs
}

// stageErrCountingProvider counts model calls.
type stageErrCountingProvider struct {
	providers.Provider
	mu    sync.Mutex
	calls int
}

func (p *stageErrCountingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// hostTexts is a BlockedMessageFunc answering reply, recording what it is given.
type hostTexts struct {
	reply string
	mu    sync.Mutex
	got   []BlockedMessage
}

func (h *hostTexts) fn(_ context.Context, b BlockedMessage) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.got = append(h.got, b)
	return h.reply
}

func (h *hostTexts) calls() []BlockedMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]BlockedMessage(nil), h.got...)
}

// textStage is a ProviderStage on the mock provider, streaming or not, with
// hook and fn.
func textStage(streaming bool, hook hooks.ProviderHook, fn BlockedMessageFunc) *ProviderStage {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false), streaming: streaming}
	return NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{MaxTokens: 100, BlockedMessage: fn},
		nil, hooks.NewRegistry(hooks.WithProviderHook(hook)))
}

// Helper to create a test message element
func newTestMsgElement(role, content string) core.StreamElement {
	msg := &types.Message{
		Role:    role,
		Content: content,
	}
	return core.NewMessageElement(msg)
}

// Helper to run a stage and collect output
func runTestStage(t *testing.T, s core.Stage, inputs []core.StreamElement) []core.StreamElement {
	t.Helper()

	input := make(chan core.StreamElement, len(inputs))
	for _, elem := range inputs {
		input <- elem
	}
	close(input)

	output := make(chan core.StreamElement, 100)
	ctx := context.Background()

	err := s.Process(ctx, input, output)
	require.NoError(t, err)

	var results []core.StreamElement
	for elem := range output {
		results = append(results, elem)
	}
	return results
}

func bannedWordsHook(t *testing.T, word string) hooks.ProviderHook {
	t.Helper()
	guard, err := guardrails.NewGuardrailHook("banned_words", map[string]any{"words": []any{word}},
		guardrails.WithMessage("I can't say that."))
	require.NoError(t, err)
	return guard
}

// grantingExec is a "local" executor whose "activate" tool grants the tools
// named by grant, as skill__activate does, and whose other tools return ok.
type grantingExec struct {
	grant []string

	mu      sync.Mutex
	granted []string
	ran     []string
}

// grants is the live grant accessor (DuplexToolConfig.ToolGrants).
func (e *grantingExec) grants() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.granted...)
}

func (e *grantingExec) calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.ran...)
}

// countingEcho is a local executor that counts its runs.
type countingEcho struct{ runs atomic.Int32 }

func (e *countingEcho) Name() string { return "counting-echo" }

// passthroughRecorderStage records every element it sees and forwards it, so a
// test can prove a stage downstream of ProviderStage still ran.
type passthroughRecorderStage struct {
	mu   sync.Mutex
	seen []types.Message
}

func (s *passthroughRecorderStage) messages() []types.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]types.Message(nil), s.seen...)
}

// finishReasonProvider is a non-streaming fake that returns a fixed canonical
// finish reason, used to verify the provider stage copies it onto the message.
type finishReasonProvider struct {
	delayedStreamProvider
}

// delayedStreamProvider simulates a provider that waits before producing its
// first chunk — like Ollama queued behind other requests.
type delayedStreamProvider struct {
	providers.Provider
	delay    time.Duration
	response string

	// gate, when set, replaces delay: the reply waits until gate closes.
	// waiting, when set, is closed once the call has started waiting.
	gate    chan struct{}
	waiting chan struct{}
}

// reasoningEndToEndProvider returns a response carrying reasoning, and records the
// messages it was asked to predict on (to verify reasoning never re-enters context).
type reasoningEndToEndProvider struct {
	delayedStreamProvider
	gotMessages []types.Message
}

// loopingToolProvider calls "echo" with fresh arguments on every round it is
// offered tools, so only a round limit ends its loop.
type loopingToolProvider struct {
	*mock.ToolProvider
	rounds atomic.Int64
}

// afterCallTexts is an output hook recording the text of each completed reply
// it judges.
type afterCallTexts struct{ seen []string }

// transcriptSTT is a base.STTProvider returning a fixed transcript.
type transcriptSTT struct{ text string }

// openWindow starts an idle window, forgetting resets that came before it.
func (p *idleProbe) openWindow() {
	for {
		select {
		case <-p.resets:
		default:
			return
		}
	}
}

// closeWindow ends the window and reports whether it held: a reset arrived
// after openWindow.
func (p *idleProbe) closeWindow() bool {
	select {
	case <-p.resets:
		return true
	case <-time.After(idleProbeBound):
		return false
	}
}

func (e *grantingExec) Name() string { return "local" }

func (e *grantingExec) Execute(_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage) (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ran = append(e.ran, d.Name)
	if d.Name == "activate" {
		e.granted = append([]string(nil), e.grant...)
	}
	return json.RawMessage(`"ok"`), nil
}

// idleProbeBound is how long closeWindow waits for a reset before it calls the
// window lapsed. It only limits how long a failing test takes: a passing one
// returns at the first reset.
const idleProbeBound = 2 * time.Second

func (f *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	if f.onSleep != nil {
		return f.onSleep(ctx, d)
	}
	f.t = f.t.Add(d)
	return nil
}

func (p *stageErrCountingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record()
	return p.Provider.Predict(ctx, req)
}

func (p *stageErrCountingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record()
	return p.Provider.PredictStream(ctx, req)
}

func (p *stageErrCountingProvider) record() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
}

func (e *grantingExec) revoke() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.granted = nil
}

// grantNow replaces the live grants, as a host granting a tool between
// rounds does.
func (e *grantingExec) grantNow(names ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.granted = append([]string(nil), names...)
}
