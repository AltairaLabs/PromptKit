package duplex

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/cue"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/provider"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/require"
)

// Helpers copied from stage's test files: duplex's tests cannot import
// stage (#2876), so the ones both packages use are kept on each side.

// maxCharsChunkInterceptor is a test ChunkInterceptor that aborts when
// accumulated content exceeds maxChars.
type maxCharsChunkInterceptor struct {
	maxChars    int
	accumulated string
}

// denyAllProviderHook is a test hook that denies all provider calls.
type denyAllProviderHook struct {
	reason   string
	metadata map[string]any
}

// allowBeforeDenyAfterHook allows BeforeCall but denies AfterCall.
type allowBeforeDenyAfterHook struct {
	reason   string
	metadata map[string]any
}

// denyToolHook blocks specific tools by name.
type denyToolHook struct {
	blockedTool string
	reason      string
}

// enforceBeforeCallHook enforces on BeforeCall, supplying canned text.
type enforceBeforeCallHook struct {
	reason      string
	replacement string
	metadata    map[string]any
}

// enforceAfterCallHook enforces on AfterCall, replacing the response content.
type enforceAfterCallHook struct {
	replacement string
}

// collectValidationFailures subscribes to validation.failed and returns a
// getter that closes the bus (flushing async delivery) and returns the data.
func collectValidationFailures(
	t *testing.T, bus *events.EventBus,
) func() []*events.ValidationEventData {
	t.Helper()
	var mu sync.Mutex
	var received []*events.Event
	bus.Subscribe(events.EventValidationFailed, func(e *events.Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})
	return func() []*events.ValidationEventData {
		bus.Close() // flush: the bus dispatches on a worker pool
		mu.Lock()
		defer mu.Unlock()
		out := make([]*events.ValidationEventData, 0, len(received))
		for _, e := range received {
			if d, ok := e.Data.(*events.ValidationEventData); ok {
				out = append(out, d)
			}
		}
		return out
	}
}

func registryWithTools(t *testing.T, names ...string) *tools.Registry {
	t.Helper()
	r := tools.NewRegistry()
	for _, n := range names {
		err := r.Register(&tools.ToolDescriptor{
			Name:        n,
			Description: "desc-" + n,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Mode:        "local",
		})
		if err != nil {
			t.Fatalf("Register %q: %v", n, err)
		}
	}
	return r
}

// mockAsyncExecutor implements tools.AsyncToolExecutor for testing
type mockAsyncExecutor struct {
	name       string
	status     tools.ToolExecutionStatus
	content    []byte
	parts      []types.ContentPart
	errorMsg   string
	pendingMsg string
}

// fakeResolver returns a scripted sequence of Handoffs, repeating the last.
// The resolver contract is "what should this turn be running", so a real
// implementation returns the ORIGIN state's prompt until the transition
// commits and the DESTINATION's afterwards — the sequence models that.
type fakeResolver struct {
	sequence []provider.Handoff
	err      error
	meta     map[string]any
	calls    int
}

// holdTestDeadline bounds every wait in these tests. A held round over a few
// scripted chunks takes milliseconds; a broken one may never return (a
// segmenter loop that makes no progress spins inside Process, a judgment no
// one answers blocks it), and must fail here, not at the test binary's
// timeout.
const holdTestDeadline = 5 * time.Second

// within returns what f returns, failing the test when f has not returned by
// holdTestDeadline. f runs on its own goroutine, so a call that spins or
// blocks forever still fails the test.
func within[T any](t *testing.T, what string, f func() T) T {
	t.Helper()
	done := make(chan T, 1)
	go func() { done <- f() }()
	select {
	case v := <-done:
		return v
	case <-time.After(holdTestDeadline):
		t.Fatalf("%s did not return within %v", what, holdTestDeadline)
		var zero T
		return zero
	}
}

func (h *maxCharsChunkInterceptor) Name() string { return "max_chars_test" }

func (h *maxCharsChunkInterceptor) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *maxCharsChunkInterceptor) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *maxCharsChunkInterceptor) OnChunk(
	_ context.Context, chunk *providers.StreamChunk,
) hooks.Decision {
	h.accumulated += chunk.Delta
	if len(h.accumulated) > h.maxChars {
		return hooks.Deny(fmt.Sprintf(
			"content length %d exceeds max %d", len(h.accumulated), h.maxChars,
		))
	}
	return hooks.Allow
}

func (h *denyAllProviderHook) Name() string { return "deny_all" }

