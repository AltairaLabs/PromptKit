package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_NoArgsReturnsUsageError(t *testing.T) {
	var buf bytes.Buffer
	err := run(nil, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRun_UnknownSubcommandReturnsError(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"bogus"}, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown subcommand "bogus"`)
}

func TestRun_BadFlagReturnsError(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"weak-assertions", "--nope"}, &buf)
	require.Error(t, err)
}

func TestRun_WeakAssertionsEmitsJSONByDefault(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import "testing"

func TestDoesNothing(t *testing.T) {
	_ = 1 + 1
}
`)
	var buf bytes.Buffer
	require.NoError(t, run([]string{"weak-assertions", dir}, &buf))

	var got []Finding
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "TestDoesNothing", got[0].Subject)
}

func TestRun_WeakAssertionsTextFlagEmitsPlainText(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import "testing"

func TestDoesNothing(t *testing.T) {
	_ = 1 + 1
}
`)
	var buf bytes.Buffer
	require.NoError(t, run([]string{"weak-assertions", "--text", dir}, &buf))
	assert.Contains(t, buf.String(), "no-assertion\tTestDoesNothing")
}

func TestRun_WeakAssertionsDefaultsToCurrentDir(t *testing.T) {
	// No paths given: falls back to "." rather than erroring. Write a weak
	// test file directly into the chdir'd directory so the default-to-"."
	// scan actually finds something. A previous version of this test scanned
	// an empty temp dir, where "defaulted to . and found nothing" is
	// indistinguishable from "scanned nothing at all" — that version stayed
	// green even with the `if len(paths) == 0 { paths = []string{"."} }`
	// branch deleted from run, since fs.Args() being empty then left paths
	// as a nil/empty slice and WeakAssertions(nil) also reports zero
	// findings by iterating zero times.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x_test.go"), []byte(`package p

import "testing"

func TestDoesNothing(t *testing.T) {
	_ = 1 + 1
}
`), 0o600))
	t.Chdir(dir)

	var buf bytes.Buffer
	require.NoError(t, run([]string{"weak-assertions"}, &buf))

	var got []Finding
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "TestDoesNothing", got[0].Subject)
}

// TestRun_PropagatesSeamsAnalyzerError pins run()'s error propagation for the
// "seams" subcommand: a path with no reachable go.mod makes LossyRebuilds
// return an error (see TestLossyRebuilds_ErrorsOnUnloadablePattern), and run
// must surface it rather than swallow it. Replacing `if err != nil { return
// err }` with a no-op would leave this suite green everywhere else, since no
// other run-level test drives an analyzer into an error path.
func TestRun_PropagatesSeamsAnalyzerError(t *testing.T) {
	dir := t.TempDir() // deliberately no go.mod anywhere reachable above it
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(`package m

type Wide struct{ A, B string }
`), 0o600))

	var buf bytes.Buffer
	err := run([]string{"seams", dir + "/..."}, &buf)
	assert.Error(t, err)
}

// TestRun_SeamsAppliesMinFieldsAndMinDroppedInOrder pins both the --min-fields
// and --min-dropped flags AND their order at the LossyRebuilds call site.
// Wide has 6 exported fields; the literal sets 4 of them (via field-sourced
// values, so it clears hasFieldSourcedValue), dropping 2 (E, F). With
// min-fields=5 and min-dropped=2, both gates pass (6>=5, 2>=2) and a finding
// is expected. Two distinct regressions would silently swap what these flags
// mean to LossyRebuilds and both make this test fail:
//   - registering --min-fields/--min-dropped after fs.Parse: Parse would
//     reject them as undefined flags, and run would return an error instead
//     of nil.
//   - swapping the two ints at the LossyRebuilds(paths, *minFields,
//     *minDropped) call site: minFields would effectively receive 2 (passes,
//     6>=2) and minDropped would effectively receive 5 — but only 2 fields
//     are dropped, so 2>=5 is false and the finding disappears.
func TestRun_SeamsAppliesMinFieldsAndMinDroppedInOrder(t *testing.T) {
	dir := writeModule(t, `package m

type Wide struct {
	A, B, C, D, E, F string
}

func Rebuild(src Wide) Wide {
	return Wide{A: src.A, B: src.B, C: "c", D: "d"}
}
`)
	var buf bytes.Buffer
	err := run([]string{"seams", "--text", "--min-fields=5", "--min-dropped=2", dir}, &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "lossy-rebuild")
	assert.Contains(t, buf.String(), "Wide")
}

// TestRun_SeamsUsesDefaultThresholds exercises the "seams" subcommand with no
// threshold flags at all, pinning the documented defaults (min-fields=5,
// min-dropped=3) end to end through run(). Message has 6 exported fields and
// the literal sets 2 of them via field-sourced values (src.Role, src.Content),
// dropping 4 — both defaults are cleared.
func TestRun_SeamsUsesDefaultThresholds(t *testing.T) {
	dir := writeModule(t, `package m

type Message struct {
	Role, Content, FinishReason, Meta, Reasoning, Extra string
}

func Rebuild(src Message) *Message {
	return &Message{Role: src.Role, Content: src.Content}
}
`)
	var buf bytes.Buffer
	require.NoError(t, run([]string{"seams", "--text", dir}, &buf))
	assert.Contains(t, buf.String(), "lossy-rebuild")
}

// TestRun_SeamsEmitsJSONByDefault mirrors the weak-assertions coverage of the
// default (non --text) output path for the seams subcommand.
func TestRun_SeamsEmitsJSONByDefault(t *testing.T) {
	dir := writeModule(t, `package m

type Wide struct {
	A, B, C, D, E, F string
}

func Rebuild(src Wide) Wide {
	return Wide{A: src.A, B: src.B}
}
`)
	var buf bytes.Buffer
	require.NoError(t, run([]string{"seams", dir}, &buf))

	var got []Finding
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "lossy-rebuild", got[0].Kind)
}
