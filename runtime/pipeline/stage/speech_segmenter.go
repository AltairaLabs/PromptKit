package stage

import (
	"strings"
	"unicode"
	"unicode/utf8"
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
// reply still being written; ok is false while no boundary is known yet. first
// is set for a reply's first segment.
//
// It has the shape of the stream_hold segmenter's nextSegmentEnd (#2828) so the
// two can share one: once that lands this body becomes a call to it, with
// speech's sizes. Until then: a sentence ends at '.', '!', '?' or '…' (with
// any closing quotes or brackets after it) followed by whitespace, so "3.14" is
// not split and a period at the end of the text so far is not an end until the
// next character arrives. A period after a common abbreviation ("Dr.",
// "e.g."), an initial ("J.") or a number (a list item's "1.") does not end a
// sentence. A line end ends a segment, and a run with neither is cut at the
// last whitespace before maxRunes.
func speechSegmentEnd(text string, from int, _ bool, maxRunes int) (int, bool) {
	rest := text[from:]
	if end := sentenceBoundary(rest); end >= 0 {
		return from + end, true
	}
	if end := capBoundary(rest, maxRunes); end >= 0 {
		return from + end, true
	}
	return 0, false
}

// sentenceBoundary returns the offset just past the first sentence or line end
// in s that has speakable text before it, or -1.
func sentenceBoundary(s string) int {
	for i, r := range s {
		switch {
		case r == '\n':
			if strings.TrimSpace(s[:i]) != "" {
				return i + 1
			}
		case isSentenceTerminator(r):
			end, ok := sentenceEndAfter(s, i)
			if !ok {
				// The character after the terminator has not arrived yet:
				// nothing later can be an end before this one is decided.
				return -1
			}
			if end >= 0 {
				return end
			}
		}
	}
	return -1
}

// sentenceEndAfter decides whether the terminator at s[i] ends a sentence.
// It returns the offset past the terminator and any closing punctuation after
// it when it does, -1 when it does not, and ok=false when s ends before that
// can be decided.
func sentenceEndAfter(s string, i int) (end int, ok bool) {
	j := i
	for j < len(s) {
		r, size := utf8.DecodeRuneInString(s[j:])
		if !isSentenceTerminator(r) && !isClosingPunct(r) {
			break
		}
		j += size
	}
	if j >= len(s) {
		return -1, false
	}
	next, _ := utf8.DecodeRuneInString(s[j:])
	if !unicode.IsSpace(next) {
		return -1, true
	}
	if s[i] == '.' && j == i+1 && notSentenceEnd(wordBefore(s, i)) {
		return -1, true
	}
	return j, true
}

// capBoundary returns where a run over maxRunes is split, at the last
// whitespace within the cap, else at the cap; -1 while s is within the cap.
func capBoundary(s string, maxRunes int) int {
	if utf8.RuneCountInString(strings.TrimSpace(s)) <= maxRunes {
		return -1
	}
	lead := len(s) - len(strings.TrimLeftFunc(s, unicode.IsSpace))
	limit, n := len(s), 0
	for i := range s[lead:] {
		if n == maxRunes {
			limit = lead + i
			break
		}
		n++
	}
	if ws := strings.LastIndexFunc(s[lead:limit], unicode.IsSpace); ws > 0 {
		return lead + ws + 1
	}
	return limit
}

func isSentenceTerminator(r rune) bool {
	return r == '.' || r == '!' || r == '?' || r == '…'
}

func isClosingPunct(r rune) bool {
	switch r {
	case '"', '\'', ')', ']', '}', '”', '’', '»':
		return true
	}
	return false
}

// wordBefore returns the word that ends at s[i], without leading punctuation.
func wordBefore(s string, i int) string {
	start := strings.LastIndexFunc(s[:i], unicode.IsSpace) + 1
	return strings.TrimLeft(s[start:i], `"'([{“‘«`)
}

// speechAbbreviations are words whose period does not end a sentence.
var speechAbbreviations = map[string]bool{ //nolint:gochecknoglobals // read-only lookup table
	"mr": true, "mrs": true, "ms": true, "dr": true, "prof": true, "sr": true, "jr": true,
	"st": true, "vs": true, "e.g": true, "i.e": true, "approx": true,
	"inc": true, "ltd": true, "co": true, "mt": true, "fig": true,
}

// notSentenceEnd reports whether a period after word is part of the word: an
// abbreviation, an initial, or a number (a list item).
func notSentenceEnd(word string) bool {
	if word == "" {
		return false
	}
	if r, size := utf8.DecodeRuneInString(word); size == len(word) && unicode.IsLetter(r) {
		return true
	}
	if strings.IndexFunc(word, func(r rune) bool { return !unicode.IsDigit(r) }) < 0 {
		return true
	}
	return speechAbbreviations[strings.ToLower(word)]
}
