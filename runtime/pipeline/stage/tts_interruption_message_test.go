package stage_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pricedStreamingTTS is a tts.Service with a pricing descriptor and a
// configurable PCM rate, for the interruptible stage's cost and rate paths.
type pricedStreamingTTS struct {
	mockTTSService
	pcmRate int
}

func (p *pricedStreamingTTS) SupportedFormats() []tts.AudioFormat {
	pcm := tts.FormatPCM16
	pcm.SampleRate = p.pcmRate
	return []tts.AudioFormat{tts.FormatMP3, pcm}
}

func (p *pricedStreamingTTS) Pricing() *base.PricingDescriptor {
	return &base.PricingDescriptor{
		Source:   base.PricingSourceInline,
		Currency: "usd",
		Items:    []base.PriceItem{{Unit: "character", Rate: 0.0001}},
	}
}

func (p *pricedStreamingTTS) ImplName() string  { return "priced-tts" }
func (p *pricedStreamingTTS) ModelName() string { return "priced-model" }

func audioElements(results []stage.StreamElement) []stage.StreamElement {
	var out []stage.StreamElement
	for _, r := range results {
		if r.Audio != nil {
			out = append(out, r)
		}
	}
	return out
}

// The assistant message reaches downstream stages on the audio element, with
// the synthesis cost stamped on it (#2257).
func TestTTSStageWithInterruption_KeepsMessageAndStampsCost(t *testing.T) {
	svc := &pricedStreamingTTS{pcmRate: 24000}
	s := stage.NewTTSStageWithInterruption(svc, stage.DefaultTTSStageWithInterruptionConfig())

	msg := &types.Message{Role: "assistant", Content: "hello world", Meta: map[string]interface{}{"k": "v"}}
	results := runStage(t, s, []stage.StreamElement{stage.NewMessageElement(msg)}, 2*time.Second)

	require.Len(t, results, 1)
	out := results[0]
	require.NotNil(t, out.Audio)
	require.NotNil(t, out.Message, "the assistant message must survive synthesis")
	assert.Equal(t, "hello world", out.Message.Content)
	assert.Equal(t, "v", out.Message.Meta["k"], "existing metadata is kept")

	costMap, ok := out.Message.Meta["tts_cost"].(map[string]any)
	require.True(t, ok, "tts_cost must be stamped, got %v", out.Message.Meta)
	assert.InDelta(t, 11*0.0001, costMap["total_cost_usd"], 1e-9)

	_, leaked := msg.Meta["tts_cost"]
	assert.False(t, leaked, "the stamp goes on a copy, not the provider's message")
}

func TestTTSStageWithInterruption_UnpricedServiceKeepsMessageWithoutCost(t *testing.T) {
	s := stage.NewTTSStageWithInterruption(&mockTTSService{}, stage.DefaultTTSStageWithInterruptionConfig())

	msg := &types.Message{Role: "assistant", Content: "hello"}
	results := runStage(t, s, []stage.StreamElement{stage.NewMessageElement(msg)}, 2*time.Second)

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Message)
	assert.Same(t, msg, results[0].Message)
	assert.Nil(t, results[0].Message.Meta)
}

// A barge-in that cancels synthesis drops the audio but not the message, and
// prices nothing.
func TestTTSStageWithInterruption_CanceledSynthesisForwardsMessage(t *testing.T) {
	started := make(chan struct{})
	svc := &pricedStreamingTTS{pcmRate: 24000}
	svc.synthesizeFunc = func(ctx context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s := stage.NewTTSStageWithInterruption(svc, stage.DefaultTTSStageWithInterruptionConfig())

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 16)
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), input, output) }()

	input <- stage.NewMessageElement(&types.Message{Role: "assistant", Content: "a long reply"})
	<-started
	input <- stage.NewInterruptElement()
	close(input)
	require.NoError(t, <-done)

	var messages []*types.Message
	var audio, interrupts int
	for e := range output {
		if e.Message != nil {
			messages = append(messages, e.Message)
		}
		if e.Audio != nil {
			audio++
		}
		if e.Interrupt {
			interrupts++
		}
	}
	assert.Equal(t, 0, audio)
	assert.Equal(t, 1, interrupts)
	require.Len(t, messages, 1, "the canceled reply's message is still forwarded")
	assert.NotContains(t, messages[0].Meta, "tts_cost")
}

