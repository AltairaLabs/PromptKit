package stage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// segmentAll feeds deltas to a segmenter one at a time and returns the segments
// it completed along the way, then what flush leaves.
func segmentAll(maxRunes int, deltas ...string) (completed []string, rest string) {
	g := speechSegmenter{maxRunes: maxRunes}
	for _, d := range deltas {
		completed = append(completed, g.add(d)...)
	}
	return completed, g.flush()
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

func TestSpeechSegmenter_KeepsAbbreviationsAndInitials(t *testing.T) {
	got, rest := segmentAll(0, "Dr. Smith met J. K. Rowling, e.g. at noon. Then she left. ")
	assert.Equal(t, []string{"Dr. Smith met J. K. Rowling, e.g. at noon.", "Then she left."}, got)
	assert.Empty(t, rest)
}

func TestSpeechSegmenter_NumberedListItemsAreNotSentenceEnds(t *testing.T) {
	got, rest := segmentAll(0, "Steps:\n1. Open it.\n2. Close it.")
	assert.Equal(t, []string{"Steps:", "1. Open it."}, got)
	assert.Equal(t, "2. Close it.", rest)
}

func TestSpeechSegmenter_ClosingQuoteStaysWithItsSentence(t *testing.T) {
	got, rest := segmentAll(0, `He said "Hi." Then he went (quietly.) Done`)
	assert.Equal(t, []string{`He said "Hi."`, `Then he went (quietly.)`}, got)
	assert.Equal(t, "Done", rest)
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
}

func TestSpeechSegmenter_FlushAndResetDropText(t *testing.T) {
	g := speechSegmenter{}
	assert.Empty(t, g.add("Half a sente"))
	g.reset()
	assert.Empty(t, g.flush(), "reset drops what was not yet spoken")
	assert.Empty(t, g.add("   "))
	assert.Empty(t, g.flush(), "whitespace alone is not a segment")
}
