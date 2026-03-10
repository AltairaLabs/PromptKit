package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

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
			Passed:      true,
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
			Type:   h.Type(),
			Passed: false,
			Score:  boolScore(false),
			Value:  map[string]any{"violations": found},
			Explanation: fmt.Sprintf(
				"forbidden content found: %s",
				strings.Join(found, "; "),
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Score:       boolScore(true),
		Explanation: "no forbidden content detected",
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
