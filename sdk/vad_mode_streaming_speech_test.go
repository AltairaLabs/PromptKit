package sdk

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// segmentTTSRate is the PCM rate segmentTTS synthesizes at.
const segmentTTSRate = 16000

// ttsCall is one synthesis request: its text, when it started, and whether its
// context was canceled before the test ended.
type ttsCall struct {
	text string
	at   time.Time
	ctx  context.Context
}

// segmentTTS records every synthesis call with the time it started, and
// returns 20ms of PCM whose samples all hold the call's ordinal × 1000, so the
// order of the audio the caller receives shows which call it came from. With
// blockFirst set, the first call waits for its context (a long synthesis a
// barge-in cancels).
type segmentTTS struct {
	convMockTTSService
	blockFirst bool

	mu    sync.Mutex
	calls []ttsCall
	first chan struct{}
	once  sync.Once
}

func newSegmentTTS() *segmentTTS {
	return &segmentTTS{convMockTTSService: *newConvMockTTSService(), first: make(chan struct{})}
}

func (s *segmentTTS) Synthesize(ctx context.Context, text string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
	s.mu.Lock()
	s.calls = append(s.calls, ttsCall{text: text, at: time.Now(), ctx: ctx})
	ordinal := len(s.calls)
	s.mu.Unlock()
	s.once.Do(func() { close(s.first) })
	if s.blockFirst && ordinal == 1 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return io.NopCloser(bytes.NewReader(markerPCM(ordinal))), nil
}

func (s *segmentTTS) SupportedFormats() []tts.AudioFormat {
	pcm := tts.FormatPCM16
	pcm.SampleRate = segmentTTSRate
	return []tts.AudioFormat{pcm}
}

func (s *segmentTTS) Pricing() *base.PricingDescriptor {
	return &base.PricingDescriptor{
		Source: base.PricingSourceInline, Currency: "usd",
		Items: []base.PriceItem{{Unit: "character", Rate: segmentTTSCharRate}},
	}
}

func (s *segmentTTS) ImplName() string  { return "segment-tts" }
func (s *segmentTTS) ModelName() string { return "segment-tts-model" }

const segmentTTSCharRate = 0.0001

func (s *segmentTTS) snapshot() []ttsCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ttsCall(nil), s.calls...)
}

func (s *segmentTTS) texts() []string {
	var out []string
	for _, c := range s.snapshot() {
		out = append(out, c.text)
	}
	return out
}

// markerPCM is 20ms of PCM at segmentTTSRate, every sample ordinal × 1000.
func markerPCM(ordinal int) []byte {
	n := segmentTTSRate / 50
	pcm := make([]byte, n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(ordinal*1000)))
	}
	return pcm
}

// markers returns the synthesis-call ordinals of the audio the caller
// received, in order, one per run of audio from the same call.
func markers(chunks [][]byte) []int {
	var out []int
	for _, c := range chunks {
		if len(c) < 2 {
			continue
		}
		m := int(int16(binary.LittleEndian.Uint16(c))) / 1000
		if len(out) == 0 || out[len(out)-1] != m {
			out = append(out, m)
		}
	}
	return out
}

// storyProvider streams a scripted reply per turn, one chunk at a time. Before
// sending chunk gateAt of the first turn it waits (up to gateWait) for gate
// to close, so a test can see what happens while the reply is still arriving.
// With holdAfter > 0, the first turn sends that many chunks and then keeps
// generating until its context is canceled, closing canceled.
type storyProvider struct {
	turnRecordingProvider
	turns     [][]string
	gate      <-chan struct{}
	gateAt    int
	gateWait  time.Duration
	holdAfter int
	canceled  chan struct{}

	mu        sync.Mutex
	lastChunk time.Time
	n         int
}

func (p *storyProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	p.mu.Lock()
	turn := p.n
	p.n++
	p.mu.Unlock()
	chunks := []string{"ack"}
	if turn < len(p.turns) {
		chunks = p.turns[turn]
	}
	ch := make(chan providers.StreamChunk)
	go func() {
		defer close(ch)
		content := ""
		for i, delta := range chunks {
			if turn == 0 && p.gate != nil && i == p.gateAt {
				select {
				case <-p.gate:
				case <-time.After(p.gateWait):
				}
			}
			content += delta
			chunk := providers.StreamChunk{Content: content, Delta: delta}
			if i == len(chunks)-1 && !(turn == 0 && p.holdAfter > 0) {
				stop := "stop"
				chunk.FinishReason = &stop
			}
			select {
			case ch <- chunk:
			case <-ctx.Done():
				return
			}
			p.mu.Lock()
			p.lastChunk = time.Now()
			p.mu.Unlock()
		}
		if turn == 0 && p.holdAfter > 0 {
			<-ctx.Done()
			close(p.canceled)
		}
	}()
	return ch, nil
}

