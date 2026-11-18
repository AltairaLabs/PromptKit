//go:build integration

package gemini

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestLiveVoice_MetadataVoiceIsSpoken_Live opens Gemini Live sessions with
// voices from metadata["voice"] and checks the voice is the one spoken: Kore
// (a female voice) is pitched clearly above Puck (a male voice, and the
// default). The Live API accepts any voice name at setup, so pitch is the
// observable. With LIVE_VOICE_OUT set to a directory, each reply's PCM
// (24 kHz mono) is written there to listen to.
//
// Run:
//
//	GEMINI_API_KEY=... go test -tags integration ./runtime/providers/gemini/ \
//	    -run TestLiveVoice_MetadataVoiceIsSpoken_Live -v
func TestLiveVoice_MetadataVoiceIsSpoken_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	p := NewProvider("gemini-live", defaultLiveModel, "", providers.ProviderDefaults{}, false)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	speak := func(metadata map[string]interface{}, name string) []byte {
		session, err := p.CreateStreamSession(ctx, &providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio, SampleRate: 16000, Encoding: "pcm16", Channels: 1,
				ChunkSize: 3200, BitDepth: 16,
			},
			Metadata: metadata,
		})
		require.NoError(t, err, name)
		defer session.Close()
		require.NoError(t, session.SendText(ctx, "Say exactly: one moment please, I am checking that for you."))
		var pcm []byte
		for {
			select {
			case c, ok := <-session.Response():
				if !ok {
					return pcm
				}
				require.NoError(t, c.Error)
				if c.MediaData != nil {
					pcm = append(pcm, c.MediaData.Data...)
				}
				if done, _ := c.Metadata["assistant_turn_complete"].(bool); done || c.FinishReason != nil {
					if dir := os.Getenv("LIVE_VOICE_OUT"); dir != "" {
						require.NoError(t, os.WriteFile(filepath.Join(dir, "live-"+name+".pcm"), pcm, 0o600))
					}
					return pcm
				}
			case <-ctx.Done():
				t.Fatalf("%s: no reply", name)
			}
		}
	}
	audio := []string{"AUDIO"}
	kore := speak(map[string]interface{}{"response_modalities": audio, "voice": "Kore"}, "Kore")
	unset := speak(map[string]interface{}{"response_modalities": audio}, "default")
	koreF0, defaultF0 := medianPitchHz(kore), medianPitchHz(unset)
	t.Logf("Kore: %.2fs, median pitch %.0f Hz; default (Puck): %.2fs, median pitch %.0f Hz",
		float64(len(kore))/48000, koreF0, float64(len(unset))/48000, defaultF0)
	assert.Greater(t, len(kore), 24000, "at least half a second of speech")
	assert.Greater(t, koreF0, defaultF0*1.3, "Kore is pitched well above the default male voice")
}

// medianPitchHz estimates the median fundamental frequency of 24 kHz PCM16
// speech by autocorrelation over 40 ms frames with energy, searching 70-400 Hz.
func medianPitchHz(pcm []byte) float64 {
	const rate, frame = 24000, 960
	samples := make([]float64, len(pcm)/2)
	for i := range samples {
		samples[i] = float64(int16(uint16(pcm[2*i]) | uint16(pcm[2*i+1])<<8))
	}
	var pitches []float64
	for start := 0; start+frame <= len(samples); start += frame / 2 {
		f := samples[start : start+frame]
		var energy float64
		for _, s := range f {
			energy += s * s
		}
		if math.Sqrt(energy/frame) < 500 {
			continue
		}
		bestLag, best := 0, 0.0
		for lag := rate / 400; lag <= rate/70; lag++ {
			var r float64
			for i := 0; i+lag < frame; i++ {
				r += f[i] * f[i+lag]
			}
			if r > best {
				best, bestLag = r, lag
			}
		}
		if bestLag > 0 && best > 0.3*energy {
			pitches = append(pitches, float64(rate)/float64(bestLag))
		}
	}
	if len(pitches) == 0 {
		return 0
	}
	sort.Float64s(pitches)
	return pitches[len(pitches)/2]
}
