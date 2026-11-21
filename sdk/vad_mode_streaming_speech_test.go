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
	// blockSecond makes the second call wait for its context (a barge-in
	// while the second sentence is synthesized); second closes when it starts.
	blockSecond bool
	second      chan struct{}
	secondOnce  sync.Once

	mu    sync.Mutex
	calls []ttsCall
	first chan struct{}
	once  sync.Once
}

func newSegmentTTS() *segmentTTS {
	return &segmentTTS{convMockTTSService: *newConvMockTTSService(), first: make(chan struct{}),
		second: make(chan struct{})}
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
	if ordinal == 2 {
		s.secondOnce.Do(func() { close(s.second) })
		if s.blockSecond {
			<-ctx.Done()
			return nil, ctx.Err()
		}
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

// streamingSpeechBound bounds every wait in these tests. A passing test never
// waits on it; a broken pipeline that stops reading the caller's audio, never
// ends a turn, or hangs on Close must fail here, not at the test binary's
// timeout.
const streamingSpeechBound = 8 * time.Second

// speak sends one utterance (speakOneUtterance), failing the test if the
// session stops taking the caller's audio.
func streamingSpeak(t *testing.T, conv *Conversation) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		speakOneUtterance(t, conv)
	}()
	select {
	case <-done:
	case <-time.After(streamingSpeechBound):
		t.Fatal("the session stopped taking the caller's audio")
	}
}

// closeWithin closes conv and returns Close's error, failing the test if
// Close does not return.
func closeWithin(t *testing.T, conv *Conversation) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- conv.Close() }()
	select {
	case err := <-done:
		return err
	case <-time.After(streamingSpeechBound):
		t.Error("Close did not return")
		return nil
	}
}

// awaitClosed waits for ch to close, failing the test with what if it has not
// by streamingSpeechBound.
func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(streamingSpeechBound):
		t.Fatal(what)
	}
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
	t.Cleanup(func() { _ = closeWithin(t, conv) })
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

	streamingSpeak(t, conv)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, streamingSpeechBound),
		"the reply never completed")

	calls := ttsSvc.snapshot()
	require.Equal(t, sentences, ttsSvc.texts(), "each sentence is synthesized once, in order")
	assert.True(t, calls[0].at.Before(provider.lastChunkAt()),
		"the first sentence is synthesized before the model finishes the reply (first call %v, reply end %v)",
		calls[0].at, provider.lastChunkAt())
	assert.Equal(t, []int{1, 2, 3}, rec.audioMarkers(), "the caller receives each sentence's audio, in order")

	require.NoError(t, closeWithin(t, conv))
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

	streamingSpeak(t, conv)
	awaitClosed(t, ttsSvc.first, "the first sentence was never synthesized while the reply streamed")

	streamingSpeak(t, conv) // talks over the reply

	awaitClosed(t, provider.canceled, "caller speech must cancel the model call still generating the reply")
	first := ttsSvc.snapshot()[0]
	require.True(t, waitFor(func() bool { return first.ctx.Err() != nil }, streamingSpeechBound),
		"caller speech must cancel the synthesis in flight")
	require.True(t, provider.waitForTurns(2, streamingSpeechBound), "the caller's speech must be the next turn")
	assert.Contains(t, userTextsIn(provider.turnAt(1)), "what is the capital")
	// Once the next turn's reply has ended, everything the stage had of the
	// canceled reply has gone through it: nothing of it can follow.
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 2 }, streamingSpeechBound),
		"the next turn's reply never ended")
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

	streamingSpeak(t, conv)
	require.True(t, provider.waitForTurns(1, streamingSpeechBound), "the first turn never reached the model")

	streamingSpeak(t, conv)

	awaitClosed(t, provider.canceled, "a turn spoken while the reply is pending must cancel it")
	require.True(t, provider.waitForTurns(2, streamingSpeechBound), "the short utterance must be the next turn")
	assert.Contains(t, userTextsIn(provider.turnAt(1)), "that's that")
	assert.True(t, waitFor(rec.sawInterrupt, streamingSpeechBound), "the caller is told to flush playback")
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

	streamingSpeak(t, conv)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, streamingSpeechBound),
		"the reply never completed")

	assert.Equal(t, []string{"The fox woke early.", dragonGuardMessage}, ttsSvc.texts(),
		"the cleared sentence and then the guardrail's message are spoken; the blocked text never is")
	assert.Equal(t, []int{1, 2}, rec.audioMarkers())

	require.NoError(t, closeWithin(t, conv))
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

	streamingSpeak(t, conv)
	model.next(t)
	assert.Equal(t, "The weather is mild today.", recvWithin(t, judge.started))
	assert.Empty(t, ttsSvc.texts(), "nothing is spoken while the judge holds it")

	letThrough(t, judge.gate)
	recvWithin(t, ttsSvc.first)
	assert.Equal(t, []string{"The weather is mild today."}, ttsSvc.texts(), "the released segment is spoken")

	model.next(t)
	assert.Equal(t, "The weather is mild today. Buy CompetitorCo shares now.", recvWithin(t, judge.started))
	model.next(t) // the reply ends while the segment is judged
	letThrough(t, judge.gate)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, streamingSpeechBound),
		"the reply never completed")

	assert.Equal(t, []string{"The weather is mild today.", holdMessage}, ttsSvc.texts(),
		"the failed segment is never spoken; the guardrail's message is")
	assert.Equal(t, []int{1, 2}, rec.audioMarkers())

	require.NoError(t, closeWithin(t, conv))
	stored := storedAssistant(t, store, convID)
	require.Len(t, stored, 1)
	assert.Equal(t, "The weather is mild today. "+holdMessage, stored[0].Content,
		"the stored reply is what the caller heard")
}

