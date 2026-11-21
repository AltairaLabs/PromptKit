package pipeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
)

// A realtime session bounds each partial-reply judgment at 3 s unless the
// host set a bound; the rest of the stream hold settings reach the stage as
// set.
func TestRealtimeStreamHold_BoundsJudgments(t *testing.T) {
	got := realtimeStreamHold(stage.StreamHoldConfig{Realtime: stage.RealtimeHoldSegment, MinChars: 40})
	assert.Equal(t, stage.StreamHoldConfig{
		Realtime: stage.RealtimeHoldSegment, MinChars: 40, SegmentTimeout: 3 * time.Second,
	}, got)

	set := realtimeStreamHold(stage.StreamHoldConfig{SegmentTimeout: 5 * time.Second})
	assert.Equal(t, 5*time.Second, set.SegmentTimeout)
}
