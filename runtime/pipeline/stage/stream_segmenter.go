package stage

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// segmentConfig sizes the segments a stream_hold: segment guardrail judges
// (StreamHoldConfig). Sizes are in bytes of the reply text.
type segmentConfig struct {
	// minChars is the size sentences are merged up to before a segment ends.
	minChars int
	// firstMinChars is minChars for a round's first segment, smaller because
	// the first release is the latency a caller notices most.
	firstMinChars int
	// maxChars is the cap: a segment with no boundary by then is cut at the
	// last whitespace (the last line end inside a code fence) before it.
	maxChars int
}

const codeFence = "```"

// nextSegmentEnd finds where the segment that starts at from ends in text, a
// reply still being written. ok is false while no boundary is known yet: a
// boundary is declared only once the text after it shows that it is one.
//
// Boundaries are a sentence terminator followed by whitespace, a CJK
// terminator, a blank line, and a line end before a list item or table row.
// Terminators inside a code fence, inline code or a link are not boundaries.
// Sentences merge until the segment reaches minChars (firstMinChars when first);
// without a boundary by maxChars the segment is cut at the cap. The segmenter
// only affects latency and cost: every byte is judged before it is released,
// whatever the boundaries.
func nextSegmentEnd(text string, from int, first bool, cfg segmentConfig) (int, bool) {
	minLen := cfg.minChars
	if first {
		minLen = cfg.firstMinChars
	}
	sc := segmentScanner{text: text, inFence: strings.Count(text[:from], codeFence)%2 == 1}
	for i := from; i < len(text); {
		end, size := sc.step(i)
		if end > 0 && end-from >= max(minLen, 1) {
			return end, true
		}
		i += size
	}
	return capSegmentEnd(text, from, sc.inFence, cfg.maxChars)
}

// segmentScanner tracks the markdown context a boundary is suppressed in.
type segmentScanner struct {
	text     string
	inFence  bool
	inInline bool // inline code
	inLink   bool // a link's text or URL
}

// step reads the rune at i and returns the end of the segment it closes (0 for
// none) and how many bytes it consumed.
func (sc *segmentScanner) step(i int) (end, size int) {
	text := sc.text
	if strings.HasPrefix(text[i:], codeFence) {
		sc.inFence = !sc.inFence
		return 0, len(codeFence)
	}
	r, size := utf8.DecodeRuneInString(text[i:])
	if sc.inFence {
		return 0, size
	}
	switch r {
	case '\n':
		sc.inInline, sc.inLink = false, false
		return lineBoundary(text, i), size
	case '`':
		sc.inInline = !sc.inInline
		return 0, size
	case '[':
		sc.inLink = !sc.inInline
		return 0, size
	case ')':
		sc.inLink = false
		return 0, size
	case ']':
		// A link's URL follows in parentheses; anything else ends the link.
		sc.inLink = strings.HasPrefix(text[i+size:], "(")
		return 0, size
	}
	if sc.inInline || sc.inLink {
		return 0, size
	}
	switch r {
	case '。', '！', '？':
		return i + size, size
	case '.', '!', '?', '…':
		return sentenceEnd(text, i+size), size
	}
	return 0, size
}

// sentenceEnd returns where a sentence whose terminator ends at after ends:
// past any closing quotes or brackets, when whitespace follows them. 0 when
// what follows is not (or not yet known to be) whitespace.
func sentenceEnd(text string, after int) int {
	j := after
	for j < len(text) {
		r, size := utf8.DecodeRuneInString(text[j:])
		if !strings.ContainsRune(`"'”’)]»`, r) {
			break
		}
		j += size
	}
	if j >= len(text) {
		return 0
	}
	if r, _ := utf8.DecodeRuneInString(text[j:]); unicode.IsSpace(r) {
		return j
	}
	return 0
}

// lineBoundary returns where a segment ends at the line end at i: after a
// blank line, or before a line that starts a list item or table row. 0 when
// the next line does neither, or is not yet long enough to tell.
func lineBoundary(text string, i int) int {
	next := text[i+1:]
	switch {
	case strings.HasPrefix(next, "\n"):
		return i + len("\n\n")
	case strings.HasPrefix(next, "- "), strings.HasPrefix(next, "* "), strings.HasPrefix(next, "+ "),
		strings.HasPrefix(next, "|"):
		return i + 1
	}
	digits := len(next) - len(strings.TrimLeft(next, "0123456789"))
	if digits > 0 && strings.HasPrefix(next[digits:], ". ") {
		return i + 1
	}
	return 0
}

// capSegmentEnd cuts a segment that reached maxChars with no boundary: after
// the last line end before the cap inside a code fence, else after the last
// whitespace, else at the cap on a rune boundary.
func capSegmentEnd(text string, from int, fenceOpen bool, maxChars int) (int, bool) {
	if maxChars <= 0 || len(text)-from < maxChars {
		return 0, false
	}
	window := text[from : from+maxChars]
	inFence := fenceOpen || strings.Contains(window, codeFence)
	cut := strings.LastIndexByte(window, '\n')
	if !inFence {
		cut = strings.LastIndexFunc(window, unicode.IsSpace)
	}
	if cut > 0 {
		_, size := utf8.DecodeRuneInString(window[cut:])
		return from + cut + size, true
	}
	end := from + maxChars
	for end > from && !utf8.RuneStart(text[end]) {
		end--
	}
	if end == from {
		return 0, false
	}
	return end, true
}
