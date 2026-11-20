package stage

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
)

// The earcon is a short neutral tone: a sine with a linear attack and release,
// so it starts and ends at silence without a click.
const (
	earconDuration  = 200 * time.Millisecond
	earconAttack    = 10 * time.Millisecond
	earconRelease   = 40 * time.Millisecond
	earconFrequency = 660.0  // Hz
	earconAmplitude = 8000.0 // about -12 dBFS
)

// earconPCM is the earcon, the short tone a cue stage plays for a cue no other
// source voices, as PCM16 mono little-endian at rate. It depends on rate
// only, so every session at a rate plays the same bytes.
func earconPCM(rate int) []byte {
	n := int(int64(rate) * int64(earconDuration) / int64(time.Second))
	attack := float64(int64(rate) * int64(earconAttack) / int64(time.Second))
	release := float64(int64(rate) * int64(earconRelease) / int64(time.Second))
	out := make([]byte, bytesPerSamplePCM16*n)
	for i := 0; i < n; i++ {
		env := 1.0
		if pos := float64(i); pos < attack {
			env = pos / attack
		} else if left := float64(n - 1 - i); left < release {
			env = left / release
		}
		v := math.Round(earconAmplitude * env * math.Sin(2*math.Pi*earconFrequency*float64(i)/float64(rate)))
		binary.LittleEndian.PutUint16(out[bytesPerSamplePCM16*i:], uint16(int16(v))) //nolint:gosec // |v| <= amplitude
	}
	return out
}

// earconClip is the earcon as a clip ready to play at rate.
func earconClip(rate int) cueClip {
	pcm := earconPCM(rate)
	return cueClip{Audio: &AudioData{Samples: pcm, SampleRate: rate, Channels: 1, Format: AudioFormatPCM16,
		Duration: audio.PCM16Duration(len(pcm), rate, 1)}}
}
