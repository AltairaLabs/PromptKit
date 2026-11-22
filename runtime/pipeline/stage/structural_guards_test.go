package stage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoBulkWriterInStageConfigs is a structural invariant: no struct
// field declared in **this package** (runtime/pipeline/stage) or in any
// package under it (the stage/internal/** packages its stages move into,
// #2876) may have a type that mentions BulkWriter. Hot-path runtime stages must reach the
// store via MessageAppender, MetadataAccessor, or SummaryAccessor —
// never via the bulk-write escape hatch.
//
// Scope is intentionally limited to the runtime stages. Arena's stages
// (PromptArena's stages) and admin tooling (PromptArena's cmd/) are explicit
// consumers of BulkWriter and live outside this guard. If a parallel
// hot-path stage is added in another directory, this test won't catch
// it — extend the walk to that directory if/when that happens.
//
// Walks the AST rather than reflecting because the latter would miss
// un-instantiated config types.
func TestNoBulkWriterInStageConfigs(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	fset := token.NewFileSet()
	var violations []string

	for _, path := range stageSourceFiles(t, wd) {
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			if st.Fields == nil {
				return true
			}
			for _, field := range st.Fields.List {
				if mentionsBulkWriter(field.Type) {
					pos := fset.Position(field.Pos())
					names := fieldNames(field)
					violations = append(violations,
						pos.String()+": struct "+ts.Name.Name+" field "+names+" references BulkWriter")
				}
			}
			return true
		})
	}

	if len(violations) > 0 {
		t.Fatalf("hot-path stages must not depend on BulkWriter — use MessageAppender / "+
			"MetadataAccessor / SummaryAccessor instead:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// stageSourceFiles returns the non-test Go files of the package in root and of
// every package below it, skipping testdata trees, which hold fixtures rather
// than code.
func stageSourceFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

// mentionsBulkWriter walks a type expression and reports whether any
// identifier in it is "BulkWriter". Catches both `statestore.BulkWriter` and
// `*statestore.BulkWriter`, plus the unqualified form should anyone alias
// the type into the package.
func mentionsBulkWriter(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "BulkWriter" {
			found = true
			return false
		}
		return true
	})
	return found
}

func fieldNames(f *ast.Field) string {
	if len(f.Names) == 0 {
		return "<embedded>"
	}
	parts := make([]string, len(f.Names))
	for i, n := range f.Names {
		parts[i] = n.Name
	}
	return strings.Join(parts, ", ")
}
