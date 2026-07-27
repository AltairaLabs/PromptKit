package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeModule writes a single-package module to a temp dir and returns its path.
// go/packages needs a real module to resolve types.
func writeModule(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.26.0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o600))
	return dir
}

func TestLossyRebuilds_ReportsDroppedFields(t *testing.T) {
	dir := writeModule(t, `package m

type Wide struct {
	A, B, C, D, E, F string
}

func Rebuild(src Wide) Wide {
	return Wide{A: src.A, B: src.B}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "lossy-rebuild", got[0].Kind)
	assert.Contains(t, got[0].Subject, "Wide")
	// The point of the finding is naming what was dropped.
	for _, f := range []string{"C", "D", "E", "F"} {
		assert.Contains(t, got[0].Detail, f)
	}
}

// A literal setting every field is not lossy, however many fields there are.
//
// The literal must still contain a field-sourced value (s.A, s.B) so this
// exercises the completeness check itself rather than being suppressed
// earlier by hasFieldSourcedValue: a mutation that made droppedFieldNames
// ignore what the literal actually sets (reporting every exported field as
// dropped regardless) must turn this from empty to non-empty.
func TestLossyRebuilds_IgnoresCompleteLiteral(t *testing.T) {
	dir := writeModule(t, `package m

type Wide struct {
	A, B, C, D string
}

func Build(s Wide) Wide {
	return Wide{A: s.A, B: s.B, C: "c", D: "d"}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Small structs are noise: a 3-field struct with 1 field set is usually fine.
//
// Trio has exactly 3 exported fields, one below the minFields=4 threshold
// used here, but enough that dropping 2 of them (B, C) clears minDropped=2 —
// unlike a 2-field Pair, which can never drop 2 fields and so cannot
// distinguish "the minFields gate ran" from "the minFields gate was deleted."
// The literal must contain a field-sourced value (s.A) so this test is not
// short-circuited by hasFieldSourcedValue before the gate under test runs.
func TestLossyRebuilds_IgnoresSmallStructs(t *testing.T) {
	dir := writeModule(t, `package m

type Trio struct {
	A, B, C string
}

func Build(s Trio) Trio {
	return Trio{A: s.A}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Unexported fields are not the caller's business and must not count.
//
// All four exported fields are set (one of them, A, via a field-sourced
// value so the literal isn't excluded before this check runs), so the
// correct result is empty. A mutation that counted the three unexported
// fields (e, f, g) as exported would make them look dropped and produce a
// finding.
func TestLossyRebuilds_IgnoresUnexportedFields(t *testing.T) {
	dir := writeModule(t, `package m

type Mixed struct {
	A, B, C, D string
	e, f, g    string
}

func Build(s Mixed) Mixed {
	return Mixed{A: s.A, B: s.B, C: "c", D: "d"}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err, "all four exported fields are set; unexported ones are irrelevant")
	assert.Empty(t, got, "all four exported fields are set; unexported ones are irrelevant")
}

// Positional literals set every field by definition — but only because
// setFieldNames reports positional=true and checkLiteral returns before ever
// computing dropped fields. The literal here uses field-sourced values
// (s.A, s.B) so hasFieldSourcedValue would say yes if reached: if the
// positional gate is deleted, checkLiteral falls through to
// droppedFieldNames(exported, nil) — since setFieldNames returns a nil set
// for a positional literal — which treats every exported field as dropped
// and reports a spurious 100%-dropped finding on a fully-specified literal.
func TestLossyRebuilds_IgnoresPositionalLiteral(t *testing.T) {
	dir := writeModule(t, `package m

type Wide struct {
	A, B, C, D string
}

func Build(s Wide) Wide {
	return Wide{s.A, s.B, "c", "d"}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestLossyRebuilds_DetectsTheMessageRebuildShape reproduces the #1681 defect
// in miniature: a wide struct rebuilt from a narrower source with most fields
// left zero. The live acceptance run against commit 53441944 is documented in
// the plan; this keeps the signature honest afterwards.
func TestLossyRebuilds_DetectsTheMessageRebuildShape(t *testing.T) {
	dir := writeModule(t, `package m

type Message struct {
	Role         string
	Content      string
	Parts        []string
	CostInfo     *int
	FinishReason string
	Meta         map[string]any
	Reasoning    *int
	Timestamp    int64
	LatencyMs    int64
	ToolCalls    []string
	Validations  []string
	Name         string
	ID           string
}

type Narrow struct {
	Content string
	Parts   []string
}

func build(src Narrow, cost *int) *Message {
	return &Message{
		Role:     "assistant",
		Content:  src.Content,
		Parts:    src.Parts,
		CostInfo: cost,
	}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 5, 3)
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Contains(t, got[0].Detail, "FinishReason",
		"the field that actually broke #1681 must be named")
	assert.Contains(t, got[0].Subject, "4/13")
}

func TestSplitPatternDir(t *testing.T) {
	cases := []struct {
		name        string
		pattern     string
		wantDir     string
		wantPattern string
	}{
		{"recursive suffix", "some/dir/...", "some/dir", "./..."},
		{"recursive suffix at root", "/...", ".", "./..."},
		{"bare ellipsis", "...", ".", "./..."},
		{"plain directory", "some/dir", "some/dir", "."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, pattern := splitPatternDir(c.pattern)
			assert.Equal(t, c.wantDir, dir)
			assert.Equal(t, c.wantPattern, pattern)
		})
	}
}

// TestLossyRebuilds_IgnoresFreshBuildFromScratch pins the discriminator added
// after Step 6's false-positive measurement: a literal whose values are all
// constants, locals, or call results (never a field read off some other
// value) was authored fresh, not rebuilt, however many fields it drops. This
// is the shape of every eval-handler result constructor in
// runtime/evals/handlers, which otherwise swamped the signature with noise.
func TestLossyRebuilds_IgnoresFreshBuildFromScratch(t *testing.T) {
	dir := writeModule(t, `package m

type Wide struct {
	A, B, C, D, E, F string
}

func label() string { return "x" }

func Build() Wide {
	return Wide{A: "a", B: label()}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	assert.Empty(t, got, "no field is read off another value, so this is a fresh build, not a rebuild")
}

// TestLossyRebuilds_DetectsNestedFieldReads pins the post-review broadening:
// a field read counts wherever it appears in the value's expression subtree,
// not just as the top-level expression. Each of these wraps a field read in
// a conversion, a concatenation, and an index expression respectively — all
// three were rejected by the pre-review isFieldRead, which only unwrapped
// &/*/parens around a bare selector.
func TestLossyRebuilds_DetectsNestedFieldReads(t *testing.T) {
	dir := writeModule(t, `package m

type Source struct {
	A     string
	Items []string
}

type Wide struct {
	A, B, C, D, E, F string
}

func BuildConversion(s Source) Wide {
	return Wide{A: string(s.A), B: "b"}
}

func BuildConcatenation(s Source) Wide {
	return Wide{A: s.A + "!", B: "b"}
}

func BuildIndex(s Source) Wide {
	return Wide{A: s.Items[0], B: "b"}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	require.Len(t, got, 3, "conversion, concatenation, and index reads must all be recognized as field-sourced")
}

// TestLossyRebuilds_DetectsConcreteMethodCallSource pins the narrow
// method-call boundary: LossyRebuilds does not count a method call as
// reading existing data, on a concrete receiver or an interface one. A
// narrow rule recovering concrete-receiver calls (to catch the streaming
// twin of #1681, state.accumulatedContent() at sdk/streaming.go:444) was
// tried and reverted — see LossyRebuilds' doc for why: it could not tell
// state.accumulatedContent() (a real data read) apart from the eval-handler
// convention's h.Type() (a hardcoded getter), and counting both drove
// runtime/hooks+runtime/evals from 14 findings to 186. streaming.go:444 is
// therefore a known false negative.
func TestLossyRebuilds_IgnoresMethodCallSource(t *testing.T) {
	dir := writeModule(t, `package m

type State struct {
	buf string
}

func (s *State) Content() string { return s.buf }

type Reader interface {
	Content() string
}

type Wide struct {
	A, B, C, D, E, F string
}

func BuildConcrete(s *State) Wide {
	return Wide{A: s.Content(), B: "b"}
}

func BuildInterface(r Reader) Wide {
	return Wide{A: r.Content(), B: "b"}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	assert.Empty(t, got, "neither a concrete-receiver nor an interface method call is a field read")
}

// TestLossyRebuilds_IgnoresIndirectFieldForwarding plainly pins the far edge
// of the recall boundary documented on LossyRebuilds: a value copied out of a
// field by an earlier statement, a parameter forwarded opaquely, and a plain
// (non-method) function's return value are all indistinguishable, at this
// pass, from data authored fresh — none of them contain a field read, index
// read, or concrete method call anywhere in the literal's own value
// expression. Recovering these needs data-flow tracing this tool doesn't do.
func TestLossyRebuilds_IgnoresIndirectFieldForwarding(t *testing.T) {
	dir := writeModule(t, `package m

type Source struct {
	A string
}

type Wide struct {
	A, B, C, D, E, F string
}

func fresh() string { return "x" }

func BuildFromLocal(s Source) Wide {
	x := s.A
	return Wide{A: x, B: "b"}
}

func BuildFromParam(a string) Wide {
	return Wide{A: a, B: "b"}
}

func BuildFromFunc() Wide {
	return Wide{A: fresh(), B: "b"}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestLossyRebuilds_DoesNotDoubleCountViaTestVariant pins the Tests:false
// decision: go/packages' "[pkg.test]" variant shares the same non-test syntax
// as the plain package, so loading with Tests:true would parse — and report
// — every production-code literal once per variant. This fixture has both a
// production file and a _test.go file in the same package, which no other
// fixture in this file does.
func TestLossyRebuilds_DoesNotDoubleCountViaTestVariant(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.26.0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(`package m

type Wide struct {
	A, B, C, D, E, F string
}

func Rebuild(src Wide) Wide {
	return Wide{A: src.A, B: src.B}
}
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a_test.go"), []byte(`package m

import "testing"

func TestSomething(t *testing.T) {
	_ = Rebuild(Wide{})
}
`), 0o600))

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	assert.Len(t, got, 1, "the production literal in a.go must be counted exactly once, "+
		"not once per package variant ([pkg] and [pkg.test])")
}

// TestLossyRebuilds_ErrorsOnUnloadablePattern pins Finding 1: a directory
// go/packages cannot resolve to any module at all (no reachable go.mod) must
// surface as an error, not a silent empty result.
func TestLossyRebuilds_ErrorsOnUnloadablePattern(t *testing.T) {
	dir := t.TempDir() // deliberately no go.mod
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(`package m

type Wide struct{ A, B string }
`), 0o600))

	_, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	assert.Error(t, err)
}

// TestLossyRebuilds_ErrorsOnRootNotCoveredByGoWork reproduces the exact shape
// that used to make a whole-repo scan silently report 0 findings and exit 0:
// a go.work exists, but the directory being scanned (here, the workspace
// root) is not itself a module and is not one of the workspace's members, so
// "go list ./..." from that directory fails with "directory prefix . does
// not contain modules listed in go.work" — a package-level load error, not a
// Go error return from packages.Load, so it must be checked explicitly.
func TestLossyRebuilds_ErrorsOnRootNotCoveredByGoWork(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "modA")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.work"),
		[]byte("go 1.26.0\n\nuse (\n\t./modA\n)\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "go.mod"),
		[]byte("module example.com/modA\n\ngo 1.26.0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "a.go"), []byte(`package m

type Wide struct{ A, B string }
`), 0o600))

	_, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	assert.Error(t, err,
		"scanning the workspace root itself (not a module member) must not silently report zero findings")
}
