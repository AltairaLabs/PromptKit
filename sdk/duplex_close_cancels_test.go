package sdk

import (
	"context"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// closeBound is how long canceling the context given to Start may take to
// stop a session whose model or TTS call is still in flight (#2847). The
// session used to wait for that call, then for the pipeline idle timeout.
const closeBound = 300 * time.Millisecond

// blockingCall records that a provider call started and how it ended: it
// blocks until its context is canceled.
type blockingCall struct {
	entered  chan struct{}
	canceled chan error
}

func newBlockingCall() *blockingCall {
	return &blockingCall{entered: make(chan struct{}, 16), canceled: make(chan error, 16)}
}

func (b *blockingCall) block(ctx context.Context) error {
	b.entered <- struct{}{}
	<-ctx.Done()
	b.canceled <- ctx.Err()
	return ctx.Err()
}

// waitEntered waits for the blocking call to start.
func (b *blockingCall) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-b.entered:
	case <-time.After(8 * time.Second):
		t.Fatal("the blocking call never started")
	}
}

// requireCanceledBy asserts the blocked call returned on its context's
// cancellation before deadline.
func (b *blockingCall) requireCanceledBy(t *testing.T, deadline time.Time) {
	t.Helper()
	select {
	case got := <-b.canceled:
		assert.ErrorIs(t, got, context.Canceled, "the blocked call must see its context canceled")
	case <-time.After(time.Until(deadline)):
		t.Fatal("the blocked call did not observe cancellation")
	}
}

// blockingLLM is a text provider whose every call blocks until canceled.
type blockingLLM struct {
	turnRecordingProvider
	call *blockingCall
}

func (p *blockingLLM) Predict(ctx context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, p.call.block(ctx)
}

func (p *blockingLLM) PredictStream(ctx context.Context, _ providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, p.call.block(ctx)
}

// blockingTTS is a TTS service whose every synthesis blocks until canceled.
type blockingTTS struct {
	convMockTTSService
	call *blockingCall
}

func (b *blockingTTS) Synthesize(ctx context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
	return nil, b.call.block(ctx)
}

// liveMic is a microphone that, like a real one, keeps capturing: one
// utterance, then silence until the test ends. Its frames back up into the
// session's input while the model or TTS call blocks the pipeline.
type liveMic struct {
	frames   chan audio.MediaFrame
	stop     chan struct{}
	stopOnce sync.Once
}

func newLiveMic() *liveMic {
	m := &liveMic{frames: make(chan audio.MediaFrame), stop: make(chan struct{})}
	format := audio.Format{SampleRate: perTurnTestSampleRate, Channels: 1}
	const frame = 20 * time.Millisecond
	go func() {
		defer close(m.frames)
		for i := 0; ; i++ {
			pcm := pcmSilence(frame, perTurnTestSampleRate)
			if i < 30 {
				pcm = pcmSpeech(frame, perTurnTestSampleRate)
			}
			select {
			case m.frames <- audio.MediaFrame{Kind: audio.KindAudio, Data: pcm, Format: format}:
			case <-m.stop:
				return
			}
		}
	}()
	return m
}

func (m *liveMic) Frames() <-chan audio.MediaFrame { return m.frames }
func (m *liveMic) Kind() audio.MediaKind           { return audio.KindAudio }
func (m *liveMic) Close() error                    { return nil }

// unplug stops the microphone's capture goroutine.
func (m *liveMic) unplug() { m.stopOnce.Do(func() { close(m.stop) }) }

// vadBlockedOn opens the model and TTS for a VAD session, one of which blocks.
func vadBlockedOn(blockTTS bool) (providers.Provider, tts.Service, *blockingCall) {
	call := newBlockingCall()
	if blockTTS {
		return &turnRecordingProvider{}, &blockingTTS{convMockTTSService: *newConvMockTTSService(), call: call}, call
	}
	return &blockingLLM{call: call}, newConvMockTTSService(), call
}

func vadModeTestConfig() *VADModeConfig {
	return &VADModeConfig{
		SilenceDuration:   300 * time.Millisecond,
		MinSpeechDuration: 100 * time.Millisecond,
		MaxTurnDuration:   5 * time.Second,
		SampleRate:        perTurnTestSampleRate,
	}
}

