package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

// Finding kinds produced by classify/classifyBody.
const (
	kindNoAssertion   = "no-assertion"
	kindWeakAssertion = "weak-assertion"
)

// testifyPackageAlias/testifyRequireAlias are the conventional local names
// testify assertion packages are imported under (assert.Equal,
// require.NoError, ...).
const (
	testifyPackageAlias = "assert"
	testifyRequireAlias = "require"
)

// assertionsProvingNothing are testify calls that establish only that a call
// did not fail. A test whose every assertion is one of these passes regardless
// of whether the code under test produced the right answer.
var assertionsProvingNothing = map[string]bool{
	"NoError": true, "NoErrorf": true,
	"Nil": true, "Nilf": true,
	"NotNil": true, "NotNilf": true,
	provesNothing: true,
}

// provesNothing stands in for a testify call that names a real assertion but
// cannot fail on the code under test: a constant condition, a value compared
// with itself, or a count of calls to a collaborator. See isVacuous and
// countsCalls.
const provesNothing = "<proves-nothing>"

// Minimum argument counts, t included, for a condition assertion
// (assert.True(t, cond)) and a comparison (assert.Equal(t, want, got)).
const (
	conditionArgs  = 2
	comparisonArgs = 3
)

// constantConditions are assertions whose second argument is the condition.
var constantConditions = map[string]bool{
	"True": true, "Truef": true, "False": true, "Falsef": true,
}

// comparisons are assertions whose second and third arguments are compared.
var comparisons = map[string]bool{
	"Equal": true, "Equalf": true, "EqualValues": true, "EqualValuesf": true,
	"Exactly": true, "Exactlyf": true,
}

// isVacuous reports whether a testify call is decided by the test's own
// source, whatever the code under test does: assert.True(t, true), or
// assert.Equal(t, x, x) where both sides are the same expression or both are
// literals.
func isVacuous(name string, args []ast.Expr) bool {
	switch {
	case constantConditions[name] && len(args) >= conditionArgs:
		id, ok := args[1].(*ast.Ident)
		return ok && (id.Name == "true" || id.Name == "false")
	case comparisons[name] && len(args) >= comparisonArgs:
		_, lit1 := args[1].(*ast.BasicLit)
		_, lit2 := args[2].(*ast.BasicLit)
		return (lit1 && lit2) || types.ExprString(args[1]) == types.ExprString(args[2])
	}
	return false
}

// callCountNames are the last selector or identifier names, lowercased, that
// mark a value as a tally of calls to a collaborator: fake.calls,
// prov.CallCount(), len(m.Calls), handlerCalled.
var callCountNames = []string{"calls", "callcount", "numcalls", "invocations", "called", "invoked"}

// domainCallNames end in a callCountNames suffix but name data a caller
// observes: in an LLM library, a response's tool calls are output, not a
// tally of calls to a fake.
var domainCallNames = []string{"toolcalls"}

// collectionAssertions inspect a collection's size. On a bare identifier
// (`calls := ExtractToolCalls(...)`) the name says nothing about where the
// collection came from, so only a field or method of a collaborator counts.
var collectionAssertions = map[string]bool{
	"Len": true, "Lenf": true, "Empty": true, "Emptyf": true, "NotEmpty": true, "NotEmptyf": true,
}

// countsCalls reports whether the value a testify call inspects is a call
// tally. A test that checks only how often a fake was called passes with the
// fake fed garbage, so it pins the implementation's call pattern, not what a
// caller observes.
func countsCalls(name string, args []ast.Expr) bool {
	if len(args) < conditionArgs {
		return false
	}
	if !comparisons[name] {
		return isCallTally(args[1], !collectionAssertions[name])
	}
	// assert.Equal(t, 2, fake.calls): an integer literal against a tally. A
	// slice of recorded requests compared with an expected slice inspects
	// what the fake received, and is a real assertion.
	if len(args) < comparisonArgs {
		return false
	}
	lit, ok := args[1].(*ast.BasicLit)
	return ok && lit.Kind == token.INT && isCallTally(args[2], true)
}

// isCallTally reports whether expr ends in one of callCountNames, looking
// through len(...), calls, and a dereference. bareIdent says whether a plain
// identifier may count; it never does inside len(...), for the reason given
// on collectionAssertions.
func isCallTally(expr ast.Expr, bareIdent bool) bool {
	for {
		switch e := expr.(type) {
		case *ast.CallExpr:
			if id, ok := e.Fun.(*ast.Ident); ok && id.Name == "len" && len(e.Args) == 1 {
				expr, bareIdent = e.Args[0], false
				continue
			}
			expr = e.Fun
		case *ast.StarExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.SelectorExpr:
			return endsInCallCount(e.Sel.Name)
		case *ast.Ident:
			return bareIdent && endsInCallCount(e.Name)
		default:
			return false
		}
	}
}

