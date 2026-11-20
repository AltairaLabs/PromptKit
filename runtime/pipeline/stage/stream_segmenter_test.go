package stage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
			name: "cap with no space cuts on a rune boundary", text: strings.Repeat("中", 10),
			cfg: segmentConfig{minChars: 1, firstMinChars: 1, maxChars: 10}, want: 9, wantOK: true,
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
			end, ok := nextSegmentEnd(tc.text, tc.from, tc.first, tc.cfg)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, end)
		})
	}
}
