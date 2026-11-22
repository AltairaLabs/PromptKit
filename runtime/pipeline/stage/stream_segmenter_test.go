package stage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// segmentEnd is what nextSegmentEnd returned.
type segmentEnd struct {
	end int
	ok  bool
}

// feedWithin is seg.feed(delta), failing the test when it does not return:
// a scan that makes no progress loops forever.
func feedWithin(t *testing.T, seg *segmenter, delta string) []int {
	t.Helper()
	return within(t, "segmenter.feed", func() []int { return seg.feed(delta) })
}

// The segmenter splits a reply still being written into the segments a
// stream_hold: segment guardrail judges. A boundary is only declared once the
// text after it shows it is one, so every case gives the text so far.
func TestNextSegmentEnd(t *testing.T) {
	small := segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 600}
	cases := []struct {
		name   string
		text   string
		from   int
		first  bool
		cfg    segmentConfig
		want   int
		wantOK bool
	}{
		{name: "sentence then space", text: "Hello there. Next", cfg: small, want: 12, wantOK: true},
		{name: "terminator at the end is not yet a boundary", text: "Hello there.", cfg: small},
		{name: "question and exclamation", text: "Really? Yes", cfg: small, want: 7, wantOK: true},
		{name: "ellipsis", text: "Well… then", cfg: small, want: len("Well…"), wantOK: true},
		{name: "closing quote is kept with its sentence", text: `He said "stop." Then`, cfg: small,
			want: len(`He said "stop."`), wantOK: true},
		{name: "decimal is not a boundary", text: "Pi is 3.14 or so", cfg: small},
		{name: "URL is not a boundary", text: "See https://example.com/a.b for", cfg: small},
		{name: "CJK terminator needs no space", text: "你好。再见", cfg: small, want: len("你好。"), wantOK: true},
		{name: "blank line ends a paragraph", text: "Intro\n\nMore", cfg: small, want: len("Intro\n\n"), wantOK: true},
		{name: "list item starts a segment", text: "Items:\n- one", cfg: small, want: len("Items:\n"), wantOK: true},
		{name: "numbered list item", text: "Steps:\n1. go", cfg: small, want: len("Steps:\n"), wantOK: true},
		{name: "table row", text: "Table:\n| a |", cfg: small, want: len("Table:\n"), wantOK: true},
		{name: "plain newline is not a boundary", text: "line one\nline two", cfg: small},
		{name: "inside a code fence", text: "```\nx = 1. y = 2. z", cfg: small},
		{name: "after a code fence closes", text: "```\nx. y\n``` Done. Next", cfg: small,
			want: len("```\nx. y\n``` Done."), wantOK: true},
		{name: "inline code", text: "Run `a. b` now. Then", cfg: small, want: len("Run `a. b` now."), wantOK: true},
		{name: "link text", text: "See [a. b](http://x) here. Then", cfg: small,
			want: len("See [a. b](http://x) here."), wantOK: true},
		{name: "initial is not a boundary", text: "Ask J. Smith. Then", cfg: small,
			want: len("Ask J. Smith."), wantOK: true},
		{name: "initialism is not a boundary", text: "Fruit, e.g. apples. Then", cfg: small,
			want: len("Fruit, e.g. apples."), wantOK: true},
		{name: "title is not a boundary", text: "Ask Dr. Who now. Then", cfg: small,
			want: len("Ask Dr. Who now."), wantOK: true},
		{name: "parenthesized initialism", text: "Go (i.e. now). Then", cfg: small,
			want: len("Go (i.e. now)."), wantOK: true},
		{name: "company abbreviations", text: "Acme Inc. and Foo Ltd. and Bar Co. sold it. Then", cfg: small,
			want: len("Acme Inc. and Foo Ltd. and Bar Co. sold it."), wantOK: true},
		{name: "mount abbreviation", text: "Climb Mt. Fuji now. Then", cfg: small,
			want: len("Climb Mt. Fuji now."), wantOK: true},
		{name: "i.e. is not a boundary", text: "Rain, i.e. water. Then", cfg: small,
			want: len("Rain, i.e. water."), wantOK: true},
		{name: "a year ends a sentence", text: "It began in 1990. Then", cfg: small,
			want: len("It began in 1990."), wantOK: true},
		{name: "a digit before the period ends a sentence", text: "It was 1999. Then", cfg: small,
			want: len("It was 1999."), wantOK: true},
		{name: "scan starts at from", text: "One. Two. Three", from: 4, cfg: small, want: 9, wantOK: true},
		{name: "a segment of exactly min_chars ends", text: "Hello. More",
			cfg: segmentConfig{minChars: 6, firstMinChars: 6, maxChars: 600}, want: 6, wantOK: true},
		{name: "min_chars counts from the segment start", text: "One. Two. Three. Four", from: 4,
			cfg: segmentConfig{minChars: 6, firstMinChars: 6, maxChars: 600}, want: 16, wantOK: true},
		{name: "a line starting with a dot is not a list item", text: "Intro:\n. dot",
			cfg: segmentConfig{minChars: 7, firstMinChars: 7, maxChars: 600}, want: 8, wantOK: true},
		{
			name: "cap at exactly max_chars", text: "aaaa bbbb cc",
			cfg: segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 12}, want: 10, wantOK: true,
		},
		{
			name: "cap counts from the segment start", text: "xxxx aaaa bb", from: 5,
			cfg: segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 12},
		},
		{
			name: "cap inside a fence opened before the segment", text: "```\nab cd\nef gh ij kl", from: 4,
			cfg: segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 12}, want: 10, wantOK: true,
		},
		{
			name: "cap with only leading whitespace cuts at the cap", text: " aaaaaaaaaaaaa",
			cfg: segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 12}, want: 12, wantOK: true,
		},
		{
			name: "short sentences merge up to min_chars", text: "Hi. Yes. Okay then. More",
			cfg: segmentConfig{minChars: 10, firstMinChars: 10, maxChars: 600}, want: len("Hi. Yes. Okay then."), wantOK: true,
		},
		{
			name: "first segment uses the smaller minimum", text: "Hi there. And then some more.",
			first: true, cfg: segmentConfig{minChars: 60, firstMinChars: 5, maxChars: 600},
			want: len("Hi there."), wantOK: true,
		},
		{
			name: "later segment waits for min_chars", text: "Hi there. And then",
			cfg: segmentConfig{minChars: 60, firstMinChars: 5, maxChars: 600},
		},
		{
			name: "cap cuts at the last space", text: "aaaa bbbb cccc dddd",
			cfg: segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 12}, want: len("aaaa bbbb "), wantOK: true,
		},
		{
			name: "cap counts characters, not bytes", text: strings.Repeat("中", 12),
			cfg: segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 10}, want: len(strings.Repeat("中", 10)), wantOK: true,
		},
		{
			name: "under the cap in characters waits", text: strings.Repeat("中", 9),
			cfg: segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 10},
		},
		{
			name: "min_chars counts characters", text: "Ça été. Fini. Encore",
			cfg: segmentConfig{minChars: 9, firstMinChars: 9, maxChars: 600}, want: len("Ça été. Fini."), wantOK: true,
		},
		{
			name: "cap inside a code fence cuts at a line end", text: "```\nab cd\nef gh ij",
			cfg: segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 14}, want: len("```\nab cd\n"), wantOK: true,
		},
		{
			name: "under the cap without a boundary waits", text: "aaaa bbbb",
			cfg: segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 12},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := within(t, "nextSegmentEnd", func() segmentEnd {
				end, ok := nextSegmentEnd(tc.text, tc.from, tc.first, tc.cfg)
				return segmentEnd{end: end, ok: ok}
			})
			assert.Equal(t, tc.wantOK, got.ok)
			assert.Equal(t, tc.want, got.end)
		})
	}
}

