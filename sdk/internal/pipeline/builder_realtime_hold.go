package pipeline

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
)

// realtimeSegmentTimeout is the default bound on a stream_hold guardrail's
// partial-reply judgment in a realtime session that holds replies
// (StreamHoldConfig.Realtime): the caller hears nothing while a segment
// waits, and a judgment past it fails closed. Measured live, an LLM judge
// answered a segment in 1.0 to 1.8 s, and 2 of 24 judgments ran past 2 s,
// cutting replies that would have passed.
const realtimeSegmentTimeout = 3 * time.Second

// realtimeStreamHold is the conversation's stream hold settings for a
// realtime session: a judgment bound the host did not set is
// realtimeSegmentTimeout.
func realtimeStreamHold(c stage.StreamHoldConfig) stage.StreamHoldConfig {
	if c.SegmentTimeout <= 0 {
		c.SegmentTimeout = realtimeSegmentTimeout
	}
	return c
}
