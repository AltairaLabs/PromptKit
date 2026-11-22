package stage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The guards in this file keep the split of this package into
// stage/internal/<name> packages (#2876) honest. They ask the go command about
// the package graph, so they cost a `go list` (and, for the alias guard, a
// compile of this package's export data) per run.
//
// The Mutation Gate reruns this package's tests once per mutant, and these
// guards check the shape of the API and the import graph, which no mutant of a
// condition or an operator can change. scripts/check-mutation-diff.sh sets
// PROMPTKIT_MUTATION_RUN so they stay out of that bill.
func skipUnderMutationRun(t *testing.T) {
	t.Helper()
	if os.Getenv("PROMPTKIT_MUTATION_RUN") != "" {
		t.Skip("API and import-graph guard: no mutant can change its result")
	}
}

// stagePackagePath is this package's import path.
var stagePackagePath = reflect.TypeFor[StageError]().PkgPath()

// TestInternalTypesReachableFromAPIHaveAliases fails when the exported API of
// this package reaches a type declared under stage/internal/ that this package
// does not alias under the same name.
//
// A consumer cannot import an internal package, so a type it can only reach
// (a field, a parameter, a result) but not name breaks code that declares a
// variable of it. The API-compatibility allowlist accepts apidiff's "changed
// from X to .../stage/internal/<pkg>.X" for a moved type BECAUSE this test
// guarantees stage.X still names it (scripts/api-compat-allowlist.sh).
func TestInternalTypesReachableFromAPIHaveAliases(t *testing.T) {
	skipUnderMutationRun(t)
	pkg := loadExportedTypes(t, stagePackagePath)
	if got := internalAliasViolations(pkg); len(got) > 0 {
		t.Fatalf("types under %s/internal/ reachable from the exported API need a same-named alias "+
			"here (type X = <pkg>.X):\n  %s", stagePackagePath, strings.Join(got, "\n  "))
	}
}

// TestInternalAliasViolations_ReportsEachUnaliasedType drives the checker over a
// stage-shaped package pair, so a checker that stops looking cannot leave the
// real guard above passing on its own.
func TestInternalAliasViolations_ReportsEachUnaliasedType(t *testing.T) {
	core := `package core
type Aliased struct {
	Next   *Nested     // reached through an aliased type's field
	hidden Unexported  // unexported field: unreachable
}
type Nested struct{}
type Unexported struct{}
type Returned struct{}
type Defined struct{}
type Iface interface{ Deep() []Deep }
type Deep int
type Elem struct{}
func (Elem) Method() *Via { return nil }
type Via struct{}
type Unused struct{}
type Renamed struct{}
`
	stage := `package stage
import "example.com/stage/internal/core"
type Aliased = core.Aliased
type Nested = core.Nested
type Iface = core.Iface
type Elem = core.Elem
type Defined core.Defined           // a defined type is not an alias
type Renamed = core.Nested          // the right name, the wrong target
func Returns() core.Returned        { return core.Returned{} }
func TakesDefined(core.Defined)     {}
func TakesRenamed(*core.Renamed)    {}
func unexported() core.Unused       { return core.Unused{} }
var Value core.Elem
`
	pkg := typeCheckPair(t, map[string]string{
		"example.com/stage/internal/core": core,
		"example.com/stage":               stage,
	})

	got := internalAliasViolations(pkg)
	want := []string{
		"core.Deep (reached via Iface) has no alias stage.Deep",
		"core.Defined (reached via TakesDefined) has no alias stage.Defined",
		"core.Renamed (reached via TakesRenamed) has no alias stage.Renamed",
		"core.Returned (reached via Returns) has no alias stage.Returned",
		"core.Via (reached via Elem) has no alias stage.Via",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations:\n got  %q\n want %q", got, want)
	}
}

// TestInternalPackagesDoNotImportStage fails when a package under
// stage/internal/, or any of its test files, depends on this package.
//
// The point of the split is that a mutant in an internal package recompiles
// that package and its own tests only. One internal test importing stage
// brings all of stage back into every one of those mutants' builds.
func TestInternalPackagesDoNotImportStage(t *testing.T) {
	skipUnderMutationRun(t)
	got := internalPackagesDependingOn(t, ".", stagePackagePath)
	if len(got) > 0 {
		t.Fatalf("packages under internal/ must not depend on %s (move the shared helper into "+
			"internal/core or internal/stagetest instead):\n  %s", stagePackagePath, strings.Join(got, "\n  "))
	}
}