// sttCalls is how many turns the scripted STT has transcribed.
func (m *scriptedSTTService) sttCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// blip sends a noise: 60ms loud, then silence long enough to end a turn. The
// VAD (startingOnlyVAD) calls it Starting and never Speaking, as it would a
// cough or a door.
func blip(t *testing.T, conv *Conversation) {
	t.Helper()
	ctx := context.Background()
	send := func(pcm []byte) {
		require.NoError(t, conv.SendChunk(ctx, &providers.StreamChunk{
			MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: pcm},
		}))
	}
	const frame = 20 * time.Millisecond
	for i := 0; i < 3; i++ {
		send(pcmSpeech(frame, perTurnTestSampleRate))
	}
	for i := 0; i < 30; i++ {
		send(pcmSilence(frame, perTurnTestSampleRate))
	}
}

// blipWithin sends a blip, failing the test if the session stops taking audio.
func blipWithin(t *testing.T, conv *Conversation) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		blip(t, conv)
	}()
	awaitClosed(t, done, "the session stopped taking the caller's audio")
}

// Noise while a reply is pending (the model has not answered yet), or while
// it plays, is not a barge-in: it transcribes to nothing, and the reply is
// spoken and stored in full, its model call never canceled.
func TestVADMode_NoiseDuringAReplyLeavesIt(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	for _, tc := range []struct {
		name string
		// noiseAfter is how many sentences are synthesized before the noise.
		noiseAfter int
	}{{"pending", 0}, {"playing", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			ttsSvc := newSegmentTTS()
			release := make(chan struct{})
			provider := &storyProvider{
				turns: [][]string{{"The fox woke early. ", "It ran to the river. ", "Then it slept."}},
				gate:  release, gateAt: tc.noiseAfter, gateWait: streamingSpeechBound,
			}
			sttSvc := newScriptedSTT("tell me a story", "")
			store := statestore.NewMemoryStore()
			conv, rec := openStreamingSpeechConv(t, provider, sttSvc, ttsSvc,
				WithStateStore(store), WithVAD(&startingOnlyVAD{}))
			convID := conv.ID()

			streamingSpeak(t, conv)
			require.True(t, provider.waitForTurns(1, streamingSpeechBound), "the request never reached the model")
			if tc.noiseAfter > 0 {
				awaitClosed(t, ttsSvc.first, "the first sentence was never synthesized")
			}
			blipWithin(t, conv)
			require.True(t, waitFor(func() bool { return sttSvc.sttCalls() >= 2 }, streamingSpeechBound),
				"the noise was never transcribed")
			close(release)

			require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, streamingSpeechBound),
				"the reply never completed")
			assert.False(t, rec.sawInterrupt(), "noise does not interrupt the reply")
			assert.Equal(t, []string{"The fox woke early.", "It ran to the river.", "Then it slept."}, ttsSvc.texts(),
				"the reply is spoken in full")
			assert.Equal(t, 1, provider.turnCount(), "the noise is not a turn the model answers")

			require.NoError(t, closeWithin(t, conv))
			stored := storedAssistant(t, store, convID)
			require.Len(t, stored, 1)
			assert.Equal(t, "The fox woke early. It ran to the river. Then it slept.", stored[0].Content)
			assert.NotEqual(t, types.FinishReasonInterrupted, stored[0].FinishReason, "the reply was not cut")
			assert.NotContains(t, stored[0].Meta, types.MetaPlayoutCut)
		})
	}
}

