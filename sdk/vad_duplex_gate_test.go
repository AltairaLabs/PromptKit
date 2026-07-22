package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingTextProvider implements providers.Provider but deliberately NOT
// providers.StreamInputSupport — it is the shape of every text-only LLM
// (Claude, GPT via Chat Completions) that VAD mode exists to serve. It records
// the user text of every PredictStream request so a test can prove the model
// actually received the STT transcript, not merely that construction succeeded.
type recordingTextProvider struct {
	base.Implementation

	mu       sync.Mutex
	received []string
}

func (p *recordingTextProvider) ID() string    { return "recording-text" }
func (p *recordingTextProvider) Model() string { return "recording-text-model" }

func (p *recordingTextProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req)
	return providers.PredictionResponse{Content: "ack"}, nil
}

func (p *recordingTextProvider) PredictStream(
	_ context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	stop := "stop"
	ch := make(chan providers.StreamChunk, 1)
	ch <- providers.StreamChunk{Content: "ack", Delta: "ack", FinishReason: &stop}
	close(ch)
	return ch, nil
}

func (p *recordingTextProvider) record(req providers.PredictionRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, msg := range req.Messages {
		if msg.Role != "user" {
			continue
		}
		// STTStage populates the transcript via AddTextPart (Parts), not the
		// legacy Content field — read both.
		if msg.Content != "" {
			p.received = append(p.received, msg.Content)
		}
		for _, part := range msg.Parts {
			if part.Text != nil && *part.Text != "" {
				p.received = append(p.received, *part.Text)
			}
		}
	}
}

func (p *recordingTextProvider) userTexts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.received))
	copy(out, p.received)
	return out
}

// waitForUserText polls until the provider has seen a user message containing
// want, returning whether it arrived within the timeout.
func (p *recordingTextProvider) waitForUserText(want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, got := range p.userTexts() {
			if got == want {
				return true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func (p *recordingTextProvider) SupportsStreaming() bool      { return true }
func (p *recordingTextProvider) ShouldIncludeRawOutput() bool { return false }
func (p *recordingTextProvider) Close() error                 { return nil }
func (p *recordingTextProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

// TestOpenDuplexVADModeAcceptsNonStreamInputProvider proves the duplex
// construction gate admits a text-only provider when WithVADMode is set. In VAD
// mode the pipeline (AudioTurn → STT → LLM → TTS) handles speech and the model
// only ever sees text, so providers.StreamInputSupport — the ASM interface for
// sending audio to the model — is not required. Requiring it excludes exactly
// the providers VAD mode was built for. See #1637.
func TestOpenDuplexVADModeAcceptsNonStreamInputProvider(t *testing.T) {
	packFile := writeIngestionTestPack(t)

	provider := &recordingTextProvider{}
	if _, ok := providers.Provider(provider).(providers.StreamInputSupport); ok {
		t.Fatal("test premise broken: recordingTextProvider must NOT implement StreamInputSupport")
	}

	conv, err := OpenDuplex(packFile, "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithVADMode(&convMockSTTService{}, newConvMockTTSService(), nil),
	)
	require.NoError(t, err,
		"OpenDuplex must accept a non-StreamInputSupport provider when WithVADMode is set")
	require.NotNil(t, conv)
	assert.Equal(t, DuplexMode, conv.mode)
	assert.NotNil(t, conv.duplexSession,
		"duplex session must build on the VAD path without a stream provider")

	// Close is slow here (~30s) regardless of whether the response channel is
	// drained: an idle VAD duplex session always waits out the full drain
	// timeout. That is #1638, not this gate.
	_ = conv.Close()
}

// pcmSpeech returns d worth of 16-bit mono PCM at sampleRate carrying a
// full-scale tone — loud enough for an energy-based VAD to call it speech.
func pcmSpeech(d time.Duration, sampleRate int) []byte {
	n := int(float64(sampleRate) * d.Seconds())
	buf := make([]byte, n*2)
	for i := 0; i < n; i++ {
		v := int16(12000)
		if i%8 < 4 {
			v = -12000
		}
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}
	return buf
}

// pcmSilence returns d worth of zeroed 16-bit mono PCM at sampleRate.
func pcmSilence(d time.Duration, sampleRate int) []byte {
	return make([]byte, int(float64(sampleRate)*d.Seconds())*2)
}

// TestOpenDuplexVADModeDeliversTranscriptToTextProvider is the behavioral half
// of #1637: admitting a text-only provider through the construction gate is
// only useful if the VAD pipeline then actually drives it. This feeds PCM
// through the real AudioTurn → STT → LLM path and asserts the model received
// the STT transcript as a user message.
//
// The assertion is made after Close only because that is the weakest claim
// that proves the gate: the transcript reached the model at all. Whether it
// arrives per turn *during* the session — it does, since #1644 — is covered by
// TestVADModeFiresLLMDuringSessionNotAtClose in vad_mode_per_turn_test.go.
func TestOpenDuplexVADModeDeliversTranscriptToTextProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn in wall-clock time and closes a duplex session")
	}
	const sampleRate = 16000
	packFile := writeIngestionTestPack(t)
	provider := &recordingTextProvider{}

	conv, err := OpenDuplex(packFile, "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithVADMode(&convMockSTTService{}, newConvMockTTSService(), &VADModeConfig{
			SilenceDuration:   300 * time.Millisecond,
			MinSpeechDuration: 100 * time.Millisecond,
			MaxTurnDuration:   5 * time.Second,
			SampleRate:        sampleRate,
			Language:          "en",
			Voice:             "alloy",
			Speed:             1.0,
		}),
	)
	require.NoError(t, err)

	responseCh, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for range responseCh {
		}
	}()

	ctx := context.Background()
	send := func(pcm []byte) {
		require.NoError(t, conv.SendChunk(ctx, &providers.StreamChunk{
			MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: pcm},
		}))
	}

	// One utterance: speech long enough to open a turn, then silence long
	// enough to close it. convMockSTTService transcribes anything as "hello".
	// Frames are streamed at their real duration because AudioTurnStage
	// measures speech and silence with time.Since and only re-evaluates the
	// turn on element arrival — bulk-sending the same bytes never closes a turn.
	const frame = 20 * time.Millisecond
	for i := 0; i < 30; i++ {
		send(pcmSpeech(frame, sampleRate))
		time.Sleep(frame)
	}
	for i := 0; i < 30; i++ {
		send(pcmSilence(frame, sampleRate))
		time.Sleep(frame)
	}

	// Close both flushes the turn and, today, reports a drain timeout on this
	// path (tracked separately as #1638). The drain outcome is logged rather
	// than asserted: what this test is about is whether the transcript reached
	// the model, which it does regardless.
	if err := conv.Close(); err != nil {
		t.Logf("Close reported: %v (see #1638)", err)
	}
	assert.True(t, provider.waitForUserText("hello", 5*time.Second),
		"model must receive the STT transcript as a user message; got %v", provider.userTexts())
}
