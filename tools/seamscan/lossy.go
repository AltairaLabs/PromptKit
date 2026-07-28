package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// LossyRebuilds reports composite literals that set materially fewer exported
// fields than their struct type has.
//
// This is the shape that dropped types.Message.FinishReason in #1681: a fresh
// Message was built from a narrower pipeline type with four of thirteen fields
// set, so everything else silently became the zero value. Detecting it needs
// resolved types — the full field set of a named struct is not recoverable from
// syntax alone, which is why this cannot be a regex.
//
// minFields suppresses small structs, where partial construction is usually
// deliberate. minDropped suppresses near-complete literals.
//
// The scan deliberately loads only the non-test package (packages.Config.Tests
// is left false): #1681 was production code, and asking go/packages for the
// "[pkg.test]" variant too would parse every production file in the package
// twice (once under the plain package, once under its test variant) and
// double-count every finding in it, on top of surfacing enormous volumes of
// deliberate partial construction in test fixtures that swamp real signal.
//
// # Detection boundary
//
// A literal only qualifies once at least one field value reads existing data
// rather than authoring it fresh (see hasFieldSourcedValue). This is what
// separates a rebuild from an ordinary sparse constructor (e.g. the
// runtime/evals result-builder convention, which sets a couple of fields from
// locals/call results and leaves the rest for a later stage by design — not a
// bug).
//
// What it detects: a field read anywhere in a value's expression subtree, not
// just as the top-level expression — directly (src.Content, &src.Content) or
// nested in a conversion, concatenation, index expression, or sub-literal
// (string(src.A), src.A+"!", src.Items[0], Inner{X: src.A}) — plus any
// slice/array/map index read, regardless of what it indexes.
//
// What it does not detect: a method call whose receiver and arguments contain
// no field or index read anywhere — state.accumulatedContent() and f(x) are
// invisible, even when the callee really is just forwarding a field through a
// getter. Note the converse: because the walk covers the whole subtree
// including a call's receiver chain, x.Field.Method() DOES qualify (the
// receiver reads a field), so some findings are method-call values whose
// result may be unrelated to the field read — resp.Message.GetContent() and
// int(d.turns.Load()) both fire on that basis. A narrow rule counting calls to a
// concrete (non-interface) receiver's method was tried specifically to
// recover the streaming twin of #1681 at sdk/streaming.go:444
// (state.accumulatedContent()), which is exactly that shape. It was reverted:
// receiver concreteness doesn't separate a data-forwarding getter from an
// ordinary computed one (h.Type() returning a hardcoded literal is just as
// concrete-receiver as state.accumulatedContent() reading accumulated state),
// and it drove runtime/hooks+runtime/evals from 14 findings to 186 — the
// eval-handler convention of a hardcoded Type()/Name() getter appears in
// nearly every EvalResult constructor in that package. streaming.go:444 is
// therefore a known false negative, not a bug: recovering it needs either
// call-graph analysis of the callee (does its body actually read a field?)
// or a per-site allowlist, both out of scope for this pass. Also undetected,
// for the same reason (no data-flow tracing here): a value already copied out
// of a field by an earlier statement (x := src.A; T{F: x}), and a parameter
// or local forwarded opaquely.
func LossyRebuilds(patterns []string, minFields, minDropped int) ([]Finding, error) {
	var out []Finding
	for _, pattern := range patterns {
		found, err := lossyRebuildsInPattern(pattern, minFields, minDropped)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

// lossyRebuildsInPattern loads the single pattern (a filesystem path, plain or
// with a "/..." recursive suffix) and reports lossy rebuilds found in it.
//
// Go's module resolution keys off the process working directory, not off the
// pattern string: "go list /abs/path/..." run with the working directory
// fixed at this tool's own module fails with "directory prefix ... does not
// contain main module or its selected dependencies" whenever the pattern
// names some other module entirely — which is every real invocation, since
// this tool's own module is never the thing being scanned. splitPatternDir
// turns the pattern into (Dir, listPattern) so packages.Load's working
// directory sits inside the target itself; from there Go's normal upward
// go.work search does the right thing in both cases this tool needs: a real
// scan target that is part of a multi-module workspace resolves against that
// workspace, and an isolated single-module fixture (as the tests build) falls
// back to its own go.mod because no go.work is reachable above it.
//
// A pattern that matches nothing, or that go/packages could not load at all
// (no reachable go.mod, a malformed module, ...), is treated as an error
// rather than a silent zero findings: go/packages reports a load failure as a
// package whose Errors is non-empty rather than a Go error return, so a caller
// that only checks the err from packages.Load never notices. That is exactly
// how "seams" on the whole repo root used to exit 0 having scanned nothing.
func lossyRebuildsInPattern(pattern string, minFields, minDropped int) ([]Finding, error) {
	dir, listPattern := splitPatternDir(pattern)
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo,
		Dir: dir,
	}
	pkgs, err := packages.Load(cfg, listPattern)
	if err != nil {
		return nil, fmt.Errorf("loading packages for %q: %w", pattern, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages matched %q", pattern)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		return nil, fmt.Errorf("%d package load error(s) for %q (see above)", n, pattern)
	}

	var out []Finding
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.TypesInfo == nil {
			return
		}
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				f, ok := checkLiteral(p, lit, minFields, minDropped)
				if ok {
					out = append(out, f)
				}
				return true
			})
		}
	})
	return out, nil
}

