package sdk

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// audioRecordingSTT is a scripted STT that also keeps the audio of every call,
// so a test can check what part of an utterance reached transcription.
type audioRecordingSTT struct {
	*scriptedSTTService
	mu    sync.Mutex
	audio [][]byte
}

func newAudioRecordingSTT(transcripts ...string) *audioRecordingSTT {
	return &audioRecordingSTT{scriptedSTTService: newScriptedSTT(transcripts...)}
}

func (s *audioRecordingSTT) Transcribe(ctx context.Context, req base.STTRequest) (base.STTResponse, error) {
	s.mu.Lock()
	s.audio = append(s.audio, append([]byte(nil), req.Audio...))
	s.mu.Unlock()
	return s.scriptedSTTService.Transcribe(ctx, req)
}

func (s *audioRecordingSTT) audioAt(n int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n >= len(s.audio) {
		return nil
	}
	return s.audio[n]
}

// speechSamples counts the samples of pcm loud enough to be pcmSpeech's tone.
func speechSamples(pcm []byte) int {
	n := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int16(binary.LittleEndian.Uint16(pcm[i:]))
		if v > 1000 || v < -1000 {
			n++
		}
	}
	return n
}

// sizedPCMTTS synthesizes every reply as a fixed length of 16kHz PCM silence,
// so a test controls how long the caller would be listening to it.
type sizedPCMTTS struct {
	convMockTTSService
	playback time.Duration
}

func newSizedPCMTTS(playback time.Duration) *sizedPCMTTS {
	return &sizedPCMTTS{convMockTTSService: *newConvMockTTSService(), playback: playback}
}

func (p *sizedPCMTTS) Synthesize(context.Context, string, tts.SynthesisConfig) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(pcmSilence(p.playback, perTurnTestSampleRate))), nil
}

func (p *sizedPCMTTS) SupportedFormats() []tts.AudioFormat {
	pcm := tts.FormatPCM16
	pcm.SampleRate = perTurnTestSampleRate
	return []tts.AudioFormat{pcm}
}

// partialThenHoldProvider starts its first reply and then keeps generating
// until the turn is canceled, like a model mid-way through a long answer.
// Later calls reply at once.
type partialThenHoldProvider struct {
	turnRecordingProvider
	started chan struct{}
	once    sync.Once
}

func (p *partialThenHoldProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	first := false
	p.once.Do(func() { first = true })
	ch := make(chan providers.StreamChunk)
	go func() {
		defer close(ch)
		if first {
			ch <- providers.StreamChunk{Content: "Once upon a time", Delta: "Once upon a time"}
			close(p.started)
			<-ctx.Done()
			return
		}
		stop := "stop"
		ch <- providers.StreamChunk{Content: "Paris.", Delta: "Paris.", FinishReason: &stop}
	}()
	return ch, nil
}

