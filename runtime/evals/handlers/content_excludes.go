package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// Compile-time interface check for streaming support.
var _ evals.StreamableEvalHandler = (*ContentExcludesHandler)(nil)

const (
	matchModeSubstring    = "substring"
	matchModeWordBoundary = "word_boundary"
)

// ContentExcludesHandler checks that NONE of the assistant messages
// across the full conversation contain any of the forbidden patterns.
// Params: patterns []string (case-insensitive matching).
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

	matcher := buildMatcher(mode, patterns)

	var found []string
	for i := range evalCtx.Messages {
		msg := &evalCtx.Messages[i]
		if !strings.EqualFold(msg.Role, roleAssistant) {
			continue
		}
		content := msg.GetContent()
		for _, p := range patterns {
			if matcher(content, p) {
				found = append(found, fmt.Sprintf(
					"turn %d contains %q", i, p,
				))
			}
		}
	}

	if len(found) > 0 {
		return &evals.EvalResult{
			Type:  h.Type(),
			Score: boolScore(false),
			Value: map[string]any{"violations": found},
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
	for _, p := range patterns {
		if containsInsensitive(content, p) {
			return &evals.EvalResult{
				Type:  h.Type(),
				Score: boolScore(false),
				Value: map[string]any{"violations": []string{fmt.Sprintf("contains %q", p)}},
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

// buildMatcher returns a match function for the given mode.
// For word_boundary mode, it pre-compiles regexes for all patterns.
func buildMatcher(
	mode string, patterns []string,
) func(text, pattern string) bool {
	if mode != matchModeWordBoundary {
		return containsInsensitive
	}

	// Pre-compile word-boundary regexes for each pattern.
	regexMap := make(map[string]*regexp.Regexp, len(patterns))
	for _, p := range patterns {
		re := regexp.MustCompile(
			`(?i)\b` + regexp.QuoteMeta(p) + `\b`,
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
