package duplex

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/cue"
)

// CuesOf is the clip bank s tells of the session it creates, nil when no cue
// stage plays its cues. It lets stage's realtime chain tests see which cue
// stage a chain wired: they cannot reach s's unexported fields from outside
// this package. stage does not re-export it.
func CuesOf(s *DuplexProviderStage) *cue.Bank {
	return s.cues
}

// ReorderPlaceholder is the caller text s writes for a turn that ends with no
// transcript. stage does not re-export it.
func ReorderPlaceholder(s *TranscriptReorderStage) string {
	return s.placeholder
}

// SetPlayoutClock makes s read the time and wait for playout with clock. It
// lets stage's tests drive a playout hold on a fake clock. stage does not
// re-export it.
func SetPlayoutClock(s *PlayoutHoldStage, clock core.HoldClock) {
	s.clock = clock
}