func (p *storyProvider) lastChunkAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastChunk
}

// speechRecorder collects what the caller receives: the audio, in order, and
// how many assistant turns completed.
type speechRecorder struct {
	mu        sync.Mutex
	audio     [][]byte
	completes int
	interrupt bool
}

func (r *speechRecorder) consume(ch <-chan providers.StreamChunk) {
	for chunk := range ch {
		r.mu.Lock()
		if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
			r.audio = append(r.audio, append([]byte(nil), chunk.MediaData.Data...))
		}
		if done, _ := chunk.Metadata["assistant_turn_complete"].(bool); done {
			r.completes++
		}
		if chunk.Interrupted {
			r.interrupt = true
		}
		r.mu.Unlock()
	}
}

func (r *speechRecorder) turnCompletes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.completes
}

func (r *speechRecorder) audioMarkers() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return markers(r.audio)
}

func (r *speechRecorder) sawInterrupt() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.interrupt
}

func openStreamingSpeechConv(
	t *testing.T, provider providers.Provider, sttSvc *scriptedSTTService, ttsSvc tts.Service, opts ...Option,
) (*Conversation, *speechRecorder) {
	t.Helper()
	all := append([]Option{
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithVADMode(sttSvc, ttsSvc, vadTestConfig()),
	}, opts...)
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main", all...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	responseCh, err := conv.Response()
	require.NoError(t, err)
	rec := &speechRecorder{}
	go rec.consume(responseCh)
	return conv, rec
}

// storedAssistant returns the assistant messages the store holds for conv.
func storedAssistant(t *testing.T, store statestore.Store, convID string) []types.Message {
	t.Helper()
	state, err := store.Load(context.Background(), convID)
	require.NoError(t, err)
	var out []types.Message
	for _, m := range state.Messages {
		if m.Role == "assistant" {
			out = append(out, m)
		}
	}
	return out
}

// ttsCostUSD reads the total of the tts_cost stamped on msg.
func ttsCostUSD(t *testing.T, msg types.Message) float64 {
	t.Helper()
	cost, ok := msg.Meta["tts_cost"].(map[string]any)
	require.True(t, ok, "the reply carries its synthesis cost; meta %v", msg.Meta)
	usd, ok := cost["total_cost_usd"].(float64)
	require.True(t, ok, "tts_cost carries total_cost_usd; got %v", cost)
	return usd
}

// A VAD reply is spoken a sentence at a time as the model streams it: the
// first sentence is synthesized while the rest of the reply is still being
// generated, every sentence once, in order, and the turn's transcript and
// synthesis cost are the whole reply's (#2846).
func TestVADMode_SpeaksEachSentenceAsTheReplyStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	ttsSvc := newSegmentTTS()
	sentences := []string{"The fox woke early.", "It ran to the river.", "Then it slept."}
	provider := &storyProvider{
		turns: [][]string{{"The fox woke early. ", "It ran to the river. ", "Then it slept."}},
		// The rest of the reply waits for the first synthesis call, or 2s.
		gate: ttsSvc.first, gateAt: 1, gateWait: 2 * time.Second,
	}
	store := statestore.NewMemoryStore()
	conv, rec := openStreamingSpeechConv(t, provider, newScriptedSTT("tell me a story"), ttsSvc,
		WithStateStore(store))
	convID := conv.ID()

	speakOneUtterance(t, conv)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, 8*time.Second),
		"the reply never completed")

	calls := ttsSvc.snapshot()
	require.Equal(t, sentences, ttsSvc.texts(), "each sentence is synthesized once, in order")
	assert.True(t, calls[0].at.Before(provider.lastChunkAt()),
		"the first sentence is synthesized before the model finishes the reply (first call %v, reply end %v)",
		calls[0].at, provider.lastChunkAt())
	assert.Equal(t, []int{1, 2, 3}, rec.audioMarkers(), "the caller receives each sentence's audio, in order")

	require.NoError(t, conv.Close())
	stored := storedAssistant(t, store, convID)
	require.Len(t, stored, 1, "the turn stores one reply")
	assert.Equal(t, "The fox woke early. It ran to the river. Then it slept.", stored[0].Content)
	chars := 0
	for _, s := range sentences {
		chars += len(s)
	}
	assert.InDelta(t, float64(chars)*segmentTTSCharRate, ttsCostUSD(t, stored[0]), 1e-12,
		"the reply's synthesis cost is the sum of its sentences'")
}