func (h *denyAllProviderHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.DenyWithMetadata(h.reason, h.metadata)
}

func (h *denyAllProviderHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *allowBeforeDenyAfterHook) Name() string { return "deny_after" }

func (h *allowBeforeDenyAfterHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *allowBeforeDenyAfterHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.DenyWithMetadata(h.reason, h.metadata)
}

func (h *denyToolHook) Name() string { return "deny_tool" }

func (h *denyToolHook) BeforeExecution(
	_ context.Context, req hooks.ToolRequest,
) hooks.Decision {
	if req.Name == h.blockedTool {
		return hooks.Deny(h.reason)
	}
	return hooks.Allow
}

func (h *denyToolHook) AfterExecution(
	_ context.Context, _ hooks.ToolRequest, _ hooks.ToolResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforceBeforeCallHook) Name() string { return "enforce_before" }

func (h *enforceBeforeCallHook) BeforeCall(
	_ context.Context, req *hooks.ProviderRequest,
) hooks.Decision {
	req.Replacement = h.replacement
	return hooks.Enforced(h.reason, h.metadata)
}

func (h *enforceBeforeCallHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforceAfterCallHook) Name() string { return "enforce_after" }

func (h *enforceAfterCallHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforceAfterCallHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	resp.Message.Content = h.replacement
	return hooks.Enforced("output blocked", map[string]any{
		"validator_type": "test_output",
	})
}

func (m *mockAsyncExecutor) Name() string {
	return m.name
}

func (m *mockAsyncExecutor) Execute(_ context.Context, descriptor *tools.ToolDescriptor, _ json.RawMessage) (json.RawMessage, error) {
	if m.status == tools.ToolStatusFailed {
		return nil, fmt.Errorf("%s", m.errorMsg)
	}
	return m.content, nil
}

func (m *mockAsyncExecutor) ExecuteAsync(_ context.Context, descriptor *tools.ToolDescriptor, _ json.RawMessage) (*tools.ToolExecutionResult, error) {
	result := &tools.ToolExecutionResult{
		Status:  m.status,
		Content: m.content,
		Parts:   m.parts,
		Error:   m.errorMsg,
	}
	if m.status == tools.ToolStatusPending {
		result.PendingInfo = &tools.PendingToolInfo{
			Reason:   "requires_approval",
			Message:  m.pendingMsg,
			ToolName: descriptor.Name,
		}
	}
	return result, nil
}

func (f *fakeResolver) ResolveCurrentState(_ context.Context) (provider.Handoff, error) {
	if f.err != nil {
		return provider.Handoff{}, f.err
	}
	f.calls++
	if len(f.sequence) == 0 {
		return provider.Handoff{}, nil
	}
	idx := f.calls - 1
	if idx >= len(f.sequence) {
		idx = len(f.sequence) - 1
	}
	return f.sequence[idx], nil
}

func (f *fakeResolver) CurrentStateMeta() map[string]any { return f.meta }

const cueTestRate = 24000

const brunoText = "We don't talk about Bruno."

func startChain(t *testing.T, stages ...processor) *stageChain {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := &stageChain{in: make(chan core.StreamElement, 64), errs: make(chan error, len(stages)), n: len(stages)}
	in := c.in
	for _, s := range stages {
		out := make(chan core.StreamElement, 64)
		go func(s processor, in <-chan core.StreamElement, out chan core.StreamElement) {
			c.errs <- s.Process(ctx, in, out)
		}(s, in, out)
		in = out
	}
	c.out = in
	return c
}

// cueAudioOf concatenates the samples of the cue audio elements in elems.
func cueAudioOf(elems []core.StreamElement) []byte {
	var b []byte
	for _, e := range elems {
		if e.Audio != nil && e.Meta.CueAudio != nil {
			b = append(b, e.Audio.Samples...)
		}
	}
	return b
}

// spokenSession is a session whose replies are spoken in voice at rate.
func spokenSession(voice string, rate int) *providersmock.MockStreamSession {
	return providersmock.NewMockStreamSession().WithOutputAudio(
		providers.OutputAudio{Voice: voice, SampleRate: rate, Family: "test-live"})
}

func captureLogSync(t *testing.T) *syncLogBuffer {
	t.Helper()
	b := &syncLogBuffer{}
	logger.SetOutput(b)
	t.Cleanup(func() { logger.SetOutput(nil) })
	return b
}

// processor is the part of a Stage a chain needs.
type processor interface {
	Process(ctx context.Context, input <-chan core.StreamElement, output chan<- core.StreamElement) error
}

