package main

import (
	"go/ast"
	"go/parser"
	"go/token"
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
}

// WeakAssertions reports test functions and subtests whose assertions cannot
// distinguish correct behavior from broken behavior.
func WeakAssertions(paths []string) ([]Finding, error) {
	var out []Finding
	for _, root := range paths {
		files, err := goTestFiles(root)
		if err != nil {
			return nil, err
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
	return out, nil
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
		if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
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
			c.testify = append(c.testify, fn.Sel.Name)
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