// TestInternalPackagesDependingOn_FindsTestOnlyImport runs the same go list
// query over a throwaway module whose internal package imports its parent only
// from an external test file, the case a plain import graph misses.
func TestInternalPackagesDependingOn_FindsTestOnlyImport(t *testing.T) {
	skipUnderMutationRun(t)
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":                    "module example.com/m\n\ngo 1.26\n",
		"stage/stage.go":            "package stage\n\nconst X = 1\n",
		"stage/internal/clean/c.go": "package clean\n",
		"stage/internal/dirty/d.go": "package dirty\n",
		"stage/internal/dirty/d_test.go": "package dirty_test\n\nimport (\n\t\"testing\"\n\n" +
			"\t\"example.com/m/stage\"\n)\n\nfunc TestX(t *testing.T) { _ = stage.X }\n",
	}
	for name, src := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := internalPackagesDependingOn(t, filepath.Join(dir, "stage"), "example.com/m/stage")
	want := []string{"example.com/m/stage/internal/dirty.test", "example.com/m/stage/internal/dirty_test " +
		"[example.com/m/stage/internal/dirty.test]"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dependents:\n got  %q\n want %q", got, want)
	}
}

// internalPackagesDependingOn lists the packages under dir/internal/, test
// variants included, whose transitive dependencies include target.
func internalPackagesDependingOn(t *testing.T, dir, target string) []string {
	t.Helper()
	out := goCommand(t, dir, "list", "-test", "-e",
		"-f", `{{.ImportPath}}{{range .Deps}}|{{.}}{{end}}`, "./internal/...")
	var dependents []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		for _, dep := range fields[1:] {
			if dep == target {
				dependents = append(dependents, fields[0])
				break
			}
		}
	}
	sort.Strings(dependents)
	return dependents
}

// goCommand runs the go command in dir, outside any workspace, and returns its
// stdout. A pattern that matches no packages is a warning on stderr, not an
// error.
func goCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	// runtime is a leaf module (scripts/check-runtime-is-leaf.sh), so it
	// resolves on its own; the throwaway module in the tests has no workspace.
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out)
}

// loadExportedTypes type-checks path from the export data the go command
// builds for it and its dependencies.
func loadExportedTypes(t *testing.T, path string) *types.Package {
	t.Helper()
	out := goCommand(t, ".", "list", "-export", "-deps", "-json=ImportPath,Export", path)
	exports := map[string]string{}
	dec := json.NewDecoder(strings.NewReader(out))
	for {
		var p struct{ ImportPath, Export string }
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		exports[p.ImportPath] = p.Export
	}
	imp := importer.ForCompiler(token.NewFileSet(), "gc", func(p string) (io.ReadCloser, error) {
		file, ok := exports[p]
		if !ok || file == "" {
			return nil, fmt.Errorf("no export data for %s", p)
		}
		return os.Open(file) //nolint:gosec // a path the go command just printed
	})
	pkg, err := imp.Import(path)
	if err != nil {
		t.Fatalf("import %s: %v", path, err)
	}
	return pkg
}

