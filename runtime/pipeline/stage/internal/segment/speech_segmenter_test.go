package segment

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// segmentAll feeds deltas to a segmenter one at a time and returns the segments
// it completed along the way, then what flush leaves. A segmenter that makes
// no progress loops forever, so the run is bounded.
func segmentAll(maxRunes int, deltas ...string) (completed []string, rest string) {
	type result struct {
		completed []string
		rest      string
	}
	done := make(chan result, 1)
	go func() {
		g := Speech{MaxRunes: maxRunes}
		var r result
		for _, d := range deltas {
			r.completed = append(r.completed, g.Add(d)...)
		}
		r.rest = g.Flush()
		done <- r
	}()
	select {
	case r := <-done:
		return r.completed, r.rest
	case <-time.After(segmentTestDeadline):
		panic("the speech segmenter made no progress")
	}
}

func TestSpeechSegmenter_SplitsAtSentenceEnds(t *testing.T) {
	got, rest := segmentAll(0, "The fox woke", " early. It ran", " far! Did it", " stop? No.")
	assert.Equal(t, []string{"The fox woke early.", "It ran far!", "Did it stop?"}, got)
	assert.Equal(t, "No.", rest, "a sentence end with nothing after it waits for the reply to end")
}

func TestSpeechSegmenter_SentenceEndNeedsTheNextCharacter(t *testing.T) {
	// "3." could be "3.14": the period is not an end until a space follows it.
	got, _ := segmentAll(0, "Pi is 3.")
	assert.Empty(t, got)
	got, rest := segmentAll(0, "Pi is 3.", "14 or so. Next")
	assert.Equal(t, []string{"Pi is 3.14 or so."}, got)
	assert.Equal(t, "Next", rest)
}

func TestSpeechSegmenter_CapsALongRunAtAWordBoundary(t *testing.T) {
	long := strings.Repeat("word ", 30) // 150 runes, no sentence end
	got, rest := segmentAll(40, long)
	for _, seg := range got {
		assert.LessOrEqual(t, len([]rune(seg)), 40, "segment %q exceeds the cap", seg)
		assert.False(t, strings.HasSuffix(seg, "wor"), "a cap must not split a word: %q", seg)
	}
	assert.Equal(t, strings.TrimSpace(long), strings.Join(append(got, rest), " "),
		"capped segments still speak every word, once, in order")
}

func TestSpeechSegmenter_CapsAnUnbrokenRun(t *testing.T) {
	got, rest := segmentAll(10, strings.Repeat("x", 25))
	assert.Equal(t, []string{"xxxxxxxxxx", "xxxxxxxxxx"}, got)
	assert.Equal(t, "xxxxx", rest)

	// With no cap set, the default one applies: a run with no sentence end is
	// still spoken in pieces, not held until the reply ends.
	got, rest = segmentAll(0, strings.Repeat("y", 2*defaultSegmentMaxRunes+10))
	assert.Equal(t, []string{strings.Repeat("y", defaultSegmentMaxRunes), strings.Repeat("y", defaultSegmentMaxRunes)}, got)
	assert.Equal(t, strings.Repeat("y", 10), rest)
}

func TestSpeechSegmenter_FlushAndResetDropText(t *testing.T) {
	g := Speech{}
	assert.Empty(t, g.Add("Half a sente"))
	assert.Equal(t, "Half a sente", g.Pending(), "the text not yet a segment is pending")
	g.Reset()
	assert.Empty(t, g.Pending())
	assert.Empty(t, g.Flush(), "reset drops what was not yet spoken")
	assert.Empty(t, g.Add("   "))
	assert.Empty(t, g.Flush(), "whitespace alone is not a segment")
}