// Caller speech while a reply is still being generated and synthesized cancels
// it: the model call, the synthesis in flight and the rest of the reply are
// all dropped, and what the caller said is the next turn (#2846).
func TestVADMode_CallerSpeechCancelsTheStreamingReply(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	ttsSvc := newSegmentTTS()
	ttsSvc.blockFirst = true
	provider := &storyProvider{
		turns:     [][]string{{"The fox woke early. ", "It ran to"}, {"Paris."}},
		holdAfter: 2,
		canceled:  make(chan struct{}),
	}
	conv, rec := openStreamingSpeechConv(t, provider, newScriptedSTT("tell me a story", "what is the capital"), ttsSvc)

	speakOneUtterance(t, conv)
	select {
	case <-ttsSvc.first:
	case <-time.After(8 * time.Second):
		t.Fatal("the first sentence was never synthesized while the reply streamed")
	}

	speakOneUtterance(t, conv) // talks over the reply

	select {
	case <-provider.canceled:
	case <-time.After(8 * time.Second):
		t.Fatal("caller speech must cancel the model call still generating the reply")
	}
	first := ttsSvc.snapshot()[0]
	require.True(t, waitFor(func() bool { return first.ctx.Err() != nil }, 8*time.Second),
		"caller speech must cancel the synthesis in flight")
	require.True(t, provider.waitForTurns(2, 8*time.Second), "the caller's speech must be the next turn")
	assert.Contains(t, userTextsIn(provider.turnAt(1)), "what is the capital")
	require.True(t, waitFor(func() bool { return len(ttsSvc.texts()) >= 2 }, 8*time.Second),
		"the next turn's reply is spoken")
	time.Sleep(20 * time.Millisecond) // nothing of the canceled reply may follow
	assert.Equal(t, []string{"The fox woke early.", "Paris."}, ttsSvc.texts(),
		"nothing of the canceled reply is synthesized after the caller spoke")
	assert.True(t, rec.sawInterrupt(), "the caller is told to flush playback")
	assert.Equal(t, []int{2}, rec.audioMarkers(), "only the next turn's reply is heard")
}

// startingOnlyVAD reports VADStateStarting for loud audio and never Speaking:
// a short utterance ("That's that.") that ends before a VAD calls it speech
// proper, though it is long enough to be a turn.
type startingOnlyVAD struct {
	mu    sync.Mutex
	state audio.VADState
}

func (v *startingOnlyVAD) Name() string { return "starting-only" }

func (v *startingOnlyVAD) Analyze(_ context.Context, pcm []byte) (float64, error) {
	var sum float64
	n := len(pcm) / 2
	for i := 0; i < n; i++ {
		s := float64(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / math.MaxInt16
		sum += s * s
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.state = audio.VADStateQuiet
	if n > 0 && math.Sqrt(sum/float64(n)) > 0.05 {
		v.state = audio.VADStateStarting
		return 0.6, nil
	}
	return 0, nil
}

func (v *startingOnlyVAD) State() audio.VADState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.state
}

func (v *startingOnlyVAD) OnStateChange() <-chan audio.VADEvent { return nil }
func (v *startingOnlyVAD) Reset()                               {}

// An utterance that becomes a turn while a reply is pending barges in on it,
// even when the VAD never reported it as Speaking: it used to be queued as the
// next turn and answered after the whole reply had played (#2846).
func TestVADMode_ShortUtteranceDuringAPendingReplyBargesIn(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	provider := &storyProvider{
		turns:     [][]string{{"Once upon a time"}, {"Paris."}},
		holdAfter: 1,
		canceled:  make(chan struct{}),
	}
	conv, rec := openStreamingSpeechConv(t, provider, newScriptedSTT("tell me a story", "that's that"),
		newSegmentTTS(), WithVAD(&startingOnlyVAD{}))

	speakOneUtterance(t, conv)
	require.True(t, provider.waitForTurns(1, 8*time.Second), "the first turn never reached the model")

	speakOneUtterance(t, conv)

	select {
	case <-provider.canceled:
	case <-time.After(8 * time.Second):
		t.Fatal("a turn spoken while the reply is pending must cancel it")
	}
	require.True(t, provider.waitForTurns(2, 8*time.Second), "the short utterance must be the next turn")
	assert.Contains(t, userTextsIn(provider.turnAt(1)), "that's that")
	assert.True(t, waitFor(rec.sawInterrupt, 8*time.Second), "the caller is told to flush playback")
}

// dragonGuard replaces a reply that mentions a dragon with its own message,
// as an enforcing output guardrail does mid-stream.
type dragonGuard struct{}

const dragonGuardMessage = "I can't talk about that."

func (dragonGuard) Name() string { return "dragon-guard" }
func (dragonGuard) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}
func (dragonGuard) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