func endsInCallCount(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range domainCallNames {
		if strings.HasSuffix(lower, suffix) {
			return false
		}
	}
	for _, suffix := range callCountNames {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// WeakAssertions reports test functions and subtests whose assertions cannot
// distinguish correct behavior from broken behavior.
func WeakAssertions(paths []string) ([]Finding, error) {
	var (
		out  []Finding
		errs []error
	)
	for _, root := range paths {
		files, err := goTestFiles(root)
		if err != nil {
			// Keep scanning the remaining roots. Returning here would discard
			// every finding already collected because one path was bad — the
			// same all-or-nothing shape fixed in LossyRebuilds for #1729, and
			// fixing only one of the two would be precisely the sibling
			// divergence that motivated that issue.
			errs = append(errs, err)
			continue
		}
		for _, path := range files {
			found, err := scanTestFile(path)
			if err != nil {
				// A file that does not parse is not our problem to report.
				continue
			}
			out = append(out, found...)
		}
	}
	return out, errors.Join(errs...)
}

// scanTestFile parses a single test file and classifies each of its Test*
// functions.
func scanTestFile(path string) ([]Finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		// TestMain is the package's setup hook, not a test: it has nothing
		// to assert.
		if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Name.Name == "TestMain" {
			continue
		}
		out = append(out, inspectTestFunc(fset, path, fn)...)
	}
	return out, nil
}

// goTestFiles walks root collecting *_test.go paths. root is always a
// caller-supplied analysis target (CLI args or test fixtures), never
// untrusted request input.
//
//nolint:gosec // root is a trusted local analysis path, not request input
func goTestFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, "_test.go") {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

// inspectTestFunc classifies a test function and, recursively, every t.Run
// subtest nested inside it.
func inspectTestFunc(fset *token.FileSet, path string, fn *ast.FuncDecl) []Finding {
	var out []Finding
	classifyBody(fset, path, fn.Name.Name, fn.Pos(), fn.Body, &out)
	return out
}

// subtestCall is one t.Run(name, func(t *testing.T) {...}) found directly
// inside a body — "directly" meaning not itself nested inside some other
// subtest's body discovered at this same level.
type subtestCall struct {
	name string
	pos  token.Pos
	body *ast.BlockStmt
}

// directSubtests finds every t.Run call that is a direct child of body. It
// does not descend into a matched call's own FuncLit body, so a t.Run nested
// two levels deep is reported only once, as a direct child of its immediate
// parent — never double-counted as a child of body too.
func directSubtests(body *ast.BlockStmt) []subtestCall {
	var out []subtestCall
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isTRun(call) || len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[1].(*ast.FuncLit)
		if !ok {
			return true
		}
		out = append(out, subtestCall{name: subtestName(call.Args[0]), pos: call.Pos(), body: lit.Body})
		return false
	})
	return out
}

// classifyBody classifies one test/subtest body and recurses into its own
// direct subtests, appending every finding to out. subject is the full
// slash-joined nesting path (e.g. "TestParent/outer/inner") and pos is the
// position to report the finding at (the t.Run call site, or the function
// position for the top-level test).
//
// A body that hosts subtests of its own is reported only when its own scope
// (excluding those subtest bodies) contains a weak assertion: a parent that
// merely hosts t.Run calls, with no assertion of its own, asserts nothing by
// design and must not be flagged as "no-assertion".
//
// When a body delegates to subtests, any other nested function literal in its
// own scope — a `newFixture := func(t *testing.T) {...}` helper shared by the
// subtests, say — is fixture setup, not the parent's assertion surface: it
// runs once per subtest that calls it, at a point the parent chose to
// delegate away from. Without this, factoring identical fixture code out of
// two subtests into one shared closure (pure DRY, no behavior change) could
// flip the parent from unreported to reported, purely because its one weak
// assertion (say, a `require.NoError` guarding construction) became visible
// to the walk that used to only see it duplicated inside each *subtest* body
// (already skipped). Assertions the parent makes directly in its own
// statements, outside of any nested func literal, still count.
func classifyBody(fset *token.FileSet, path, subject string, pos token.Pos, body *ast.BlockStmt, out *[]Finding) {
	children := directSubtests(body)

	skip := make(map[ast.Node]bool, len(children))
	for _, c := range children {
		skip[c.body] = true
	}

	k := classify(body, skip, len(children) > 0)
	report := k != ""
	if len(children) > 0 {
		report = k == kindWeakAssertion
	}
	if report {
		*out = append(*out, Finding{Kind: k, File: path, Line: fset.Position(pos).Line, Subject: subject})
	}

	for _, c := range children {
		classifyBody(fset, path, subject+"/"+c.name, c.pos, c.body, out)
	}
}

