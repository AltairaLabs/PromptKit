package sdk

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// pricedPCMTTS is a priced TTS service that returns PCM at a set rate. When
// block is set, synthesis waits for its context, so the bot stays "speaking"
// until a barge-in cancels it.
type pricedPCMTTS struct {
	convMockTTSService
	rate    int
	block   bool
	started chan struct{}
	once    sync.Once
}

func newPricedPCMTTS(rate int) *pricedPCMTTS {
	return &pricedPCMTTS{
		convMockTTSService: *newConvMockTTSService(),
		rate:               rate,
		started:            make(chan struct{}),
	}
}

func (p *pricedPCMTTS) Synthesize(ctx context.Context, text string, cfg tts.SynthesisConfig) (io.ReadCloser, error) {
	p.once.Do(func() { close(p.started) })
	if p.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return p.convMockTTSService.Synthesize(ctx, text, cfg)
}

func (p *pricedPCMTTS) SupportedFormats() []tts.AudioFormat {
	pcm := tts.FormatPCM16
	pcm.SampleRate = p.rate
	return []tts.AudioFormat{pcm}
}

func (p *pricedPCMTTS) Pricing() *base.PricingDescriptor {
	return &base.PricingDescriptor{
		Source:   base.PricingSourceInline,
		Currency: "usd",
		Items:    []base.PriceItem{{Unit: "character", Rate: 0.0001}},
	}
}

func (p *pricedPCMTTS) ImplName() string  { return "priced-pcm-tts" }
func (p *pricedPCMTTS) ModelName() string { return "priced-pcm-model" }

func vadTestConfig() *VADModeConfig {
	return &VADModeConfig{
		SilenceDuration:   300 * time.Millisecond,
		MinSpeechDuration: 100 * time.Millisecond,
		MaxTurnDuration:   5 * time.Second,
		SampleRate:        perTurnTestSampleRate,
		Language:          "en",
		Voice:             "alloy",
		Speed:             1.0,
	}
}

// A cascaded voice session without a MessageLog stores the assistant reply,
// priced under Meta["tts_cost"] (#2257), and the audio it delivers is labeled
// with the TTS service's PCM rate (#2265).
func TestVADMode_StoresAssistantMessageWithTTSCost(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	ctx := context.Background()
	store := statestore.NewMemoryStore()
	ttsSvc := newPricedPCMTTS(16000)

	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(&turnRecordingProvider{}),
		WithSkipSchemaValidation(),
		WithStateStore(store),
		WithVADMode(newScriptedSTT("hello"), ttsSvc, vadTestConfig()),
	)
	require.NoError(t, err)
	convID := conv.ID()

	responseCh, err := conv.Response()
	require.NoError(t, err)
	audioRates := make(chan int, 16)
	go func() {
		for chunk := range responseCh {
			if chunk.MediaData != nil {
				audioRates <- chunk.MediaData.SampleRate
			}
		}
	}()

	speakOneUtterance(t, conv)
	select {
	case rate := <-audioRates:
		assert.Equal(t, 16000, rate, "audio is labeled with the service's PCM rate")
	case <-time.After(8 * time.Second):
		t.Fatal("no synthesized audio reached the caller")
	}
	require.NoError(t, conv.Close())

	state, err := store.Load(ctx, convID)
	require.NoError(t, err)
	var assistant int
	for _, m := range state.Messages {
		if m.Role != "assistant" {
			continue
		}
		assistant++
		assert.Equal(t, "ack", m.Content)
		assert.Contains(t, m.Meta, "tts_cost", "the stored reply carries its synthesis cost")
	}
	assert.Equal(t, 1, assistant, "the assistant reply must be stored once")
}

// A barge-in during the bot's reply reaches the duplex caller as a chunk with
// Interrupted set, so it can flush playback (#2264).
func TestVADMode_BargeInReachesCallerAsInterrupted(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	ttsSvc := newPricedPCMTTS(24000)
	ttsSvc.block = true

	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(&turnRecordingProvider{}),
		WithSkipSchemaValidation(),
		WithVADMode(newScriptedSTT("first", "second"), ttsSvc, vadTestConfig()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	responseCh, err := conv.Response()
	require.NoError(t, err)
	interrupted := make(chan providers.StreamChunk, 4)
	go func() {
		for chunk := range responseCh {
			if chunk.Interrupted {
				interrupted <- chunk
			}
		}
	}()

	speakOneUtterance(t, conv)
	select {
	case <-ttsSvc.started:
	case <-time.After(8 * time.Second):
		t.Fatal("the reply never started synthesizing")
	}

	// The caller talks over the bot.
	speakOneUtterance(t, conv)

	select {
	case chunk := <-interrupted:
		assert.Nil(t, chunk.MediaData, "the interrupt chunk carries no audio")
	case <-time.After(8 * time.Second):
		t.Fatal("barge-in never reached the caller as an Interrupted chunk")
	}
}