// A segmenter fed a reply delta by delta reports each segment as the text
// that completes it arrives.
// A bare list marker is not a segment: the period after "1." at the start of
// a line joins the item's text, so speech does not say "one." and pause, and a
// judge is never given a segment that is only the marker.
func TestNextSegmentEnd_ListMarkerJoinsItsItem(t *testing.T) {
	small := segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 600}
	cases := []struct {
		name   string
		text   string
		from   int
		want   int
		wantOK bool
	}{
		{name: "numbered item", text: "1. Open it. Next", want: len("1. Open it."), wantOK: true},
		{name: "numbered item after a line", text: "Steps:\n1. Open it. Next", from: len("Steps:\n"),
			want: len("Steps:\n1. Open it."), wantOK: true},
		{name: "two-digit item", text: "12. Close it. Next", want: len("12. Close it."), wantOK: true},
		{name: "indented item", text: "  3. Wait. Next", want: len("  3. Wait."), wantOK: true},
		{name: "lettered item", text: "a. Pick one. Next", want: len("a. Pick one."), wantOK: true},
		{name: "marker with nothing after it yet", text: "1. ", wantOK: false},
		{name: "a number ending a sentence mid-line", text: "It was 1999. Then", want: len("It was 1999."), wantOK: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := within(t, "nextSegmentEnd", func() segmentEnd {
				end, ok := nextSegmentEnd(tc.text, tc.from, false, small)
				return segmentEnd{end: end, ok: ok}
			})
			assert.Equal(t, tc.wantOK, got.ok)
			assert.Equal(t, tc.want, got.end)
		})
	}
}