// classify returns kindNoAssertion, kindWeakAssertion, or "" for a body,
// skipping any nested subtest bodies which are reported separately. When
// delegatesToSubtests is true, assertions inside any other nested function
// literal are treated as fixture setup and excluded too — see classifyBody.
func classify(body *ast.BlockStmt, skip map[ast.Node]bool, delegatesToSubtests bool) string {
	testify, native := collectAssertionCalls(body, skip, delegatesToSubtests)

	if native {
		return ""
	}
	if len(testify) == 0 {
		return kindNoAssertion
	}
	for _, name := range testify {
		if !assertionsProvingNothing[name] {
			return ""
		}
	}
	return kindWeakAssertion
}

// collectAssertionCalls walks body, skipping any nested subtest bodies, and
// returns every testify assertion method invoked plus whether a native
// t.Error/t.Fatal call was used. skipFuncLits additionally treats every other
// nested function literal's body as out of scope (fixture helpers, not the
// body's own assertion surface) — see classifyBody.
func collectAssertionCalls(body *ast.BlockStmt, skip map[ast.Node]bool, skipFuncLits bool) ([]string, bool) {
	c := &assertionCollector{body: body, skip: skip, skipFuncLits: skipFuncLits}
	ast.Inspect(body, c.visit)
	return c.testify, c.native
}

// assertionCollector accumulates the assertion calls found while walking a
// single test/subtest body.
type assertionCollector struct {
	body         *ast.BlockStmt
	skip         map[ast.Node]bool
	skipFuncLits bool
	testify      []string
	native       bool
}

// visit is an ast.Inspect callback: it records testify assertion calls,
// native t.Error/t.Fatal calls, and calls to locally-defined assertion
// helpers, stepping over any nested subtest body (and, when skipFuncLits is
// set, over every other nested function literal too — see classify).
func (c *assertionCollector) visit(n ast.Node) bool {
	if n == nil {
		return false
	}
	if c.shouldSkip(n) {
		return false
	}
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return true
	}
	return c.recordCall(call)
}

// shouldSkip reports whether n roots a subtree outside this collector's
// assertion surface: a nested subtest body (reported separately by its own
// classifyBody call) or, when skipFuncLits is set, any other nested function
// literal (fixture setup — see classify).
func (c *assertionCollector) shouldSkip(n ast.Node) bool {
	if b, ok := n.(*ast.BlockStmt); ok && c.skip[b] && b != c.body {
		return true
	}
	_, isFuncLit := n.(*ast.FuncLit)
	return isFuncLit && c.skipFuncLits
}

// recordCall inspects one call expression, recording it as a testify
// assertion, a native (t.Error/t.Fatal or locally-defined helper) assertion,
// or neither. Always returns true so ast.Inspect keeps descending into the
// call's own arguments.
func (c *assertionCollector) recordCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// A bare call like assertCard(t, got) or mustParse(...) verifies
		// something internally (often via t.Fatalf) with no assert./require.
		// call visible here. Treating it as a real assertion may miss a
		// truly weak helper, but treating it as weak/no-assertion risks
		// flagging a legitimate test — for a candidate-generating tool, the
		// false positive is the worse failure mode.
		if isAssertionHelperName(fn.Name) {
			c.native = true
		}
	case *ast.SelectorExpr:
		pkg, ok := fn.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch pkg.Name {
		case testifyPackageAlias, testifyRequireAlias:
			name := fn.Sel.Name
			if isVacuous(name, call.Args) || countsCalls(name, call.Args) {
				name = provesNothing
			}
			c.testify = append(c.testify, name)
		case "t":
			if strings.HasPrefix(fn.Sel.Name, "Error") || strings.HasPrefix(fn.Sel.Name, "Fatal") {
				c.native = true
			}
		}
	}
	return true
}

// assertionHelperPrefixes are name prefixes (checked case-insensitively)
// that mark a bare function call as a local assertion helper rather than
// ordinary test logic.
var assertionHelperPrefixes = []string{
	testifyPackageAlias, testifyRequireAlias, "check", "verify", "must",
}

// isAssertionHelperName reports whether name looks like a locally-defined
// assertion helper (assertCard, requireThing, checkThing, verifyThing,
// mustThing, ...).
func isAssertionHelperName(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range assertionHelperPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func isTRun(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Run" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "t"
}

// subtestName extracts a readable name from a t.Run first argument.
func subtestName(arg ast.Expr) string {
	if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		return strings.Trim(lit.Value, `"`)
	}
	return "?"
}