// stageChain runs stages connected in order.
type stageChain struct {
	in   chan core.StreamElement
	out  chan core.StreamElement
	errs chan error
	n    int
}

// next is the chain's next output element.
func (c *stageChain) next(t *testing.T) core.StreamElement {
	t.Helper()
	select {
	case e, ok := <-c.out:
		require.True(t, ok, "the chain closed its output early")
		return e
	case <-time.After(3 * time.Second):
		require.FailNow(t, "no element came out")
		return core.StreamElement{}
	}
}

// finish closes the input and returns everything still to come out. It
// gives the chain 3 seconds in all, not per element, and fails at once past
// maxChainOutput elements, so a stage that never stops emitting fails the
// test instead of hanging it.
func (c *stageChain) finish(t *testing.T) []core.StreamElement {
	t.Helper()
	close(c.in)
	deadline := time.After(3 * time.Second)
	var got []core.StreamElement
	for {
		select {
		case e, ok := <-c.out:
			if !ok {
				for range c.n {
					require.NoError(t, <-c.errs)
				}
				return got
			}
			got = append(got, e)
			if len(got) > maxChainOutput {
				require.FailNow(t, "the chain kept emitting", "more than %d elements", maxChainOutput)
			}
		case <-deadline:
			require.FailNow(t, "the chain never finished")
			return nil
		}
	}
}

// syncLogBuffer is a goroutine-safe log sink: the timeout monitor logs from
// its own goroutine while the test reads.
type syncLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// maxChainOutput is more elements than any test's chain emits.
const maxChainOutput = 1000

func roleLines(msgs []types.Message) []string {
	out := make([]string, 0, len(msgs))
	for i := range msgs {
		out = append(out, msgs[i].Role+":"+msgs[i].GetContent())
	}
	return out
}
func guardrailHook(t *testing.T, typ string, params map[string]any, opts ...guardrails.GuardrailOption) hooks.ProviderHook {
	t.Helper()
	h, err := guardrails.NewGuardrailHook(typ, params, opts...)
	require.NoError(t, err)
	return h
}

// liveText renders the live text elements the caller receives, each as
// "<text>" or, when it replaces the turn's text so far, "replace:<text>".
func liveText(elems []core.StreamElement) []string {
	var out []string
	for _, e := range elems {
		if e.Text == nil || e.Message != nil {
			continue
		}
		if e.Meta.ReplacesText {
			out = append(out, "replace:"+*e.Text)
		} else {
			out = append(out, *e.Text)
		}
	}
	return out
}

func runProviderStage(
	t *testing.T,
	stage *provider.ProviderStage,
	userContent string,
) ([]core.StreamElement, error) {
	t.Helper()
	input := make(chan core.StreamElement, 1)
	msg := types.Message{Role: "user", Content: userContent}
	elem := core.NewMessageElement(&msg)
	input <- elem
	close(input)

	output := make(chan core.StreamElement, 50)
	err := stage.Process(context.Background(), input, output)

	var elems []core.StreamElement
	for e := range output {
		elems = append(elems, e)
	}
	return elems, err
}

// assistantMessages extracts assistant messages from emitted stream elements.
func assistantMessages(elems []core.StreamElement) []types.Message {
	var out []types.Message
	for _, e := range elems {
		if e.Message != nil && e.Message.Role == "assistant" {
			out = append(out, *e.Message)
		}
	}
	return out
}

// testCueBackchannel is a preemptible cue kind no source emits yet: a short
// acknowledgment played while the caller speaks, which gives way to reply audio.
const testCueBackchannel core.CueKind = "backchannel"

// cueRamp is n samples of PCM16 mono that never fall to silence, so no byte of
// it is trimmed and every byte is distinct from a neighbor's.
func cueRamp(n int) []byte {
	b := make([]byte, 2*n)
	for i := range n {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(1000+i%700)) //nolint:gosec // small positive values
	}
	return b
}

// newFakeTTS answers every text with pcm, as PCM at 24 kHz.
func newFakeTTS(pcm []byte) *fakeTTS {
	return &fakeTTS{
		voices: []tts.Voice{{ID: "alloy", Name: "Alloy"}, {ID: "v-2", Name: "Verse"}},
		formats: []tts.AudioFormat{tts.FormatMP3,
			{Name: "pcm", MIMEType: "audio/pcm", SampleRate: 24000, BitDepth: 16, Channels: 1}},
		audio: func(string) ([]byte, error) { return pcm, nil },
	}
}

