package sdk

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedSTTService returns a different transcript per call so a test can tell
// one VAD turn from the next. convMockSTTService transcribes everything as
// "hello", which cannot distinguish "fired twice" from "fired once, twice read".
type scriptedSTTService struct {
	mu          sync.Mutex
	transcripts []string
	calls       int
}

func newScriptedSTT(transcripts ...string) *scriptedSTTService {
	return &scriptedSTTService{transcripts: transcripts}
}

func (m *scriptedSTTService) next() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls >= len(m.transcripts) {
		m.calls++
		return "overrun"
	}
	out := m.transcripts[m.calls]
	m.calls++
	return out
}

func (m *scriptedSTTService) Name() string                        { return "scripted-stt" }
func (m *scriptedSTTService) Type() base.ProviderType             { return base.ProviderTypeSTT }
func (m *scriptedSTTService) Pricing() *base.PricingDescriptor    { return nil }
func (m *scriptedSTTService) Validate() error                     { return nil }
func (m *scriptedSTTService) Init(_ context.Context) error        { return nil }
func (m *scriptedSTTService) HealthCheck(_ context.Context) error { return nil }
func (m *scriptedSTTService) Close() error                        { return nil }
func (m *scriptedSTTService) SupportedFormats() []string          { return []string{"pcm"} }

func (m *scriptedSTTService) Transcribe(_ context.Context, _ base.STTRequest) (base.STTResponse, error) {
	return base.STTResponse{Text: m.next()}, nil
}

func (m *scriptedSTTService) TranscribeBytes(
	_ context.Context, _ []byte, _ stt.TranscriptionConfig,
) (string, error) {
	return m.next(), nil
}

// turnRecordingProvider records the full message list of every request, so a
// test can assert both how many times the model fired and what history each
// call carried. recordingTextProvider flattens user text across calls and so
// cannot express either.
type turnRecordingProvider struct {
	base.Implementation

	mu    sync.Mutex
	turns [][]types.Message
}

func (p *turnRecordingProvider) ID() string    { return "turn-recording" }
func (p *turnRecordingProvider) Model() string { return "turn-recording-model" }

func (p *turnRecordingProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req)
	return providers.PredictionResponse{Content: "ack"}, nil
}

func (p *turnRecordingProvider) PredictStream(
	_ context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	stop := "stop"
	ch := make(chan providers.StreamChunk, 1)
	ch <- providers.StreamChunk{Content: "ack", Delta: "ack", FinishReason: &stop}
	close(ch)
	return ch, nil
}

func (p *turnRecordingProvider) record(req providers.PredictionRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	msgs := make([]types.Message, len(req.Messages))
	copy(msgs, req.Messages)
	p.turns = append(p.turns, msgs)
}

func (p *turnRecordingProvider) turnCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.turns)
}

// turnAt returns a copy of the messages the nth (0-based) call carried.
func (p *turnRecordingProvider) turnAt(n int) []types.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n >= len(p.turns) {
		return nil
	}
	out := make([]types.Message, len(p.turns[n]))
	copy(out, p.turns[n])
	return out
}

