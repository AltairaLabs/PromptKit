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
func TestLossyRebuilds_IgnoresCompleteLiteral(t *testing.T) {
	dir := writeModule(t, `package m

type Wide struct {
	A, B, C, D string
}

func Build() Wide {
	return Wide{A: "a", B: "b", C: "c", D: "d"}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Small structs are noise: a 2-field literal setting 1 field is usually fine.
func TestLossyRebuilds_IgnoresSmallStructs(t *testing.T) {
	dir := writeModule(t, `package m

type Pair struct {
	A, B string
}

func Build() Pair {
	return Pair{A: "a"}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Unexported fields are not the caller's business and must not count.
func TestLossyRebuilds_IgnoresUnexportedFields(t *testing.T) {
	dir := writeModule(t, `package m

type Mixed struct {
	A, B, C, D string
	e, f, g    string
}

func Build() Mixed {
	return Mixed{A: "a", B: "b", C: "c", D: "d"}
}
`)

	got, err := LossyRebuilds([]string{dir + "/..."}, 4, 2)
	require.NoError(t, err)
	assert.Empty(t, got, "all four exported fields are set; unexported ones are irrelevant")
}

// Positional literals set every field by definition.
func TestLossyRebuilds_IgnoresPositionalLiteral(t *testing.T) {
	dir := writeModule(t, `package m

type Wide struct {
	A, B, C, D string
}

func Build() Wide {
	return Wide{"a", "b", "c", "d"}
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
