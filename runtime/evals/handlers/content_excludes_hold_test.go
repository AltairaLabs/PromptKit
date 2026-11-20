package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// PartialHold names the tail of streamed content that could still become a
// forbidden pattern, so a guardrail keeps it from the caller until the next
// chunk shows whether it does.
func TestContentExcludesHandler_PartialHold(t *testing.T) {
	pineapple := map[string]any{"patterns": []any{"pineapple"}}
	cases := []struct {
		name    string
		content string
		params  map[string]any
		want    int
	}{
		{"one letter of the pattern", "Say P", pineapple, 1},
		{"several letters", "Say Pine", pineapple, 4},
		{"all but the last letter", "Say pineappl", pineapple, 8},
		{"the start, in another case", "Say pINEap", pineapple, 6},
		{"text that rules the pattern out", "Say Pine cones", pineapple, 0},
		{"no part of the pattern", "Say hello", pineapple, 0},
		{"the whole content is the start", "pin", pineapple, 3},
		{"empty content", "", pineapple, 0},
		{"no patterns", "Say P", map[string]any{}, 0},
		{"a one-letter pattern is never held", "Say a", map[string]any{"patterns": []any{"a"}}, 0},
		{
			"case_sensitive does not hold another case", "Say P",
			map[string]any{"patterns": []any{"pineapple"}, "case_sensitive": true}, 0,
		},
		{
			"case_sensitive holds its own case", "Say p",
			map[string]any{"patterns": []any{"pineapple"}, "case_sensitive": true}, 1,
		},
		{
			"the longest hold of several patterns", "I like ban",
			map[string]any{"patterns": []any{"bandana", "nana", "an"}}, 3,
		},
		{"a multibyte start", "un é", map[string]any{"patterns": []any{"éclair"}}, 2},
		{"a multibyte start in another case", "un É", map[string]any{"patterns": []any{"éclair"}}, 2},
	}
	h := &ContentExcludesHandler{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, h.PartialHold(tc.content, tc.params))
		})
	}
}