func TestTTSStageWithInterruption_SkippedTextForwardsMessage(t *testing.T) {
	svc := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			t.Error("whitespace must not be synthesized")
			return io.NopCloser(strings.NewReader("")), nil
		},
	}
	s := stage.NewTTSStageWithInterruption(svc, stage.DefaultTTSStageWithInterruptionConfig())

	msg := &types.Message{Role: "assistant", Content: "   "}
	results := runStage(t, s, []stage.StreamElement{stage.NewMessageElement(msg), makeTextElement(" ")}, 2*time.Second)

	require.Len(t, results, 1, "the message is kept; the bare whitespace Text is dropped")
	assert.Same(t, msg, results[0].Message)
	assert.Nil(t, results[0].Audio)
}

// The audio is labeled with the rate the service synthesizes PCM at, not a
// constant (#2265).
func TestTTSStageWithInterruption_LabelsServicePCMRate(t *testing.T) {
	svc := &pricedStreamingTTS{pcmRate: 16000}
	s := stage.NewTTSStageWithInterruption(svc, stage.DefaultTTSStageWithInterruptionConfig())

	results := audioElements(runStage(t, s, []stage.StreamElement{makeTextElement("hi")}, 2*time.Second))

	require.Len(t, results, 1)
	assert.Equal(t, 16000, results[0].Audio.SampleRate)
}

func TestTTSStageWithInterruption_OutputSampleRateResamples(t *testing.T) {
	pcm := generateTestPCMAudio(4800) // 2400 samples at 24 kHz = 100ms
	svc := &pricedStreamingTTS{pcmRate: 24000}
	svc.synthesizeFunc = func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(pcm))), nil
	}
	cfg := stage.DefaultTTSStageWithInterruptionConfig()
	cfg.OutputSampleRate = 16000
	s := stage.NewTTSStageWithInterruption(svc, cfg)

	results := audioElements(runStage(t, s, []stage.StreamElement{makeTextElement("hi")}, 2*time.Second))

	require.Len(t, results, 1)
	assert.Equal(t, 16000, results[0].Audio.SampleRate)
	assert.Equal(t, 3200, len(results[0].Audio.Samples), "100ms at 16 kHz is 1600 samples")
}

func TestTTSStageWithInterruption_OutputSampleRateMatchingServiceIsUnchanged(t *testing.T) {
	pcm := generateTestPCMAudio(480)
	svc := &pricedStreamingTTS{pcmRate: 16000}
	svc.synthesizeFunc = func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(pcm))), nil
	}
	cfg := stage.DefaultTTSStageWithInterruptionConfig()
	cfg.OutputSampleRate = 16000
	s := stage.NewTTSStageWithInterruption(svc, cfg)

	results := audioElements(runStage(t, s, []stage.StreamElement{makeTextElement("hi")}, 2*time.Second))

	require.Len(t, results, 1)
	assert.Equal(t, 16000, results[0].Audio.SampleRate)
	assert.Equal(t, pcm, results[0].Audio.Samples)
}

// A synthesis failure forwards the element exactly once, with the error
// attached, so the save stage does not store the assistant reply twice.
func TestTTSStageWithInterruption_SynthesisErrorForwardsMessageOnce(t *testing.T) {
	synthErr := errors.New("synthesis exploded")
	svc := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			return nil, synthErr
		},
	}
	cfg := stage.DefaultTTSStageWithInterruptionConfig()
	cfg.Retry = tts.RetryConfig{MaxAttempts: 1}
	s := stage.NewTTSStageWithInterruption(svc, cfg)

	msg := &types.Message{Role: "assistant", Content: "hello"}
	results := runStage(t, s, []stage.StreamElement{stage.NewMessageElement(msg)}, 2*time.Second)

	require.Len(t, results, 1, "the failed element must be forwarded exactly once")
	assert.Same(t, msg, results[0].Message)
	assert.ErrorIs(t, results[0].Error, synthErr)
	assert.Nil(t, results[0].Audio)
}
