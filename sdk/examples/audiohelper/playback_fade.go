package audiohelper

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
)

// fadeSamples is how many samples a fade of d lasts at rate Hz. Zero means
// the fade is too short to hear, and a flush cuts instead.
func fadeSamples(rate int, d time.Duration) int {
	if d <= 0 || rate <= 0 {
		return 0
	}
	return int(int64(rate) * int64(d) / int64(time.Second))
}

// flushTail is what the two-stream play loop plays on a flush that fades:
// the head of the audio still queued (its accumulation, then what the flush
// drained from playCh), n samples ramped down to silence, padded with silence
// to whole speaker blocks of blockSamples.
func flushTail(acc, drained []byte, n, blockSamples int) []byte {
	queued := make([]byte, 0, len(acc)+len(drained))
	queued = append(queued, acc...)
	queued = append(queued, drained...)
	tail := audio.FadeOutPCM16(queued, n)
	blockBytes := blockSamples * bytesPerSample
	if blockBytes > 0 {
		if rem := len(tail) % blockBytes; rem != 0 {
			tail = append(tail, make([]byte, blockBytes-rem)...)
		}
	}
	return tail
}
