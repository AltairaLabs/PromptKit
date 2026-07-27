package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
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

// inspectTestFunc classifies a test function and each of its subtests.
func inspectTestFunc(fset *token.FileSet, path string, fn *ast.FuncDecl) []Finding {
	var out []Finding

	// Subtests first, and record their bodies so the parent is judged on the
	// statements that are actually its own.
	subBodies := map[ast.Node]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isTRun(call) || len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[1].(*ast.FuncLit)
		if !ok {
			return true
		}
		subBodies[lit.Body] = true
		name := fn.Name.Name + "/" + subtestName(call.Args[0])
		if k := classify(lit.Body, subBodies); k != "" {
			out = append(out, Finding{
				Kind:    k,
				File:    path,
				Line:    fset.Position(call.Pos()).Line,
				Subject: name,
			})
		}
		return true
	})

	// The parent is only reported when it has no subtests of its own; a parent
	// that exists purely to host t.Run calls asserts nothing by design.
	if len(subBodies) == 0 {
		if k := classify(fn.Body, nil); k != "" {
			out = append(out, Finding{
				Kind:    k,
				File:    path,
				Line:    fset.Position(fn.Pos()).Line,
				Subject: fn.Name.Name,
			})
		}
	}
	return out
}

// classify returns "no-assertion", "weak-assertion", or "" for a body,
// skipping any nested subtest bodies which are reported separately.
func classify(body *ast.BlockStmt, skip map[ast.Node]bool) string {
	testify, native := collectAssertionCalls(body, skip)

	if native {
		return ""
	}
	if len(testify) == 0 {
		return "no-assertion"
	}
	for _, name := range testify {
		if !assertionsProvingNothing[name] {
			return ""
		}
	}
	return "weak-assertion"
}

// collectAssertionCalls walks body, skipping any nested subtest bodies, and
// returns every testify assertion method invoked plus whether a native
// t.Error/t.Fatal call was used.
func collectAssertionCalls(body *ast.BlockStmt, skip map[ast.Node]bool) ([]string, bool) {
	c := &assertionCollector{body: body, skip: skip}
	ast.Inspect(body, c.visit)
	return c.testify, c.native
}

// assertionCollector accumulates the assertion calls found while walking a
// single test/subtest body.
type assertionCollector struct {
	body    *ast.BlockStmt
	skip    map[ast.Node]bool
	testify []string
	native  bool
}

// visit is an ast.Inspect callback: it records testify assertion calls and
// native t.Error/t.Fatal calls, stepping over any nested subtest body.
func (c *assertionCollector) visit(n ast.Node) bool {
	if n == nil {
		return false
	}
	if b, ok := n.(*ast.BlockStmt); ok && c.skip[b] && b != c.body {
		return false
	}
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return true
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return true
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return true
	}
	switch pkg.Name {
	case "assert", "require":
		c.testify = append(c.testify, sel.Sel.Name)
	case "t":
		if strings.HasPrefix(sel.Sel.Name, "Error") || strings.HasPrefix(sel.Sel.Name, "Fatal") {
			c.native = true
		}
	}
	return true
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