// waitForTurns polls until the model has fired at least n times.
func (p *turnRecordingProvider) waitForTurns(n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.turnCount() >= n {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func (p *turnRecordingProvider) SupportsStreaming() bool      { return true }
func (p *turnRecordingProvider) ShouldIncludeRawOutput() bool { return false }
func (p *turnRecordingProvider) Close() error                 { return nil }
func (p *turnRecordingProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

// messageText flattens a Message's text across the legacy Content field and Parts.
func messageText(msg types.Message) string {
	if msg.Content != "" {
		return msg.Content
	}
	for _, part := range msg.Parts {
		if part.Text != nil && *part.Text != "" {
			return *part.Text
		}
	}
	return ""
}

// userTextsIn returns the text of every user message in msgs.
func userTextsIn(msgs []types.Message) []string {
	var out []string
	for _, msg := range msgs {
		if msg.Role != "user" {
			continue
		}
		if text := messageText(msg); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// recordingConvTTS records every text the pipeline chose to speak, so a test can
// assert on the words the caller would hear. convMockTTSService discards them.
type recordingConvTTS struct {
	convMockTTSService

	mu     sync.Mutex
	spoken []string
}

func newRecordingConvTTS() *recordingConvTTS {
	return &recordingConvTTS{convMockTTSService: *newConvMockTTSService()}
}

func (r *recordingConvTTS) Synthesize(
	ctx context.Context, text string, cfg tts.SynthesisConfig,
) (io.ReadCloser, error) {
	r.mu.Lock()
	r.spoken = append(r.spoken, text)
	r.mu.Unlock()
	return r.convMockTTSService.Synthesize(ctx, text, cfg)
}

func (r *recordingConvTTS) texts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.spoken))
	copy(out, r.spoken)
	return out
}

// waitForSpoken polls until the pipeline has spoken at least n texts.
func (r *recordingConvTTS) waitForSpoken(n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(r.texts()) >= n {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

const perTurnTestSampleRate = 16000

// openVADModeConv opens a duplex VAD-mode conversation wired to the given STT
// and provider, with turn timings tightened so a test utterance closes quickly.
func openVADModeConv(
	t *testing.T, sttSvc *scriptedSTTService, provider *turnRecordingProvider,
) *Conversation {
	t.Helper()
	return openVADModeConvWithTTS(t, sttSvc, provider, newConvMockTTSService())
}

// openVADModeConvWithTTS is openVADModeConv with the TTS service under the
// caller's control, for tests that assert on what the pipeline speaks.
func openVADModeConvWithTTS(
	t *testing.T, sttSvc *scriptedSTTService, provider *turnRecordingProvider, ttsSvc tts.Service,
) *Conversation {
	t.Helper()
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithVADMode(sttSvc, ttsSvc, &VADModeConfig{
			SilenceDuration:   300 * time.Millisecond,
			MinSpeechDuration: 100 * time.Millisecond,
			MaxTurnDuration:   5 * time.Second,
			SampleRate:        perTurnTestSampleRate,
			Language:          "en",
			Voice:             "alloy",
			Speed:             1.0,
		}),
	)
	require.NoError(t, err)

	responseCh, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for range responseCh { //nolint:revive // drained so the output stage never blocks
		}
	}()
	return conv
}

// speakOneUtterance feeds speech then silence at real time, which is what makes
// AudioTurnStage open and then close exactly one turn. Bulk-sending the same
// bytes never closes a turn — the stage re-evaluates only on element arrival.
func speakOneUtterance(t *testing.T, conv *Conversation) {
	t.Helper()
	ctx := context.Background()
	send := func(pcm []byte) {
		require.NoError(t, conv.SendChunk(ctx, &providers.StreamChunk{
			MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: pcm},
		}))
	}
	const frame = 20 * time.Millisecond
	for i := 0; i < 30; i++ {
		send(pcmSpeech(frame, perTurnTestSampleRate))
		time.Sleep(frame)
	}
	for i := 0; i < 30; i++ {
		send(pcmSilence(frame, perTurnTestSampleRate))
		time.Sleep(frame)
	}
}

// TestVADModeFiresLLMDuringSessionNotAtClose is the core of #1644: a live voice
// conversation must get a reply while the caller is still on the line. VAD mode
// built a non-streaming ProviderStage and never emitted turn boundaries, so the
// model fired once when the input channel closed — a caller heard nothing until
// they hung up.
func TestVADModeFiresLLMDuringSessionNotAtClose(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn in wall-clock time")
	}
	sttSvc := newScriptedSTT("what is the capital of france")
	provider := &turnRecordingProvider{}
	conv := openVADModeConv(t, sttSvc, provider)
	t.Cleanup(func() { _ = conv.Close() })

	speakOneUtterance(t, conv)

	require.True(t, provider.waitForTurns(1, 8*time.Second),
		"model must fire during the session, before Close; fired %d times", provider.turnCount())
	assert.Contains(t, userTextsIn(provider.turnAt(0)), "what is the capital of france",
		"the turn the model fired on must carry that turn's transcript")
}

// TestVADModeSpeaksTheReplyNotTheCallersOwnWords guards the composition that
// the per-turn fix created. The continuous multi-turn ProviderStage re-emits
// each turn's user transcript downstream before generating, and in VAD mode TTS
// is what sits downstream — so an unfiltered speech stage reads the caller's
// own question back to them before answering it. Caught against real providers,
// pinned here because CI has no API keys.
func TestVADModeSpeaksTheReplyNotTheCallersOwnWords(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn in wall-clock time")
	}
	const transcript = "what is the capital of france"
	sttSvc := newScriptedSTT(transcript)
	provider := &turnRecordingProvider{}
	ttsSvc := newRecordingConvTTS()
	conv := openVADModeConvWithTTS(t, sttSvc, provider, ttsSvc)
	t.Cleanup(func() { _ = conv.Close() })

	speakOneUtterance(t, conv)
	require.True(t, ttsSvc.waitForSpoken(1, 8*time.Second),
		"the assistant must speak something during the session")

	spoken := ttsSvc.texts()
	assert.NotContains(t, spoken, transcript,
		"the caller's own transcript must never be spoken back at them")
	assert.Equal(t, []string{"ack"}, spoken,
		"exactly the assistant's reply is spoken, once — not the streamed deltas as well")
}

// TestVADModeThreadsHistoryAcrossTurns proves the fix is the continuous
// multi-turn loop and not merely an earlier single firing: a second utterance
// fires the model again, and that call still carries the first exchange.
func TestVADModeThreadsHistoryAcrossTurns(t *testing.T) {
	if testing.Short() {
		t.Skip("drives two real VAD turns in wall-clock time")
	}
	sttSvc := newScriptedSTT("first question", "second question")
	provider := &turnRecordingProvider{}
	conv := openVADModeConv(t, sttSvc, provider)
	t.Cleanup(func() { _ = conv.Close() })

	speakOneUtterance(t, conv)
	require.True(t, provider.waitForTurns(1, 8*time.Second), "first utterance must fire the model")

	speakOneUtterance(t, conv)
	require.True(t, provider.waitForTurns(2, 8*time.Second),
		"second utterance must fire the model again; fired %d times", provider.turnCount())

	second := provider.turnAt(1)
	assert.Contains(t, userTextsIn(second), "second question",
		"the second call must carry the second transcript")
	assert.Contains(t, userTextsIn(second), "first question",
		"the second call must still carry the first turn's history")
}