// storedTexts records each text the bank stores a rendered clip for.
func storedTexts(s *cue.CueStage) <-chan string {
	ch := make(chan string, 8)
	cue.OnStored(cue.BankOf(s), func(text string, _ cue.Clip) { ch <- text })
	return ch
}

func waitStored(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case text := <-ch:
		return text
	case <-time.After(3 * time.Second):
		require.FailNow(t, "the rendered clip was never stored")
		return ""
	}
}

// cueFaded is what a cue stage plays of pcm, a clip already at rate with no
// silence at either end: pcm with its last cueFadeOut faded to silence.
func cueFaded(pcm []byte, rate int) []byte {
	n := rate * int(cueFadeOut/time.Millisecond) / 1000
	split := len(pcm) - 2*n
	out := append([]byte(nil), pcm[:split]...)
	return append(out, audio.FadeOutPCM16(pcm[split:], n)...)
}

const noPineappleMsg = "Let's talk about something else."

// enforcingChunkInterceptor fires once from OnChunk with a caller-owned
// metadata map, so a test can mutate that map after the run and observe
// whether the recorded validation aliased it.
type enforcingChunkInterceptor struct {
	meta  map[string]any
	fired bool
}

func (h *enforcingChunkInterceptor) Name() string { return "enforcing_chunk_test" }

func (h *enforcingChunkInterceptor) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforcingChunkInterceptor) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforcingChunkInterceptor) OnChunk(
	_ context.Context, _ *providers.StreamChunk,
) hooks.Decision {
	if h.fired {
		return hooks.Allow
	}
	h.fired = true
	// Enforced rather than Deny: the stage stops reading the stream but returns
	// the message normally, so the stamped validation is observable.
	return hooks.Enforced("streaming guardrail fired", h.meta)
}

// blockMessageHook is a provider hook that blocks with msg.
type blockMessageHook struct{ msg string }

func (h blockMessageHook) Name() string { return "block" }

func (h blockMessageHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h blockMessageHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

func (h blockMessageHook) BlockMessages() []string { return []string{h.msg} }

// thoughtful is an assistant reply that carries the model's reasoning.
func thoughtful() *types.Message {
	return &types.Message{
		Role: core.RoleAssistant, Content: "the banned answer",
		Reasoning: &types.ReasoningTrace{
			Text:   "reasoning toward the banned answer",
			Opaque: []types.OpaqueReasoning{{Provider: "claude", Kind: "thinking_signature", Data: "sig"}},
		},
	}
}

// liveTextOf is liveText counting the text an element that also completes the
// turn carries.
func liveTextOf(elems []core.StreamElement) []string {
	var out []string
	for _, e := range elems {
		if e.Text == nil || (e.Message != nil && e.Message.Role != core.RoleAssistant) {
			continue
		}
		if e.Meta.ReplacesText {
			out = append(out, "replace:"+*e.Text)
		} else {
			out = append(out, *e.Text)
		}
	}
	return out
}

// sendScriptedDeltas sends each delta as a chunk carrying the reply so far.
func sendScriptedDeltas(stream chan<- providers.StreamChunk, deltas ...string) {
	sofar := ""
	for _, d := range deltas {
		sofar += d
		stream <- providers.StreamChunk{Delta: d, Content: sofar}
	}
}

// guardedStage is a streaming provider stage reading the test's stream with
// hs as its provider hooks, in order.
func guardedStage(stream chan providers.StreamChunk, hs ...hooks.ProviderHook) *provider.ProviderStage {
	opts := make([]hooks.Option, 0, len(hs))
	for _, h := range hs {
		opts = append(opts, hooks.WithProviderHook(h))
	}
	prov := &scriptedChannelProvider{Provider: providersmock.NewProvider("p", "m", false), stream: stream}
	return provider.NewProviderStageWithHooks(prov, nil, nil, &provider.ProviderConfig{MaxTokens: 100}, nil, hooks.NewRegistry(opts...))
}

// lastAssistantOf is the last assistant message among elems.
func lastAssistantOf(t *testing.T, elems []core.StreamElement) *types.Message {
	t.Helper()
	for i := len(elems) - 1; i >= 0; i-- {
		if m := elems[i].Message; m != nil && m.Role == core.RoleAssistant {
			return m
		}
	}
	require.FailNow(t, "no assistant message")
	return nil
}

// afterCallTexts is an output hook recording the text of each completed reply
// it judges.
type afterCallTexts struct{ seen []string }

func (*afterCallTexts) Name() string { return "after_call_texts" }

func (*afterCallTexts) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *afterCallTexts) AfterCall(_ context.Context, _ *hooks.ProviderRequest, r *hooks.ProviderResponse) hooks.Decision {
	h.seen = append(h.seen, r.Message.Content)
	return hooks.Allow
}

