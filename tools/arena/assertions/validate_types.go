package assertions

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// fuzzyMatchThresholdDivisor controls how close a suggestion must be.
// Threshold = len(input)/divisor + fuzzyMatchThresholdBase.
const (
	fuzzyMatchThresholdDivisor = 2
	fuzzyMatchThresholdBase    = 3
)

// ValidateAssertionTypes checks all assertion types in loaded scenarios against
// the eval handler registry. Returns a list of human-readable error strings for
// unknown types, each with a "did you mean?" suggestion when a close match exists.
func ValidateAssertionTypes(scenarios map[string]*config.Scenario, registry *evals.EvalTypeRegistry) []string {
	if len(scenarios) == 0 {
		return nil
	}

	registeredTypes := registry.Types()
	var errs []string

	for name, scenario := range scenarios {
		errs = collectScenarioErrors(name, scenario, registry, registeredTypes, errs)
	}

	sort.Strings(errs)
	return errs
}

func collectScenarioErrors(
	name string, scenario *config.Scenario,
	registry *evals.EvalTypeRegistry, registeredTypes []string,
	errs []string,
) []string {
	for _, a := range scenario.ConversationAssertions {
		if a.Type != "" && !registry.Has(a.Type) {
			errs = append(errs, formatUnknownType(name, "conversation_assertions", a.Type, registeredTypes))
		}
	}
	for i := range scenario.Turns {
		for _, a := range scenario.Turns[i].Assertions {
			if a.Type != "" && !registry.Has(a.Type) {
				loc := fmt.Sprintf("turns[%d].assertions", i)
				errs = append(errs, formatUnknownType(name, loc, a.Type, registeredTypes))
			}
		}
	}
	return errs
}

func formatUnknownType(scenario, location, badType string, registered []string) string {
	msg := fmt.Sprintf("%s: %s: unknown assertion type %q", scenario, location, badType)
	if suggestion := closestMatch(badType, registered); suggestion != "" {
		msg += fmt.Sprintf(" (did you mean %q?)", suggestion)
	}
	return msg
}

// closestMatch returns the registered type with the smallest edit distance to
// the input, or "" if no type is within a reasonable threshold.
func closestMatch(input string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	input = strings.ToLower(input)
	best := ""
	bestDist := len(input)/fuzzyMatchThresholdDivisor + fuzzyMatchThresholdBase
	for _, c := range candidates {
		d := levenshtein(input, strings.ToLower(c))
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := range len(a) {
		curr := make([]int, len(b)+1)
		curr[0] = i + 1
		for j := range len(b) {
			cost := 1
			if a[i] == b[j] {
				cost = 0
			}
			curr[j+1] = min(curr[j]+1, min(prev[j+1]+1, prev[j]+cost))
		}
		prev = curr
	}
	return prev[len(b)]
}