// A barge-in stores the reply as the caller heard it: the sentence that had
// played, not the sentence being synthesized or the rest the model wrote,
// marked interrupted with how much of its audio was heard, and the model's
// next turn sees that, not what it wrote (#2846). The store is written the
// same way whether or not it also serves as the conversation's message log.
func TestVADMode_BargeInStoresWhatTheCallerHeard(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	for _, tc := range []struct {
		name       string
		messageLog bool
	}{{"state store", false}, {"message log", true}} {
		t.Run(tc.name, func(t *testing.T) {
			ttsSvc := newSegmentTTS()
			ttsSvc.blockSecond = true
			provider := &storyProvider{
				turns:     [][]string{{"The fox woke early. ", "It ran to the river. ", "Then it"}, {"Paris."}},
				holdAfter: 3,
				canceled:  make(chan struct{}),
			}
			store := statestore.NewMemoryStore()
			sttSvc := newScriptedSTT("tell me a story", "what is the capital")
			// Without playback pacing, all audio handed on counts as heard, so
			// what was heard is exact without waiting on a clock: the first
			// sentence was handed on, the second never was (its synthesis is
			// held until the barge-in cancels it). Heard-by-playout is the
			// TTS stage's own tests' to pin, on a manual clock.
			unpaced := vadTestConfig()
			unpaced.NoPlaybackPacing = true
			opts := []Option{WithStateStore(store), WithVADMode(sttSvc, ttsSvc, unpaced)}
			if tc.messageLog {
				opts = append(opts, WithMessageLog(store))
			}
			conv, rec := openStreamingSpeechConv(t, provider, sttSvc, ttsSvc, opts...)
			convID := conv.ID()

			streamingSpeak(t, conv)
			awaitClosed(t, ttsSvc.second, "the second sentence was never synthesized")
			require.True(t, waitFor(func() bool { return len(rec.audioMarkers()) >= 1 }, streamingSpeechBound))

			streamingSpeak(t, conv) // talks over the reply
			awaitClosed(t, provider.canceled, "caller speech must cancel the model call")
			require.True(t, provider.waitForTurns(2, streamingSpeechBound))
			require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 2 }, streamingSpeechBound),
				"the next turn's reply never completed")

			var context []string
			for _, m := range provider.turnAt(1) {
				if m.Role == "assistant" {
					context = append(context, m.Content)
				}
			}
			assert.Equal(t, []string{"The fox woke early."}, context,
				"the model's next turn sees what the caller heard of its reply")

			require.NoError(t, closeWithin(t, conv))
			stored := storedAssistant(t, store, convID)
			require.Len(t, stored, 2)
			cut := stored[0]
			assert.Equal(t, "The fox woke early.", cut.Content, "what the caller heard, and nothing after it")
			assert.Equal(t, types.FinishReasonInterrupted, cut.FinishReason)
			assert.Equal(t, types.PlayoutCutBargeIn, cut.Meta[types.MetaPlayoutCut])
			assert.EqualValues(t, 20, cut.Meta[types.MetaHeardMs], "the first sentence's 20ms")
			assert.Equal(t, "Paris.", stored[1].Content)
		})
	}
}

// A session closed while a reply still plays stores the share of it the
// caller had heard, marked as cut by the session closing, as a realtime
// session with playout tracking does.
func TestVADMode_CloseMidReplyStoresWhatWasHeard(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	const sentence = "One two three four five six seven eight nine ten."
	provider := &storyProvider{turns: [][]string{{"One two three four five ", "six seven eight nine ten."}}}
	store := statestore.NewMemoryStore()
	conv, rec := openStreamingSpeechConv(t, provider, newScriptedSTT("tell me"), newSizedPCMTTS(2*time.Second),
		WithStateStore(store))
	convID := conv.ID()

	streamingSpeak(t, conv)
	require.True(t, waitFor(func() bool { return len(rec.audioMarkers()) >= 1 || rec.turnCompletes() > 0 },
		streamingSpeechBound))
	require.NoError(t, closeWithin(t, conv)) // well inside the reply's 2s

	stored := storedAssistant(t, store, convID)
	require.Len(t, stored, 1, "the reply is stored though the session closed while it played")
	cut := stored[0]
	assert.Equal(t, types.PlayoutCutSessionClosed, cut.Meta[types.MetaPlayoutCut])
	heard, _ := cut.Meta[types.MetaHeardMs].(int64)
	assert.Less(t, heard, int64(2000), "the caller had not heard it all")
	assert.EqualValues(t, 2000, cut.Meta[types.MetaAudioMs])
	assert.True(t, strings.HasPrefix(sentence, cut.Content), "at most what was heard: %q", cut.Content)
	assert.Less(t, len(cut.Content), len(sentence))
}
