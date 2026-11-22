package voice_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/voice"
)

// SetPacingClock puts a pacer on the given clock and preroll: with a preroll
// of two, the first two chunks go out at once and the third waits exactly one
// chunk's duration on the clock's sleep.
func TestSetPacingClock_PacesOnTheGivenClockAndPreroll(t *testing.T) {
	now := time.Unix(0, 0)
	var slept []time.Duration
	s := voice.NewAudioPacingStage()
	voice.SetPacingClock(s,
		func() time.Time { return now },
		func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			now = now.Add(d)
			return nil
		}, 2)

	input := make(chan core.StreamElement, 3)
	for range 3 {
		input <- makeAudioElement(make([]byte, 2400), 24000) // 50 ms each
	}
	close(input)
	output := make(chan core.StreamElement, 3)
	require.NoError(t, s.Process(context.Background(), input, output))

	assert.Len(t, drained(output), 3)
	assert.Equal(t, []time.Duration{50 * time.Millisecond}, slept)
}

func TestFlushesOnCut_ReportsWithFlushOnCut(t *testing.T) {
	assert.False(t, voice.FlushesOnCut(voice.NewAudioPacingStage()))
	assert.True(t, voice.FlushesOnCut(voice.NewAudioPacingStage().WithFlushOnCut()))
}