func openBargeInConv(t *testing.T, provider providers.Provider, sttSvc *audioRecordingSTT, ttsSvc tts.Service) (
	*Conversation, *vadContractRecorder,
) {
	t.Helper()
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithVADMode(sttSvc, ttsSvc, vadTestConfig()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	responseCh, err := conv.Response()
	require.NoError(t, err)
	rec := &vadContractRecorder{}
	go rec.consume(responseCh)
	return conv, rec
}

// boundaryBeforeInterrupt reports whether the first turn boundary precedes the
// first Interrupted chunk.
func boundaryBeforeInterrupt(events []vadContractEvent) bool {
	boundaryAt, interruptAt := -1, -1
	for i, ev := range events {
		if ev.turnComplete && boundaryAt < 0 {
			boundaryAt = i
		}
		if ev.interrupted && interruptAt < 0 {
			interruptAt = i
		}
	}
	return boundaryAt >= 0 && boundaryAt < interruptAt
}

// TestVADMode_SpeechDuringGenerationInterrupts covers #2301 (1): the caller
// speaks while the model is still generating the reply. That is a barge-in: the
// reply is cut short and closed, the caller is told to flush, and what they said
// becomes the next turn. It used to be queued and answered only after the whole
// reply had been delivered.
func TestVADMode_SpeechDuringGenerationInterrupts(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	provider := &partialThenHoldProvider{started: make(chan struct{})}
	sttSvc := newAudioRecordingSTT("tell me a long story", "what is the capital of france")
	conv, rec := openBargeInConv(t, provider, sttSvc, newSizedPCMTTS(50*time.Millisecond))

	speakOneUtterance(t, conv)
	select {
	case <-provider.started:
	case <-time.After(8 * time.Second):
		t.Fatal("the model never started replying")
	}

	speakOneUtterance(t, conv) // talks over the reply being generated

	require.True(t, waitFor(rec.sawInterrupt, 8*time.Second),
		"speech during generation must reach the caller as an Interrupted chunk; got %+v", rec.snapshot())
	require.True(t, provider.waitForTurns(2, 8*time.Second),
		"the interrupting speech must be answered as the next turn")
	assert.Contains(t, userTextsIn(provider.turnAt(1)), "what is the capital of france")
	assert.True(t, boundaryBeforeInterrupt(rec.snapshot()),
		"the cut-short reply closes before its Interrupted chunk; got %+v", rec.snapshot())
	// The provider holding turn 2 says nothing about whether its transcript
	// has reached the caller yet: that chunk travels on through the pipeline
	// and the response channel. Wait for it there.
	waitFor(func() bool { return len(rec.transcripts()) >= 2 }, 8*time.Second)
	assert.Equal(t, []string{"tell me a long story", "what is the capital of france"}, rec.transcripts())
}

// TestVADMode_SpeechDuringPlaybackInterrupts covers #2301 (2) and (3): once the
// reply's audio has been delivered the caller is still hearing it, so speaking
// then interrupts it, and the interrupting utterance reaches STT whole, onset
// included.
func TestVADMode_SpeechDuringPlaybackInterrupts(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	provider := &turnRecordingProvider{}
	sttSvc := newAudioRecordingSTT("first question", "second question")
	conv, rec := openBargeInConv(t, provider, sttSvc, newSizedPCMTTS(5*time.Second))

	speakOneUtterance(t, conv)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, 8*time.Second),
		"the first reply never completed; got %+v", rec.snapshot())

	speakOneUtterance(t, conv) // the 5s reply is still playing

	require.True(t, waitFor(rec.sawInterrupt, 8*time.Second),
		"speech while the reply plays must interrupt it; got %+v", rec.snapshot())
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 2 }, 8*time.Second),
		"the interrupting speech must be answered; got %+v", rec.snapshot())
	assert.Equal(t, []string{"first question", "second question"}, rec.transcripts())

	// speakOneUtterance speaks 600ms of tone. All of it must reach STT: the
	// barge-in used to drop the audio buffered up to the moment it fired.
	wantSpeech := int(0.6 * perTurnTestSampleRate)
	assert.Equal(t, wantSpeech, speechSamples(sttSvc.audioAt(1)),
		"the interrupting utterance must reach STT with its onset")
}

// TestVADMode_SpeechAfterPlaybackIsANewTurn is the other side of the playback
// window: once the reply would have finished playing, the caller's next words
// are a normal turn, not a barge-in.
func TestVADMode_SpeechAfterPlaybackIsANewTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	const playback = 100 * time.Millisecond
	provider := &turnRecordingProvider{}
	sttSvc := newAudioRecordingSTT("first question", "second question")
	conv, rec := openBargeInConv(t, provider, sttSvc, newSizedPCMTTS(playback))

	speakOneUtterance(t, conv)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, 8*time.Second),
		"the first reply never completed; got %+v", rec.snapshot())
	time.Sleep(3 * playback) // the reply has finished playing

	speakOneUtterance(t, conv)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 2 }, 8*time.Second),
		"the second turn must be answered; got %+v", rec.snapshot())
	assert.False(t, rec.sawInterrupt(), "speech after playback is not a barge-in; got %+v", rec.snapshot())
	assert.Equal(t, []string{"first question", "second question"}, rec.transcripts())
}

// TestVADMode_EmptyTurnLeavesNothingToInterrupt checks a turn that transcribes
// to nothing (a cough, a noise) does not leave the bot "responding" to it: the
// caller's next words are a normal turn, with no Interrupted chunk.
func TestVADMode_EmptyTurnLeavesNothingToInterrupt(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	provider := &turnRecordingProvider{}
	sttSvc := newAudioRecordingSTT("", "a real question")
	conv, rec := openBargeInConv(t, provider, sttSvc, newSizedPCMTTS(10*time.Millisecond))

	speakOneUtterance(t, conv) // transcribes to nothing: no reply
	require.True(t, waitFor(func() bool { return len(sttSvc.audioAt(0)) > 0 }, 8*time.Second))
	time.Sleep(20 * time.Millisecond) // let the STT stage finish with the empty turn

	speakOneUtterance(t, conv)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, 8*time.Second),
		"the real question must be answered; got %+v", rec.snapshot())
	assert.False(t, rec.sawInterrupt(), "there was no reply to interrupt; got %+v", rec.snapshot())
	assert.Equal(t, []string{"a real question"}, rec.transcripts())
	assert.Equal(t, 1, provider.turnCount())
}