// recursivePattern is the go/packages pattern for "this directory and
// everything under it," used once splitPatternDir has resolved the directory
// to run the query from.
const recursivePattern = "./..."

// splitPatternDir splits a filesystem-path package pattern into a directory
// to run the query from and a pattern relative to that directory. "dir/..."
// becomes ("dir", "./..."); a bare directory becomes (dir, ".").
func splitPatternDir(pattern string) (dir, listPattern string) {
	if rest, ok := strings.CutSuffix(pattern, "/..."); ok {
		if rest == "" {
			rest = "."
		}
		return rest, recursivePattern
	}
	if pattern == "..." {
		return ".", recursivePattern
	}
	return pattern, "."
}

// checkLiteral decides whether one composite literal is a lossy rebuild.
func checkLiteral(
	p *packages.Package, lit *ast.CompositeLit, minFields, minDropped int,
) (Finding, bool) {
	st, name := structType(p.TypesInfo.TypeOf(lit))
	if st == nil {
		return Finding{}, false
	}

	exported := exportedFieldNames(st)
	if len(exported) < minFields {
		return Finding{}, false
	}

	set, positional := setFieldNames(lit)
	if positional {
		// A positional literal sets every field, so it cannot be lossy.
		return Finding{}, false
	}
	if !hasFieldSourcedValue(p, lit) {
		// Nothing here reads existing data — this literal was authored from
		// scratch (constants, locals, call results), the normal shape of a
		// sparse builder/result type where the caller or a later stage is
		// expected to fill in the rest. See the package doc for the exact
		// detection boundary.
		return Finding{}, false
	}

	dropped := droppedFieldNames(exported, set)
	if len(dropped) < minDropped {
		return Finding{}, false
	}

	pos := p.Fset.Position(lit.Pos())
	return Finding{
		Kind:    "lossy-rebuild",
		File:    pos.Filename,
		Line:    pos.Line,
		Subject: fmt.Sprintf("%s (%d/%d exported fields set)", name, len(set), len(exported)),
		Detail:  "not set in literal: " + strings.Join(dropped, ", "),
	}, true
}

// exportedFieldNames returns the set of exported field names on st.
func exportedFieldNames(st *types.Struct) map[string]bool {
	exported := map[string]bool{}
	for i := 0; i < st.NumFields(); i++ {
		if fld := st.Field(i); fld.Exported() {
			exported[fld.Name()] = true
		}
	}
	return exported
}

// setFieldNames returns the field names set by a keyed composite literal. If
// the literal is positional (or empty of keyed elements while non-empty), it
// reports positional=true, since a positional literal sets every field by
// definition.
func setFieldNames(lit *ast.CompositeLit) (set map[string]bool, positional bool) {
	set = map[string]bool{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, true
		}
		if id, ok := kv.Key.(*ast.Ident); ok {
			set[id.Name] = true
		}
	}
	return set, false
}

// hasFieldSourcedValue reports whether any element of a composite literal
// (keyed or positional) is set from an expression that reads existing data —
// a field or index read anywhere in its subtree — as opposed to one authored
// fresh from constants, locals, and call results (method or otherwise). See
// LossyRebuilds' doc for exactly what shapes this does and does not catch.
func hasFieldSourcedValue(p *packages.Package, lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		value := elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			value = kv.Value
		}
		if readsExistingData(p, value) {
			return true
		}
	}
	return false
}

// readsExistingData walks e's entire expression subtree (so a conversion,
// concatenation, index, or nested sub-literal wrapping a field read still
// counts) looking for a field selection or an index expression.
//
// A narrow rule counting method calls on a concrete (non-interface) receiver
// was tried, to recover the streaming.go:444 shape (state.accumulatedContent())
// — see LossyRebuilds' doc for why it was reverted: receiver concreteness
// does not separate a data read from an ordinary computed getter, and the
// measured false-positive cost was far too high.
func readsExistingData(p *packages.Package, e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		switch v := n.(type) {
		case *ast.IndexExpr:
			found = true
			return false
		case *ast.SelectorExpr:
			if sel, ok := p.TypesInfo.Selections[v]; ok && sel.Kind() == types.FieldVal {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// droppedFieldNames returns the sorted names present in exported but absent
// from set.
func droppedFieldNames(exported, set map[string]bool) []string {
	var dropped []string
	for f := range exported {
		if !set[f] {
			dropped = append(dropped, f)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// structType unwraps pointers and named types to the underlying struct,
// returning it with a readable type name.
func structType(t types.Type) (*types.Struct, string) {
	if t == nil {
		return nil, ""
	}
	name := t.String()
	if ptr, ok := t.Underlying().(*types.Pointer); ok {
		t = ptr.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil, ""
	}
	return st, name
}
