package segment

import (
	"strings"
)

// defaultSegmentMaxRunes caps a speech segment that has no sentence end: a
// long run without one is still spoken in pieces of about this size, so the
// first audio never waits for the whole of it.
const defaultSegmentMaxRunes = 250

// Speech splits a reply's streamed text into the segments a speech-out
// stage synthesizes one at a time. Text is spoken once: every segment Add
// returns, plus what Flush returns at the end, is the text it was given, in
// order, less the whitespace between segments.
//
// Where a segment ends is speechSegmentEnd's to decide; the segmenter only
// keeps the text not yet returned.
type Speech struct {
	// MaxRunes is the size cap; zero means defaultSegmentMaxRunes.
	MaxRunes int
	pending  string
	// started is set once a segment has been returned since the last reset.
	started bool
}

// Add takes the next text of the reply and returns the segments it completes.
func (g *Speech) Add(text string) []string {
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

// Flush returns what is left of the reply as its last segment ("" when only
// whitespace is left), and empties the segmenter.
func (g *Speech) Flush() string {
	rest := strings.TrimSpace(g.pending)
	g.Reset()
	return rest
}

// Reset drops the text not yet returned as a segment.
func (g *Speech) Reset() {
	g.pending = ""
	g.started = false
}

// Pending is the text given to Add not yet returned as a segment.
func (g *Speech) Pending() string { return g.pending }

func (g *Speech) cap() int {
	if g.MaxRunes > 0 {
		return g.MaxRunes
	}
	return defaultSegmentMaxRunes
}

// speechSegmentEnd finds where the segment that starts at from ends in text, a
// reply still being written; ok is false while no boundary is known yet. It is
// the shared segmenter's boundary (nextSegmentEnd), with every sentence a
// segment of its own (speech starts as soon as one is complete) and a cap of
// maxRunes.
func speechSegmentEnd(text string, from int, first bool, maxRunes int) (int, bool) {
	return nextSegmentEnd(text, from, first, Config{MinChars: 1, FirstMinChars: 1, MaxChars: maxRunes})
}
