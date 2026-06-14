// Package engine is the deterministic orchestrator core for RFC 0010 workflow
// compositions. It resolves ${...} references against a scope, evaluates the
// constrained predicate language, merges parallel outputs, and walks the step
// DAG. Step side-effects run through an injected StepExecutor, so this package
// imports neither runtime/pipeline nor runtime/providers.
package engine

import (
	"regexp"
	"strings"
)

// Scope holds the composition input plus each completed step's output, shaped for
// ${input.X} and ${stepID.output.X} resolution.
type Scope map[string]any

// refWholeRe matches a string that is exactly a single ${...} reference.
//
//nolint:unused // used by stripRef; later tasks in this package add callers
var refWholeRe = regexp.MustCompile(`^\$\{\s*([a-zA-Z_][a-zA-Z0-9_.]*?)\s*\}$`)

// stripRef returns the inner dotted path of a string that is exactly a ${...}
// reference, and whether the input was such a reference.
//
//nolint:unused // used by resolvePath and the predicate evaluator added in later tasks
func stripRef(s string) (string, bool) {
	m := refWholeRe.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// resolvePath resolves a ${...} reference to its value in scope, walking dotted
// segments through nested maps. Returns (nil, false) if the input is not a
// whole-string reference or any segment is missing.
//
//nolint:unused // used by the predicate evaluator and step executor added in later tasks
func resolvePath(ref string, scope Scope) (any, bool) {
	inner, ok := stripRef(ref)
	if !ok {
		return nil, false
	}
	var cur any = map[string]any(scope)
	for _, seg := range strings.Split(inner, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
