package framesample

import (
	"image"
	"image/gif"
	"testing"
)

// TestDisposalDelayFallback covers the short-slice tolerance: a GIF whose
// Disposal / Delay slices are shorter than its frame list (older/simpler
// encoders) must not panic and must fall back to sane defaults.
func TestDisposalDelayFallback(t *testing.T) {
	g := &gif.GIF{Image: make([]*image.Paletted, 3)} // no Disposal, no Delay

	if got := disposalMethod(g, 2); got != gif.DisposalNone {
		t.Errorf("disposalMethod out-of-range = %d, want DisposalNone", got)
	}
	if got := delayAt(g, 2); got != 0 {
		t.Errorf("delayAt out-of-range = %d, want 0", got)
	}
}