func TestSegmenter_Feed(t *testing.T) {
	seg := newSegmenter(segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 600})
	assert.Empty(t, feedWithin(t, seg, "Hello there."))
	assert.Equal(t, []int{12, 25}, feedWithin(t, seg, " How are you? I am"))
	assert.Equal(t, []int{12, 25}, seg.segments())
	assert.Empty(t, feedWithin(t, seg, " fine"))
	assert.Equal(t, "Hello there. How are you? I am fine", seg.text())
	assert.Equal(t, 25, seg.last())

	seg.reset()
	assert.Empty(t, seg.text())
	assert.Empty(t, seg.segments())
	assert.Equal(t, 0, seg.last())
}

// The default sizing is stream_hold's.
func TestDefaultSegmentConfig(t *testing.T) {
	assert.Equal(t, segmentConfig{minChars: 60, firstMinChars: 24, maxChars: 600}, defaultSegmentConfig())
}

// segmentsOf feeds text to a segmenter in one delta and returns its segments,
// the rest included.
func segmentsOf(t *testing.T, cfg segmentConfig, text string) []string {
	t.Helper()
	seg := newSegmenter(cfg)
	start := 0
	var out []string
	for _, end := range feedWithin(t, seg, text) {
		out = append(out, text[start:end])
		start = end
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

// The cap holds when a sentence longer than it arrives whole: the boundary
// past the cap is not taken, the sentence is cut at the cap.
func TestNextSegmentEnd_CapsASentenceThatArrivesWhole(t *testing.T) {
	cfg := segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 20}
	text := "one two three four five six seven. Next."
	got := within(t, "nextSegmentEnd", func() segmentEnd {
		end, ok := nextSegmentEnd(text, 0, true, cfg)
		return segmentEnd{end: end, ok: ok}
	})
	assert.True(t, got.ok)
	assert.Equal(t, "one two three four ", text[:got.end], "cut at the last space before the cap")
}

// A cap cut never splits a code fence marker, so the text after the fence is
// still segmented as prose.
func TestNextSegmentEnd_CapCutKeepsTheFenceMarkerWhole(t *testing.T) {
	cfg := segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 10}
	text := "```\n" + strings.Repeat("y", 8) + "```" + " After. More. Done."
	got := segmentsOf(t, cfg, text)
	assert.Equal(t, text, strings.Join(got, ""), "every byte is in a segment, once")
	for _, s := range got {
		assert.NotEqual(t, "``", s[max(len(s)-2, 0):], "a segment ends inside a fence marker: %q", s)
	}
	assert.Equal(t, []string{"```\n", "yyyyyyyy", "``` After.", " More.", " Done."}, got,
		"the closing marker stays whole, and the sentences after it are segments again")
}

// The cap counts whole sentences: one exactly at the cap is a segment whole,
// and with no cap a sentence of any length is.
func TestNextSegmentEnd_SentenceAtTheCapIsWhole(t *testing.T) {
	text := "abcd efgh. Next"
	for _, maxChars := range []int{len("abcd efgh."), 0} {
		cfg := segmentConfig{minChars: 1, firstMinChars: 1, maxChars: maxChars}
		got := within(t, "nextSegmentEnd", func() segmentEnd {
			end, ok := nextSegmentEnd(text, 0, true, cfg)
			return segmentEnd{end: end, ok: ok}
		})
		assert.True(t, got.ok, "maxChars %d", maxChars)
		assert.Equal(t, "abcd efgh.", text[:got.end], "maxChars %d", maxChars)
	}
}

// A cut is moved back only off a fence marker it would split.
func TestOutsideFenceMarker(t *testing.T) {
	text := "abcd```xyz"
	assert.Equal(t, 7, outsideFenceMarker(text, 0, 7), "a marker wholly before the cut stays")
	assert.Equal(t, 4, outsideFenceMarker(text, 0, 5), "a cut inside a marker moves before it")
	assert.Equal(t, 4, outsideFenceMarker(text, 0, 6))
	assert.Equal(t, 7, outsideFenceMarker(text, 4, 5), "a marker at the segment start goes whole into it")
	assert.Equal(t, 3, outsideFenceMarker(text, 0, 3), "no marker, no move")
}
