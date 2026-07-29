package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
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
//
// # Assignment awareness
//
// A field is not "dropped" merely because the literal omits it: a field
// assigned to the literal's own target afterwards, on every path out of the
// literal, is set. Without this the signature reads identically before and
// after a fix — #1715 added resp.message.FinishReason = ... immediately below
// the literal at sdk/streaming.go:435, and the pre-fix and post-fix trees
// produced the same finding, which is why the true positive went unnoticed
// among 31 findings for sdk alone. See lateAssignedFields for the exact
// suppression rule and why conditional assignments are deliberately not
// suppressed.
//
// # Partial results
//
// A pattern that fails to load does not discard the patterns that succeeded:
// every pattern is scanned, findings from the ones that worked are returned,
// and the failures are joined into the returned error so the caller still
// exits non-zero. Returning nothing on the first bad path is how
// "seams ./sdk ./runtime" used to throw away all of sdk's findings.
func LossyRebuilds(patterns []string, minFields, minDropped int) ([]Finding, error) {
	var (
		out  []Finding
		errs []error
	)
	for _, pattern := range patterns {
		found, err := lossyRebuildsInPattern(pattern, minFields, minDropped)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, found...)
	}
	return out, errors.Join(errs...)
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
	if err := checkBareModuleRoot(pattern, dir, listPattern); err != nil {
		return nil, err
	}
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
			late := lateAssignedFields(p, file)
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				f, ok := checkLiteral(p, lit, late[lit], minFields, minDropped)
				if ok {
					out = append(out, f)
				}
				return true
			})
		}
	})
	return out, nil
}

// checkBareModuleRoot turns the single sharpest edge of the path syntax into
// an actionable message. "seamscan seams ./runtime" loads the directory as one
// package, and a module root that holds only subdirectories has no root-level
// .go files, so go/packages reports "no Go files in ..." — true, but it never
// says that "./runtime/..." is the form that works. Both spellings are
// accepted by splitPatternDir; only one of them scans a module.
//
// It deliberately reports rather than silently rewriting the pattern to
// "<dir>/...": a bare directory means "this one package, non-recursively"
// everywhere else in the tool, and quietly recursing only when the directory
// happens to hold no .go files would make the pattern's meaning depend on the
// contents of the directory it names.
func checkBareModuleRoot(pattern, dir, listPattern string) error {
	if listPattern == recursivePattern {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Unreadable or nonexistent: let packages.Load produce the error.
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return nil
		}
	}
	return fmt.Errorf(
		"no Go files directly in %q, so there is no package to scan; "+
			"to scan the whole tree below it use %q",
		pattern, filepath.Clean(pattern)+"/...")
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

// checkLiteral decides whether one composite literal is a lossy rebuild. late
// is the set of field names assigned to the literal's target after it (see
// lateAssignedFields); those fields are set, so they are not dropped.
func checkLiteral(
	p *packages.Package, lit *ast.CompositeLit, late map[string]bool, minFields, minDropped int,
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

	dropped := droppedFieldNames(exported, set, late)
	if len(dropped) < minDropped {
		return Finding{}, false
	}

	pos := p.Fset.Position(lit.Pos())
	return Finding{
		Kind:    "lossy-rebuild",
		File:    pos.Filename,
		Line:    pos.Line,
		Subject: fmt.Sprintf("%s (%d/%d exported fields set)", name, len(exported)-len(dropped), len(exported)),
		Detail:  "never set: " + strings.Join(dropped, ", "),
	}, true
}

// litSite is a composite literal together with the lvalue it was assigned to
// and the conditional constructs enclosing it.
type litSite struct {
	lit    *ast.CompositeLit
	target string
	conds  []ast.Node
}

// assignSite is a `target.Field = ...` statement: which target, which field,
// where, and the conditional constructs enclosing it.
type assignSite struct {
	target string
	field  string
	pos    token.Pos
	conds  []ast.Node
}

// lateAssignedFields maps each composite literal in file to the field names
// assigned to that literal's own target afterwards, on every path out of the
// literal. Those fields are set, so they are not dropped.
//
// "On every path" is what makes this correct rather than merely convenient.
// The motivating shape (sdk/streaming.go, #1715) assigns the literal inside a
// conditional and repairs the field after it:
//
//	if result != nil && result.Response != nil {
//	    resp.message = &types.Message{Role: ..., Content: ...}   // literal
//	}
//	resp.message.FinishReason = state.finishReason()             // repair
//
// so a "same statement list" rule would miss it. Instead an assignment counts
// only when it sits after the literal and inside no conditional that does not
// already enclose the literal — its conditional ancestors must be a subset of
// the literal's. A field assigned only on some branch therefore stays reported,
// which is the deliberate trade in #1729: a partial rebuild is a real finding,
// and a false positive there is cheaper than a missed dropped field.
//
// Targets are compared by declared object identity, not by name, so assigning
// to a different variable that happens to share a name or type suppresses
// nothing. Only `x = T{...}` / `x := &T{...}` forms are tracked; a literal
// bound by `var x = T{...}` is not, and simply keeps its pre-existing behavior.
func lateAssignedFields(p *packages.Package, file *ast.File) map[*ast.CompositeLit]map[string]bool {
	lits, assigns := collectSites(p, file)
	return matchLateAssignments(lits, assigns)
}

