package stage

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pcmSample(b []byte, i int) int {
	return int(int16(binary.LittleEndian.Uint16(b[2*i:]))) //nolint:gosec // PCM16 wire bytes
}

// The earcon is a 200 ms 660 Hz tone at the rate asked for, rising from
// silence over 10 ms and falling back to it over the last 40 ms.
func TestEarconPCM(t *testing.T) {
	for _, rate := range []int{16000, 24000} {
		pcm := EarconPCM(rate)
		n := rate / 5
		require.Len(t, pcm, 2*n, "200 ms of PCM16 mono")
		assert.Equal(t, 0, pcmSample(pcm, 0), "it starts at silence")
		assert.Equal(t, 0, pcmSample(pcm, n-1), "it ends at silence")

		// Between attack and release it is the plain tone.
		for _, i := range []int{rate / 100 * 3, rate / 10, rate / 100 * 15} {
			want := int(math.Round(8000 * math.Sin(2*math.Pi*660*float64(i)/float64(rate))))
			assert.Equal(t, want, pcmSample(pcm, i), "sample %d at %d Hz", i, rate)
		}
		// In the attack it is scaled down.
		i := rate / 200 // 5 ms: half way up
		want := int(math.Round(8000 * 0.5 * math.Sin(2*math.Pi*660*float64(i)/float64(rate))))
		assert.Equal(t, want, pcmSample(pcm, i))

		peak := 0
		for j := 0; j < n; j++ {
			peak = max(peak, pcmSample(pcm, j), -pcmSample(pcm, j))
		}
		assert.LessOrEqual(t, peak, 8000)
		assert.Greater(t, peak, 7900)
	}
	assert.Equal(t, EarconPCM(24000), EarconPCM(24000), "deterministic")
}
