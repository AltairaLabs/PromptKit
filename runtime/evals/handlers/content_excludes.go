package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// Compile-time interface check for streaming support.
var _ evals.StreamableEvalHandler = (*ContentExcludesHandler)(nil)

const (
	matchModeSubstring    = "substring"
	matchModeWordBoundary = "word_boundary"

	// paramCaseSensitive makes a pattern match only in its own case.
	paramCaseSensitive = "case_sensitive"
)

// ContentExcludesHandler checks that NONE of the assistant messages
// across the full conversation contain any of the forbidden patterns.
// Params:
//   - patterns []string: the forbidden strings
//   - match_mode string: "substring" (default) or "word_boundary"
//   - case_sensitive bool: match each pattern only in its own case (default
//     false: case-insensitive)
type ContentExcludesHandler struct{}

// Type returns the eval type identifier.
func (h *ContentExcludesHandler) Type() string {
	return "content_excludes"
}

// Eval checks all assistant messages for forbidden patterns.
func (h *ContentExcludesHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (_ *evals.EvalResult, _ error) {
	patterns := extractStringSlice(params, "patterns")
	if len(patterns) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(true),
			Explanation: "no patterns to check",
		}, nil
	}

	mode := matchModeSubstring
	if m, ok := params["match_mode"].(string); ok && m != "" {
		mode = m
	}

	matcher := buildMatcher(mode, patterns, extractBool(params, paramCaseSensitive))

	var found []string
	for _, target := range contentUnderTest(evalCtx) {
		for _, p := range patterns {
			if matcher(target.content, p) {
				found = append(found, fmt.Sprintf(
					"turn %d contains %q", target.turn, p,
				))
			}
		}
	}

	if len(found) > 0 {
		return &evals.EvalResult{
			Type:  h.Type(),
			Score: boolScore(false),
			Value: map[string]any{keyViolations: found},
			Explanation: fmt.Sprintf(
				"forbidden content found: %s",
				strings.Join(found, "; "),
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Explanation: "no forbidden content detected",
	}, nil
}

// EvalPartial checks partial streaming content for forbidden patterns.
// Always uses substring mode to avoid false negatives on partial words
// that would occur with word_boundary mode mid-stream.
func (h *ContentExcludesHandler) EvalPartial(
	_ context.Context, content string, params map[string]any,
) (*evals.EvalResult, error) {
	patterns := extractStringSlice(params, "patterns")
	if len(patterns) == 0 {
		return &evals.EvalResult{
			Type:  h.Type(),
			Score: boolScore(true),
		}, nil
	}

	// Use substring mode for streaming to avoid false negatives on partial words.
	matches := substringMatcher(extractBool(params, paramCaseSensitive))
	for _, p := range patterns {
		if matches(content, p) {
			return &evals.EvalResult{
				Type:  h.Type(),
				Score: boolScore(false),
				Value: map[string]any{keyViolations: []string{fmt.Sprintf("contains %q", p)}},
				Explanation: fmt.Sprintf(
					"forbidden content found in stream: %q", p,
				),
			}, nil
		}
	}

	return &evals.EvalResult{
		Type:  h.Type(),
		Score: boolScore(true),
	}, nil
}

// substringMatcher returns the substring match function for the case mode.
func substringMatcher(caseSensitive bool) func(text, pattern string) bool {
	if caseSensitive {
		return strings.Contains
	}
	return containsInsensitive
}

// buildMatcher returns a match function for the given mode and case mode.
// For word_boundary mode, it pre-compiles regexes for all patterns.
func buildMatcher(
	mode string, patterns []string, caseSensitive bool,
) func(text, pattern string) bool {
	if mode != matchModeWordBoundary {
		return substringMatcher(caseSensitive)
	}

	flags := "(?i)"
	if caseSensitive {
		flags = ""
	}
	// Pre-compile word-boundary regexes for each pattern.
	regexMap := make(map[string]*regexp.Regexp, len(patterns))
	for _, p := range patterns {
		re := regexp.MustCompile(
			flags + `\b` + regexp.QuoteMeta(p) + `\b`,
		)
		regexMap[p] = re
	}

	return func(text, pattern string) bool {
		if re, ok := regexMap[pattern]; ok {
			return re.MatchString(text)
		}
		return false
	}
}