// collectSites walks file once, recording every composite literal bound to an
// lvalue and every `target.Field = ...` assignment, each tagged with the
// conditional constructs enclosing it.
func collectSites(p *packages.Package, file *ast.File) ([]litSite, []assignSite) {
	var (
		lits    []litSite
		assigns []assignSite
		stack   []ast.Node
	)
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		if as, ok := n.(*ast.AssignStmt); ok {
			conds := conditionalAncestors(stack)
			lits = append(lits, litSitesIn(p, as, conds)...)
			assigns = append(assigns, assignSitesIn(p, as, conds)...)
		}
		return true
	})
	return lits, assigns
}

// litSitesIn returns the composite literals this statement binds to an lvalue.
func litSitesIn(p *packages.Package, as *ast.AssignStmt, conds []ast.Node) []litSite {
	var out []litSite
	for i, rhs := range as.Rhs {
		if i >= len(as.Lhs) {
			break
		}
		lit := literalOf(rhs)
		if lit == nil {
			continue
		}
		if key, ok := targetKey(p, as.Lhs[i]); ok {
			out = append(out, litSite{lit: lit, target: key, conds: conds})
		}
	}
	return out
}

// assignSitesIn returns the field assignments this statement performs.
func assignSitesIn(p *packages.Package, as *ast.AssignStmt, conds []ast.Node) []assignSite {
	var out []assignSite
	for _, lhs := range as.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if key, ok := targetKey(p, sel.X); ok {
			out = append(out, assignSite{
				target: key, field: sel.Sel.Name, pos: as.Pos(), conds: conds,
			})
		}
	}
	return out
}

// matchLateAssignments pairs each literal with the field assignments that run
// on every path out of it: same target, later in the file, and inside no
// conditional that does not already enclose the literal.
func matchLateAssignments(lits []litSite, assigns []assignSite) map[*ast.CompositeLit]map[string]bool {
	out := map[*ast.CompositeLit]map[string]bool{}
	for _, ls := range lits {
		for _, as := range assigns {
			if as.target != ls.target || as.pos <= ls.lit.Pos() {
				continue
			}
			if !condsSubset(as.conds, ls.conds) {
				continue
			}
			if out[ls.lit] == nil {
				out[ls.lit] = map[string]bool{}
			}
			out[ls.lit][as.field] = true
		}
	}
	return out
}

// literalOf unwraps `T{...}` and `&T{...}`, returning nil for anything else.
func literalOf(e ast.Expr) *ast.CompositeLit {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	lit, _ := e.(*ast.CompositeLit)
	return lit
}

// targetKey renders an lvalue as a key that distinguishes variables by their
// declared object, so two different variables never compare equal even when
// they share a name. Reports false for anything that is not a chain of
// identifiers and field selections.
func targetKey(p *packages.Package, e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		obj := p.TypesInfo.ObjectOf(v)
		if obj == nil {
			return "", false
		}
		// Declaration position is unique per declared object and stable.
		return fmt.Sprintf("obj@%d", obj.Pos()), true
	case *ast.SelectorExpr:
		base, ok := targetKey(p, v.X)
		if !ok {
			return "", false
		}
		return base + "." + v.Sel.Name, true
	case *ast.StarExpr:
		return targetKey(p, v.X)
	case *ast.ParenExpr:
		return targetKey(p, v.X)
	}
	return "", false
}

// conditionalAncestors returns the nodes on stack that make their body run
// conditionally. A function literal counts: its body may run never, later, or
// many times, so an assignment inside one is not on the path out of a literal
// outside it.
func conditionalAncestors(stack []ast.Node) []ast.Node {
	var conds []ast.Node
	for _, n := range stack {
		switch n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
			*ast.TypeSwitchStmt, *ast.SelectStmt, *ast.CaseClause,
			*ast.CommClause, *ast.FuncLit:
			conds = append(conds, n)
		}
	}
	return conds
}

// condsSubset reports whether every conditional enclosing the assignment also
// encloses the literal — i.e. the assignment introduces no branch of its own
// and therefore runs whenever the literal did.
func condsSubset(assign, lit []ast.Node) bool {
	for _, a := range assign {
		found := false
		for _, l := range lit {
			if a == l {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
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

// droppedFieldNames returns the sorted names present in exported but set
// neither by the literal (set) nor by a later unconditional assignment to the
// literal's target (late).
func droppedFieldNames(exported, set, late map[string]bool) []string {
	var dropped []string
	for f := range exported {
		if set[f] || late[f] {
			continue
		}
		dropped = append(dropped, f)
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