// pcm24k is the format of a clip a 24 kHz session plays.
var pcm24k = cueclip.Format{SampleRate: 24000, Channels: 1}

// redactionRecordingProvider captures the PredictionRequest the provider
// actually received, on whichever path the stage takes. Named distinctly from
// the package's other recordingProvider (composition_executor_test.go), which
// records only metadata and isn't a providers.Provider wrapper.
//
// streaming selects the path: false (the zero value) routes the stage through
// executeRound, true through executeStreamingRound. Both Predict and
// PredictStream record, so the same "provider never called" / "provider saw the
// mutated request" proofs hold on either path.
type redactionRecordingProvider struct {
	providers.Provider
	streaming bool
	mu        sync.Mutex
	requests  []providers.PredictionRequest
	calls     int
}

func (p *redactionRecordingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *redactionRecordingProvider) SupportsStreaming() bool { return p.streaming }

func (p *redactionRecordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req)
	return p.Provider.Predict(ctx, req)
}

func (p *redactionRecordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	return p.Provider.PredictStream(ctx, req)
}

func (p *redactionRecordingProvider) record(req providers.PredictionRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
	p.calls++
}

// userInputs returns, per request the provider received, the content of the
// request's last user message.
func (p *redactionRecordingProvider) userInputs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, req := range p.requests {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				out = append(out, req.Messages[i].GetContent())
				break
			}
		}
	}
	return out
}

func (p *redactionRecordingProvider) lastRequest() providers.PredictionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[len(p.requests)-1]
}

// fakeTTS is a TTS service that records each synthesis and answers with
// audio(text). With gate set, a synthesis waits for it to close.
type fakeTTS struct {
	voices  []tts.Voice
	formats []tts.AudioFormat
	audio   func(text string) ([]byte, error)
	gate    chan struct{}

	mu      sync.Mutex
	configs []tts.SynthesisConfig
}

func (f *fakeTTS) Name() string { return "fake" }

func (f *fakeTTS) SupportedVoices() []tts.Voice { return f.voices }

func (f *fakeTTS) SupportedFormats() []tts.AudioFormat { return f.formats }

func (f *fakeTTS) Synthesize(ctx context.Context, text string, cfg tts.SynthesisConfig) (io.ReadCloser, error) {
	f.mu.Lock()
	f.configs = append(f.configs, cfg)
	f.mu.Unlock()
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pcm, err := f.audio(text)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(pcm)), nil
}

func (f *fakeTTS) lastConfig() tts.SynthesisConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configs[len(f.configs)-1]
}

// cueFadeOut is how much of a clip's end the cue stage fades to silence.
const cueFadeOut = 10 * time.Millisecond

// scriptedChannelProvider streams whatever the test sends on its channel.
type scriptedChannelProvider struct {
	*providersmock.Provider
	stream chan providers.StreamChunk
}

func (p *scriptedChannelProvider) SupportsStreaming() bool { return true }

func (p *scriptedChannelProvider) PredictStream(
	context.Context, providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	return p.stream, nil
}

// fakeEventStore captures appended events for assertions.
// Implements events.EventStore.
type fakeEventStore struct {
	mu     sync.Mutex
	events []*events.Event
}

func (f *fakeEventStore) Append(_ context.Context, e *events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeEventStore) OnEvent(*events.Event) {}

func (f *fakeEventStore) Query(_ context.Context, _ *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}

func (f *fakeEventStore) QueryRaw(_ context.Context, _ *events.EventFilter) ([]*events.StoredEvent, error) {
	return nil, nil
}

func (f *fakeEventStore) Stream(_ context.Context, _ string) (<-chan *events.Event, error) {
	return nil, nil
}

func (f *fakeEventStore) Close() error { return nil }

// snapshot returns a copy of recorded events safe for inspection.
func (f *fakeEventStore) snapshot() []*events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*events.Event, len(f.events))
	copy(out, f.events)
	return out
}

// filterByType returns events whose Type matches.
func (f *fakeEventStore) filterByType(t events.EventType) []*events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*events.Event
	for _, e := range f.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}