func (dragonGuard) OnChunk(_ context.Context, chunk *providers.StreamChunk) hooks.Decision {
	if !strings.Contains(chunk.Content, "dragon") {
		return hooks.Allow
	}
	d := hooks.Enforced("dragon", nil)
	text := dragonGuardMessage
	d.Content = &text
	return d
}

// A guardrail that replaces a reply mid-stream stops it being spoken: the
// sentences released before the cut were spoken, the rest never is, and the
// guardrail's message is spoken in its place (#2846). The stored reply is what
// the caller heard: the spoken sentence, then the message.
func TestVADMode_GuardrailReplacementMidReplyIsSpokenInstead(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	ttsSvc := newSegmentTTS()
	provider := &storyProvider{
		turns: [][]string{{"The fox woke early. ", "It saw a dragon. ", "It fled."}},
		gate:  ttsSvc.first, gateAt: 1, gateWait: 2 * time.Second,
	}
	store := statestore.NewMemoryStore()
	conv, rec := openStreamingSpeechConv(t, provider, newScriptedSTT("tell me a story"), ttsSvc,
		WithStateStore(store), WithProviderHook(dragonGuard{}))
	convID := conv.ID()

	speakOneUtterance(t, conv)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, 8*time.Second),
		"the reply never completed")

	assert.Equal(t, []string{"The fox woke early.", dragonGuardMessage}, ttsSvc.texts(),
		"the cleared sentence and then the guardrail's message are spoken; the blocked text never is")
	assert.Equal(t, []int{1, 2}, rec.audioMarkers())

	require.NoError(t, conv.Close())
	stored := storedAssistant(t, store, convID)
	require.Len(t, stored, 1)
	assert.Equal(t, "The fox woke early. "+dragonGuardMessage, stored[0].Content)
}

// Under a judge guardrail that holds the reply by segment (stream_hold:
// segment), a VAD session speaks each segment only once the judge has passed
// it, and a segment the judge fails is never spoken: the guardrail's message
// is, and the stored reply is what the caller heard (#2846, #2829).
func TestVADMode_SegmentHeldReplyIsSpokenOnlyAsItIsReleased(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	judge := newHoldJudge(&holdLog{}, true, false)
	judge.gate = make(chan struct{})
	model := newPacedModel(true, mock.Turn{Type: "text",
		StreamChunks: []string{"The weather is mild today. ", "Buy CompetitorCo shares now. ", "They are cheap."}})
	ttsSvc := newSegmentTTS()
	store := statestore.NewMemoryStore()
	conv, rec := openStreamingSpeechConv(t, model, newScriptedSTT("what is the weather?"), ttsSvc,
		WithStateStore(store), smallSegments(),
		WithGuardrail(guardrails.Output("llm_judge",
			map[string]any{"criteria": "The reply talks only about the weather.", "stream_hold": "segment"},
			guardrails.WithMessage(holdMessage), guardrails.WithJudge(handlers.NewProviderJudge(judge)))))
	t.Cleanup(func() {
		close(model.step)
		close(judge.gate)
	})
	convID := conv.ID()

	speakOneUtterance(t, conv)
	model.next(t)
	assert.Equal(t, "The weather is mild today.", recvWithin(t, judge.started))
	time.Sleep(20 * time.Millisecond) // a held segment must not be spoken in the meantime
	assert.Empty(t, ttsSvc.texts(), "nothing is spoken while the judge holds it")

	judge.gate <- struct{}{}
	recvWithin(t, ttsSvc.first)
	assert.Equal(t, []string{"The weather is mild today."}, ttsSvc.texts(), "the released segment is spoken")

	model.next(t)
	assert.Equal(t, "The weather is mild today. Buy CompetitorCo shares now.", recvWithin(t, judge.started))
	model.next(t) // the reply ends while the segment is judged
	judge.gate <- struct{}{}
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, 8*time.Second),
		"the reply never completed")

	assert.Equal(t, []string{"The weather is mild today.", holdMessage}, ttsSvc.texts(),
		"the failed segment is never spoken; the guardrail's message is")
	assert.Equal(t, []int{1, 2}, rec.audioMarkers())

	require.NoError(t, conv.Close())
	stored := storedAssistant(t, store, convID)
	require.Len(t, stored, 1)
	assert.Equal(t, "The weather is mild today. "+holdMessage, stored[0].Content,
		"the stored reply is what the caller heard")
}
