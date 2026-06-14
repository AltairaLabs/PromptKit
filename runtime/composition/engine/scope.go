// Package engine is the deterministic orchestrator core for RFC 0010 workflow
// compositions. It resolves ${...} references against a scope, evaluates the
// constrained predicate language, merges parallel outputs, and walks the step
// DAG. Step side-effects run through an injected StepExecutor, so this package
// imports neither runtime/pipeline nor runtime/providers.
package engine

import (
	"fmt"
	"regexp"
	"strings"
)

// Scope holds the composition input plus each completed step's output, shaped for
// ${input.X} and ${stepID.output.X} resolution.
type Scope map[string]any

// refWholeRe matches a string that is exactly a single ${...} reference.
//
//nolint:unused // transitively called by resolveInput; wired to non-test callers in Task 5
var refWholeRe = regexp.MustCompile(`^\$\{\s*([a-zA-Z_][a-zA-Z0-9_.]*?)\s*\}$`)

// stripRef returns the inner dotted path of a string that is exactly a ${...}
// reference, and whether the input was such a reference.
//
//nolint:unused // transitively called by resolveInput; wired to non-test callers in Task 5
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
//nolint:unused // transitively called by resolveInput; wired to non-test callers in Task 5
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

// embeddedRefRe finds ${...} references anywhere inside a string (for interpolation).
//
//nolint:unused // transitively called by resolveInput; wired to non-test callers in Task 5
var embeddedRefRe = regexp.MustCompile(`\$\{\s*[a-zA-Z_][a-zA-Z0-9_.]*?\s*\}`)

// resolveInputString handles the string branch of resolveInput: a pure whole-string
// reference is returned as its typed value; otherwise embedded references are
// interpolated as text.
//
//nolint:unused // called only by resolveInput; wired to non-test callers in Task 5
func resolveInputString(v string, scope Scope) (any, error) {
	if inner, ok := stripRef(v); ok {
		val, ok := resolvePath("${"+inner+"}", scope)
		if !ok {
			return nil, fmt.Errorf("unresolved reference %q", v)
		}
		return val, nil
	}
	var resolveErr error
	out := embeddedRefRe.ReplaceAllStringFunc(v, func(ref string) string {
		val, ok := resolvePath(ref, scope)
		if !ok {
			resolveErr = fmt.Errorf("unresolved reference %q", ref)
			return ref
		}
		return fmt.Sprint(val)
	})
	if resolveErr != nil {
		return nil, resolveErr
	}
	return out, nil
}

// resolveInput resolves ${...} references inside a StepInput/Args value:
//   - a whole-string reference returns the referenced value with its original type
//   - a string containing embedded references interpolates them as text
//   - maps and slices are resolved recursively
//   - any other value passes through unchanged
//
// An unresolvable reference returns an error.
//
//nolint:unused // wired in by the engine scheduler in Task 5
func resolveInput(in any, scope Scope) (any, error) {
	switch v := in.(type) {
	case string:
		return resolveInputString(v, scope)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			rv, err := resolveInput(val, scope)
			if err != nil {
				return nil, err
			}
			out[k] = rv
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			rv, err := resolveInput(val, scope)
			if err != nil {
				return nil, err
			}
			out[i] = rv
		}
		return out, nil
	default:
		return in, nil
	}
}