// typeCheckPair type-checks in-memory packages that import only each other
// and returns the one named example.com/stage.
func typeCheckPair(t *testing.T, sources map[string]string) *types.Package {
	t.Helper()
	fset := token.NewFileSet()
	checked := map[string]*types.Package{}
	var check func(path string) (*types.Package, error)
	check = func(path string) (*types.Package, error) {
		if p, ok := checked[path]; ok {
			return p, nil
		}
		src, ok := sources[path]
		if !ok {
			return nil, fmt.Errorf("unknown package %s", path)
		}
		f, err := parser.ParseFile(fset, path+".go", src, 0)
		if err != nil {
			return nil, err
		}
		conf := types.Config{Importer: importerFunc(check)}
		p, err := conf.Check(path, fset, []*ast.File{f}, nil)
		if err != nil {
			return nil, err
		}
		checked[path] = p
		return p, nil
	}
	pkg, err := check("example.com/stage")
	if err != nil {
		t.Fatalf("type-check: %v", err)
	}
	return pkg
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// internalAliasViolations returns, sorted, one line per named type declared
// under pkg's internal/ tree that pkg's exported API reaches but pkg does not
// alias under the same name.
func internalAliasViolations(pkg *types.Package) []string {
	w := &apiWalker{
		own:     pkg.Path(),
		seen:    map[types.Type]bool{},
		reached: map[*types.TypeName]string{},
	}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		if obj := scope.Lookup(name); obj.Exported() {
			w.walk(obj.Type(), name)
		}
	}

	internal := pkg.Path() + "/internal/"
	var violations []string
	for tn, via := range w.reached {
		if tn.Pkg() == nil || !strings.HasPrefix(tn.Pkg().Path(), internal) {
			continue
		}
		if aliasesTo(scope.Lookup(tn.Name()), tn) {
			continue
		}
		violations = append(violations, fmt.Sprintf("%s.%s (reached via %s) has no alias %s.%s",
			tn.Pkg().Name(), tn.Name(), via, pkg.Name(), tn.Name()))
	}
	sort.Strings(violations)
	return violations
}

// aliasesTo reports whether obj is an alias whose target is the type tn names.
func aliasesTo(obj types.Object, tn *types.TypeName) bool {
	alias, ok := obj.(*types.TypeName)
	if !ok || !alias.IsAlias() {
		return false
	}
	named, ok := types.Unalias(alias.Type()).(*types.Named)
	return ok && named.Origin().Obj() == tn
}

// apiWalker records every named type reachable from an exported API: through
// signatures, exported and embedded fields, exported methods and type
// arguments. It descends into the named types of the package itself and of its
// internal/ tree only; another package's API is that package's business.
type apiWalker struct {
	own     string
	seen    map[types.Type]bool
	reached map[*types.TypeName]string
}

func (w *apiWalker) walk(t types.Type, via string) {
	if t == nil || w.seen[t] {
		return
	}
	w.seen[t] = true
	switch t := t.(type) {
	case *types.Alias:
		w.walk(types.Unalias(t), via)
	case *types.Named:
		w.walkNamed(t, via)
	case *types.Pointer:
		w.walk(t.Elem(), via)
	case *types.Slice:
		w.walk(t.Elem(), via)
	case *types.Array:
		w.walk(t.Elem(), via)
	case *types.Chan:
		w.walk(t.Elem(), via)
	case *types.Map:
		w.walk(t.Key(), via)
		w.walk(t.Elem(), via)
	case *types.Signature:
		w.walkTuple(t.Params(), via)
		w.walkTuple(t.Results(), via)
	case *types.Struct:
		for i := range t.NumFields() {
			if f := t.Field(i); f.Exported() || f.Embedded() {
				w.walk(f.Type(), via)
			}
		}
	case *types.Interface:
		for i := range t.NumMethods() {
			if m := t.Method(i); m.Exported() {
				w.walk(m.Type(), via)
			}
		}
	case *types.TypeParam:
		w.walk(t.Constraint(), via)
	case *types.Union:
		for i := range t.Len() {
			w.walk(t.Term(i).Type(), via)
		}
	}
}

func (w *apiWalker) walkNamed(t *types.Named, via string) {
	obj := t.Origin().Obj()
	if _, ok := w.reached[obj]; !ok {
		w.reached[obj] = via
	}
	for i := range t.TypeArgs().Len() {
		w.walk(t.TypeArgs().At(i), via)
	}
	if obj.Pkg() == nil || (obj.Pkg().Path() != w.own && !strings.HasPrefix(obj.Pkg().Path(), w.own+"/internal/")) {
		return
	}
	w.walk(t.Underlying(), via)
	for i := range t.NumMethods() {
		if m := t.Method(i); m.Exported() {
			w.walk(m.Type(), via)
		}
	}
}

func (w *apiWalker) walkTuple(tuple *types.Tuple, via string) {
	for i := range tuple.Len() {
		w.walk(tuple.At(i).Type(), via)
	}
}