// goroutineSet snapshots the live goroutines, by ID, with their stacks.
func goroutineSet() map[string]string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	out := map[string]string{}
	for _, g := range strings.Split(string(buf), "\n\n") {
		header, _, _ := strings.Cut(g, "\n")
		id, _, _ := strings.Cut(strings.TrimPrefix(header, "goroutine "), " ")
		out[id] = g
	}
	return out
}

// assertNoGoroutineLeak waits briefly for every goroutine started since
// before was taken to exit, and fails listing those still running.
func assertNoGoroutineLeak(t *testing.T, before map[string]string) {
	t.Helper()
	var leaked []string
	deadline := time.Now().Add(time.Second)
	for {
		leaked = leaked[:0]
		for id, stack := range goroutineSet() {
			if _, ok := before[id]; !ok && !strings.Contains(stack, "goroutineSet") {
				leaked = append(leaked, stack)
			}
		}
		if len(leaked) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Empty(t, leaked, "goroutines the session started are still running")
}

// voiceUnderTest is a voice conversation, its microphone, and a probe that
// waits for the work in flight to be canceled.
type voiceUnderTest struct {
	conv     *Conversation
	mic      *liveMic
	startErr chan error // what Start returned
	// requireCanceledBy asserts the in-flight work saw the cancellation
	// before deadline.
	requireCanceledBy func(t *testing.T, deadline time.Time)
}

// voiceScenario opens a voice conversation and returns once work is in
// flight that only cancellation ends: a model or TTS call that blocks, or a
// realtime reply the provider never finishes.
type voiceScenario struct {
	name  string
	start func(t *testing.T, ctx context.Context) voiceUnderTest
}

func vadScenario(name string, blockTTS bool) voiceScenario {
	return voiceScenario{name: name, start: func(t *testing.T, ctx context.Context) voiceUnderTest {
		provider, ttsSvc, call := vadBlockedOn(blockTTS)
		v := openVoiceUnderTest(t, ctx,
			WithProvider(provider),
			WithVADMode(newScriptedSTT("tell me a long story"), ttsSvc, vadModeTestConfig()))
		call.waitEntered(t)
		v.requireCanceledBy = call.requireCanceledBy
		return v
	}}
}

// asmScenario is a realtime session whose provider is mid-reply: it has sent
// part of a reply and will send no more until the session is closed.
func asmScenario() voiceScenario {
	return voiceScenario{name: "ASM", start: func(t *testing.T, ctx context.Context) voiceUnderTest {
		prov := mock.NewStreamingProvider("rt", "rt-model", false)
		v := openVoiceUnderTest(t, ctx, WithProvider(prov), WithStreamingConfig(convDuplexAudio))
		require.Eventually(t, func() bool { return prov.GetSession() != nil }, 5*time.Second, time.Millisecond)
		sess := prov.GetSession()
		sess.EmitChunk(&providers.StreamChunk{Content: "once upon", Delta: "once upon"})
		v.requireCanceledBy = func(t *testing.T, deadline time.Time) {
			t.Helper()
			requireClosedBy(t, sess.Done(), deadline, "the provider session")
		}
		return v
	}}
}

// openVoiceUnderTest opens a voice conversation on a live microphone and
// runs Start(ctx) in the background.
func openVoiceUnderTest(t *testing.T, ctx context.Context, opts ...Option) voiceUnderTest {
	t.Helper()
	mic := newLiveMic()
	t.Cleanup(mic.unplug)
	conv, err := OpenVoice(writeIngestionTestPack(t), "main", append(opts,
		WithSkipSchemaValidation(),
		WithAudioSession(&fakeAudioSession{
			sources: []audio.Source{mic},
			sinks:   []audio.Sink{audio.NewMemSink(audio.KindAudio)},
		}))...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	startErr := make(chan error, 1)
	go func() { startErr <- conv.Start(ctx) }()
	return voiceUnderTest{conv: conv, mic: mic, startErr: startErr}
}

func voiceScenarios() []voiceScenario {
	return []voiceScenario{vadScenario("VAD/LLM", false), vadScenario("VAD/TTS", true), asmScenario()}
}

// requireClosedBy asserts ch closes before deadline.
func requireClosedBy(t *testing.T, ch <-chan struct{}, deadline time.Time, what string) {
	t.Helper()
	select {
	case <-ch:
		return
	default:
	}
	select {
	case <-ch:
	case <-time.After(time.Until(deadline)):
		t.Fatalf("%s still open after the session was stopped", what)
	}
}

// Canceling the context given to Start tears the session down on its own,
// as the showcase's Ctrl+C does: the work in flight is canceled and Done
// closes, before any Close. Close then returns at once.
func TestVoice_StartContextCancelTearsDown(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real voice turn")
	}
	for _, sc := range voiceScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			before := goroutineSet()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			v := sc.start(t, ctx)
			done, err := v.conv.Done()
			require.NoError(t, err)

			start := time.Now()
			cancel()
			select {
			case err := <-v.startErr:
				assert.NoError(t, err, "a context-cancel shutdown returns nil")
			case <-time.After(closeBound):
				t.Fatal("Start did not return after its context was canceled")
			}
			requireClosedBy(t, done, start.Add(closeBound), "Done()")
			v.requireCanceledBy(t, start.Add(closeBound))

			closeStart := time.Now()
			require.NoError(t, v.conv.Close())
			assert.Less(t, time.Since(closeStart), closeBound, "Close after a canceled Start took too long")
			v.mic.unplug()
			assertNoGoroutineLeak(t, before)
		})
	}
}

