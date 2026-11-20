package stage

import (
	"strings"
)

// defaultSegmentMaxRunes caps a speech segment that has no sentence end: a
// long run without one is still spoken in pieces of about this size, so the
// first audio never waits for the whole of it.
const defaultSegmentMaxRunes = 250

// speechSegmenter splits a reply's streamed text into the segments a speech-out
// stage synthesizes one at a time. Text is spoken once: every segment add
// returns, plus what flush returns at the end, is the text it was given, in
// order, less the whitespace between segments.
//
// Where a segment ends is speechSegmentEnd's to decide; the segmenter only
// keeps the text not yet returned.
type speechSegmenter struct {
	// maxRunes is the size cap; zero means defaultSegmentMaxRunes.
	maxRunes int
	pending  string
	// started is set once a segment has been returned since the last reset.
	started bool
}

// add takes the next text of the reply and returns the segments it completes.
func (g *speechSegmenter) add(text string) []string {
	g.pending += text
	var out []string
	for {
		end, ok := speechSegmentEnd(g.pending, 0, !g.started, g.cap())
		if !ok {
			return out
		}
		seg := strings.TrimSpace(g.pending[:end])
		g.pending = g.pending[end:]
		if seg != "" {
			out = append(out, seg)
			g.started = true
		}
	}
}

// flush returns what is left of the reply as its last segment ("" when only
// whitespace is left), and empties the segmenter.
func (g *speechSegmenter) flush() string {
	rest := strings.TrimSpace(g.pending)
	g.reset()
	return rest
}

// reset drops the text not yet returned as a segment.
func (g *speechSegmenter) reset() {
	g.pending = ""
	g.started = false
}

func (g *speechSegmenter) cap() int {
	if g.maxRunes > 0 {
		return g.maxRunes
	}
	return defaultSegmentMaxRunes
}

// speechSegmentEnd finds where the segment that starts at from ends in text, a
// reply still being written; ok is false while no boundary is known yet. It is
// the shared segmenter's boundary (nextSegmentEnd), with every sentence a
// segment of its own (speech starts as soon as one is complete) and a cap of
// maxRunes.
func speechSegmentEnd(text string, from int, first bool, maxRunes int) (int, bool) {
	return nextSegmentEnd(text, from, first, segmentConfig{minChars: 1, firstMinChars: 1, maxChars: maxRunes})
}
