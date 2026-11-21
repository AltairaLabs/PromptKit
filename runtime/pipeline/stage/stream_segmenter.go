package stage

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The segmenter splits a reply still being written into segments: whole
// sentences, merged up to a minimum size and cut at a maximum. It is shared by
// every stage that works on a reply a segment at a time: the provider stage's
// stream_hold (segments judged before release) and speech output (segments
// synthesized as they complete). Use a segmenter per reply:
//
//	seg := newSegmenter(defaultSegmentConfig())
//	for delta := range deltas {
//		for _, end := range seg.feed(delta) {
//			use(seg.text()[start:end]) // a complete segment
//			start = end
//		}
//	}
//	if start < len(seg.text()) {
//		use(seg.text()[start:]) // the reply ended: the rest is the last segment
//	}
//
// Boundaries are a sentence terminator (. ! ? …) followed by whitespace, a CJK
// terminator (。！？), a blank line, and a line end before a list item or table
// row. A terminator is not a boundary inside a number (3.14), after a list
// item's number ("1." at a line start), after a
// single-letter word or an initialism (e.g., U.S.), after a common title or
// abbreviation (Mr., Dr., vs.), or inside a code fence, inline code or a link.
// A boundary is declared only once the text after it shows that it is one, so
// a segment never ends on text the next delta could continue.

// segmentConfig sizes a segmenter's segments, in characters (runes) of reply
// text.
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

// defaultSegmentConfig is the segment sizing stream_hold uses by default.
func defaultSegmentConfig() segmentConfig {
	return segmentConfig{
		minChars:      defaultStreamHoldMinChars,
		firstMinChars: defaultStreamHoldFirstMinChars,
		maxChars:      defaultStreamHoldMaxChars,
	}
}

// segmenter finds the segment boundaries of a reply as its text arrives.
type segmenter struct {
	cfg  segmentConfig
	buf  strings.Builder
	ends []int // the end offsets of the segments found so far
}

// newSegmenter returns a segmenter for one reply.
func newSegmenter(cfg segmentConfig) *segmenter { return &segmenter{cfg: cfg} }

// feed adds delta to the reply and returns the end offsets, into text(), of
// the segments it completed, in order. Most deltas complete none.
func (g *segmenter) feed(delta string) []int {
	g.buf.WriteString(delta)
	text := g.buf.String()
	var found []int
	for {
		end, ok := nextSegmentEnd(text, g.last(), len(g.ends) == 0, g.cfg)
		if !ok {
			return found
		}
		g.ends = append(g.ends, end)
		found = append(found, end)
	}
}

// text is the reply so far.
func (g *segmenter) text() string { return g.buf.String() }

// segments is the end offsets of the segments found so far.
func (g *segmenter) segments() []int { return g.ends }

// last is the end of the last segment found, where the next one starts.
func (g *segmenter) last() int {
	if len(g.ends) == 0 {
		return 0
	}
	return g.ends[len(g.ends)-1]
}

// reset discards the reply, for a reply that restarts.
func (g *segmenter) reset() {
	g.buf.Reset()
	g.ends = nil
}

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
	fenceOpen := strings.Count(text[:from], codeFence)&1 == 1 // an odd count: inside a fence
	sc := segmentScanner{text: text, inFence: fenceOpen}
	for i := from; i < len(text); {
		end, size := sc.step(i)
		if end > 0 {
			n := utf8.RuneCountInString(text[from:end])
			if cfg.maxChars > 0 && n > cfg.maxChars {
				// The sentence runs past the cap, though it arrived whole:
				// cut it at the cap, as if it were still arriving.
				break
			}
			if n >= max(minLen, 1) {
				return end, true
			}
		}
		i += size
	}
	return capSegmentEnd(text, from, fenceOpen, cfg.maxChars)
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
	case '.':
		if abbreviationBefore(text, i) || listMarkerBefore(text, i) {
			return 0, size
		}
		return sentenceEnd(text, i+size), size
	case '!', '?', '…':
		return sentenceEnd(text, i+size), size
	}
	return 0, size
}

// abbreviations are the common titles and abbreviations whose period does not
// end a sentence. Lower case; matched against the word before the period.
var abbreviations = map[string]bool{ //nolint:gochecknoglobals // a fixed lookup set
	"mr": true, "mrs": true, "ms": true, "dr": true, "prof": true, "st": true,
	"jr": true, "sr": true, "vs": true, "fig": true, "approx": true,
	"inc": true, "ltd": true, "co": true, "mt": true,
}

// abbreviationBefore reports whether the period at i ends an abbreviation
// rather than a sentence: the word before it is one letter (an initial), holds
// another period (an initialism such as e.g or U.S), or is a common title or
// abbreviation.
func abbreviationBefore(text string, i int) bool {
	start := strings.LastIndexFunc(text[:i], func(r rune) bool { return unicode.IsSpace(r) || r == '(' }) + 1
	word := text[start:i]
	if word == "" {
		return false
	}
	if utf8.RuneCountInString(word) == 1 || strings.Contains(word, ".") {
		r, _ := utf8.DecodeRuneInString(word)
		return unicode.IsLetter(r)
	}
	return abbreviations[strings.ToLower(word)]
}

// listMarkerBefore reports whether the period at i ends a numbered list
// item's marker ("1." at the start of a line): it belongs with the item's
// text, not in a segment of its own.
func listMarkerBefore(text string, i int) bool {
	lineStart := strings.LastIndexByte(text[:i], '\n') + 1
	marker := strings.TrimLeft(text[lineStart:i], " \t")
	return marker != "" && strings.Trim(marker, "0123456789") == ""
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

// capSegmentEnd cuts a segment that reached maxChars characters with no
// boundary: after the last line end before the cap inside a code fence, else
// after the last whitespace, else at the cap.
func capSegmentEnd(text string, from int, fenceOpen bool, maxChars int) (int, bool) {
	if maxChars <= 0 {
		return 0, false
	}
	capEnd, n := len(text), 0
	for i := range text[from:] {
		if n == maxChars {
			capEnd = from + i
			break
		}
		n++
	}
	if n < maxChars {
		return 0, false
	}
	capEnd = outsideFenceMarker(text, from, capEnd)
	window := text[from:capEnd]
	inFence := fenceOpen || strings.Contains(window, codeFence)
	cut := strings.LastIndexByte(window, '\n')
	if !inFence {
		cut = strings.LastIndexFunc(window, unicode.IsSpace)
	}
	if cut > 0 {
		_, size := utf8.DecodeRuneInString(window[cut:])
		return from + cut + size, true
	}
	return capEnd, true
}

// outsideFenceMarker moves a cut at end back before a code fence marker it
// would split, so the segments on either side each hold whole markers and the
// fence state at every segment start (an odd count of markers before it) stays
// true. A marker that starts at from cannot move back; the cut goes after it.
func outsideFenceMarker(text string, from, end int) int {
	for start := max(end-len(codeFence)+1, from); start < end; start++ {
		if strings.HasPrefix(text[start:], codeFence) {
			if start == from {
				return min(from+len(codeFence), len(text))
			}
			return start
		}
	}
	return end
}