// longReplyLLM streams a long reply, one word per chunk: more chunks than
// the session's Response() buffer holds.
type longReplyLLM struct {
	turnRecordingProvider
	words int
}

func (p *longReplyLLM) PredictStream(
	_ context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	ch := make(chan providers.StreamChunk)
	go func() {
		defer close(ch)
		content := ""
		for i := 0; i < p.words; i++ {
			content += "word "
			ch <- providers.StreamChunk{Content: content, Delta: "word "}
		}
		stop := "stop"
		ch <- providers.StreamChunk{Content: content, FinishReason: &stop}
	}()
	return ch, nil
}

// Close on a VAD session whose caller took Response() and stopped reading
// it, with a long reply under way and the microphone's input backed up
// behind it, finishes the turn, saves it, and returns. It used to wait out
// the 30s drain timeout: the unread output blocked the pipeline, the pipeline
// left its input unread, and the drain signal waited on that input (#2847).
func TestDuplexClose_FinishesABusyTurnWhoseReaderHasStopped(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	const words = 300
	store := statestore.NewMemoryStore()
	provider := &longReplyLLM{words: words}
	// The reply's speech: more chunks than the Response() buffer holds.
	speech := newConvMockStreamingTTSService()
	speech.streamChunks = make([]audio.Chunk, 3*words)
	for i := range speech.streamChunks {
		speech.streamChunks[i] = audio.Chunk{Data: pcmSilence(20*time.Millisecond, perTurnTestSampleRate), Index: i}
	}
	speech.streamChunks[len(speech.streamChunks)-1].Final = true
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(provider), WithSkipSchemaValidation(),
		WithVADMode(newScriptedSTT("tell me a long story"), speech, vadModeTestConfig()),
		WithStateStore(store), WithConversationID("busy-close"))
	require.NoError(t, err)
	respCh, err := conv.Response() // taken; the caller stops reading it
	require.NoError(t, err)

	speakOneUtterance(t, conv)
	require.True(t, provider.waitForTurns(1, 8*time.Second), "the utterance must start a turn")
	require.Eventually(t, func() bool { return len(respCh) == cap(respCh) }, 5*time.Second, time.Millisecond,
		"the reply fills the unread Response() and stalls the pipeline")

	start := time.Now()
	require.NoError(t, conv.Close())
	assert.Less(t, time.Since(start), drainBound, "Close waited on a reader that had stopped reading")
	assert.Equal(t, map[int]string{
		0: "user:tell me a long story",
		1: "assistant:" + strings.Repeat("word ", words),
	}, persistedTranscript(t, store, "busy-close"), "the turn in flight is finished and saved")
}

// drainBound is well under the drain timeout (30s) Close used to wait out.
const drainBound = 2 * time.Second
