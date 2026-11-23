package cue

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
)

// BankOf is the bank the stage plays from: a clip the bank is given plays.
func TestBankOf_IsTheBankTheStagePlaysFrom(t *testing.T) {
	cue := NewStage(Config{})
	bank := BankOf(cue)
	require.Same(t, cue.bank, bank)

	pcm := cueRamp(2400)
	SetRender(cue, func(context.Context, string) (Clip, error) { return pcmClip(pcm, cueTestRate), nil })
	awaitSeamRender(t, bank, cue, brunoText)
	clip, isReady, ok := Held(bank, brunoText)
	require.True(t, ok)
	assert.False(t, isReady, "a clip from the render seam is prepared before it plays")
	assert.Equal(t, pcm, clip.Audio.Samples)
	assert.Equal(t, cueFaded(pcm, cueTestRate), playedFor(t, cue, brunoText))
}

// awaitSeamRender renders text through cue's render seam and waits for it.
func awaitSeamRender(t *testing.T, b *Bank, cue *CueStage, text string) {
	t.Helper()
	select {
	case <-b.render(context.Background(), text, cue.render).done:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "the render never finished")
	}
}

// HoldBudget is the stage's configured OnDemandBudget, its default when it
// configured none, and the default with no cue stage at all.
func TestHoldBudget(t *testing.T) {
	assert.Equal(t, 20*time.Millisecond, HoldBudget(BankOf(NewStage(Config{OnDemandBudget: 20 * time.Millisecond}))))
	assert.Equal(t, 1500*time.Millisecond, HoldBudget(BankOf(NewStage(Config{}))))
	assert.Equal(t, 1500*time.Millisecond, HoldBudget(nil))
	assert.Equal(t, 1500*time.Millisecond, HoldBudget(&Bank{}), "a bank with no budget")
}

// SetRender replaces the session's sources: a cued text renders with it.
func TestSetRender_RendersACuedText(t *testing.T) {
	cue := NewStage(Config{})
	var asked []string
	pcm := cueRamp(2400)
	SetRender(cue, func(_ context.Context, text string) (Clip, error) {
		asked = append(asked, text)
		return pcmClip(pcm, cueTestRate), nil
	})
	assert.Equal(t, cueFaded(pcm, cueTestRate), playedFor(t, cue, brunoText))
	assert.Equal(t, []string{brunoText}, asked)
}

// OnStored is called with each clip a render produced, once the bank holds it.
func TestOnStored_ReportsEachRenderedClip(t *testing.T) {
	cue := NewStage(Config{})
	pcm := cueRamp(2400)
	SetRender(cue, func(context.Context, string) (Clip, error) { return pcmClip(pcm, cueTestRate), nil })
	type stored struct {
		text string
		pcm  []byte
		held bool
	}
	ch := make(chan stored, 1)
	OnStored(BankOf(cue), func(text string, clip Clip) {
		_, _, held := Held(BankOf(cue), text)
		ch <- stored{text, clip.Audio.Samples, held}
	})
	playedFor(t, cue, brunoText)
	select {
	case got := <-ch:
		assert.Equal(t, stored{brunoText, pcm, true}, got)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "the clip was never reported")
	}
}

// Held is a configured clip as given (not ready), a clip from the sources
// ready to play, and nothing for a text the bank does not hold.
func TestHeld(t *testing.T) {
	given := pcmClip(cueRamp(2400), cueTestRate)
	bank := BankOf(NewStage(Config{Clips: map[string]Clip{brunoText: given}}))
	clip, isReady, ok := Held(bank, brunoText)
	assert.Equal(t, given, clip)
	assert.False(t, isReady)
	assert.True(t, ok)

	_, isReady, ok = Held(bank, "Unknown.")
	assert.False(t, isReady)
	assert.False(t, ok)

	spoken := cueStageForSession(t, Config{Cache: cueclip.NewMemoryCache(1 << 20)}, spokenSession("alloy", 24000))
	AwaitRender(context.Background(), spoken.bank, "Earcon.")
	clip, isReady, ok = Held(spoken.bank, "Earcon.")
	assert.Equal(t, earconPCM(24000), clip.Audio.Samples)
	assert.True(t, isReady)
	assert.True(t, ok)
}

// AwaitRender returns once the text's clip from the sources is done; a text
// whose render failed is held by nothing.
func TestAwaitRender_WaitsForTheSources(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	svc.gate = make(chan struct{})
	cue := cueStageForSession(t, Config{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)},
		spokenSession("alloy", 24000))
	done := make(chan struct{})
	go func() {
		AwaitRender(context.Background(), cue.bank, "Awaited.")
		close(done)
	}()
	require.Eventually(t, func() bool { return svc.calls("Awaited.") == 1 }, 3*time.Second, time.Millisecond)
	select {
	case <-done:
		require.FailNow(t, "AwaitRender returned while the render still ran")
	default:
	}
	close(svc.gate)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "AwaitRender never returned")
	}
	clip, isReady, ok := Held(cue.bank, "Awaited.")
	require.True(t, ok)
	assert.True(t, isReady)
	assert.Equal(t, cueFaded(pcm, 24000), clip.Audio.Samples)
}

// EarconPCM is the earcon the stage plays at that rate.
func TestEarconPCMHook(t *testing.T) {
	assert.Equal(t, earconPCM(16000), EarconPCM(16000))
	assert.Len(t, EarconPCM(16000), 2*3200, "200 ms at 16 kHz")
}

// CacheOf is the cache the stage was configured with, nil for the default.
func TestCacheOf(t *testing.T) {
	cache := cueclip.NewMemoryCache(1 << 20)
	assert.Same(t, cache, CacheOf(NewStage(Config{Cache: cache})))
	assert.Nil(t, CacheOf(NewStage(Config{})))
}
