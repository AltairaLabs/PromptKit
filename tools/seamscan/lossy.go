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
		// Nothing here reads a field off some other value — this literal was
		// authored from scratch (constants, locals, call results), the normal
		// shape of a sparse builder/result type where the caller or a later
		// stage is expected to fill in the rest. #1681 forwarded fields read
		// off an existing value of a related type (result.Response.Content);
		// requiring that shape is what tells a rebuild-in-progress apart from
		// an ordinary constructor.
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
		Detail:  "unset: " + strings.Join(dropped, ", "),
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

// hasFieldSourcedValue reports whether any field in a keyed composite literal
// is set from a plain field read on some other value (an expression like
// src.Content, seen through any number of & / * / parens). A literal built
// entirely from constants, bare locals, call results, and package-qualified
// names (enum constants, http.MethodGet, ...) was authored fresh; one that
// forwards a field from elsewhere is, structurally, a rebuild.
func hasFieldSourcedValue(p *packages.Package, lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if ok && isFieldRead(p, kv.Value) {
			return true
		}
	}
	return false
}

// isFieldRead unwraps &x, *x, and (x) to see whether the expression underneath
// selects a struct field — as opposed to a method value or a package-qualified
// name, both of which are also *ast.SelectorExpr but read nothing off a value
// in scope.
func isFieldRead(p *packages.Package, e ast.Expr) bool {
	for {
		switch v := e.(type) {
		case *ast.UnaryExpr:
			e = v.X
		case *ast.StarExpr:
			e = v.X
		case *ast.ParenExpr:
			e = v.X
		case *ast.SelectorExpr:
			sel, ok := p.TypesInfo.Selections[v]
			return ok && sel.Kind() == types.FieldVal
		default:
			return false
		}
	}
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
