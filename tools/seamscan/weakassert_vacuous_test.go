package main

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// weakSubjects scans src as one test file and returns "kind subject" for
// every finding, sorted.
func weakSubjects(t *testing.T, src string) []string {
	t.Helper()
	got, err := WeakAssertions([]string{writeGo(t, "x_test.go", src)})
	require.NoError(t, err)
	out := make([]string, 0, len(got))
	for _, f := range got {
		out = append(out, f.Kind+" "+f.Subject)
	}
	sort.Strings(out)
	return out
}

// An assertion decided by the test's own source passes whatever the code
// under test does, so it is no stronger than NoError.
func TestWeakAssertions_FlagsAssertionsDecidedByTheTestItself(t *testing.T) {
	got := weakSubjects(t, `package p

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrueLiteral(t *testing.T) {
	require.NoError(t, run())
	assert.True(t, true)
}

func TestFalseLiteral(t *testing.T) {
	assert.False(t, false)
}

func TestLiteralAgainstLiteral(t *testing.T) {
	_ = run()
	assert.Equal(t, 2, 2)
}

func TestSameExpressionBothSides(t *testing.T) {
	cfg := build()
	assert.Equal(t, cfg.Name, cfg.Name)
}

func TestTrueOnResult(t *testing.T) {
	assert.True(t, run().OK)
}

func TestEqualOnResult(t *testing.T) {
	assert.Equal(t, "x", run().Name)
}
`)

	assert.Equal(t, []string{
		"weak-assertion TestFalseLiteral",
		"weak-assertion TestLiteralAgainstLiteral",
		"weak-assertion TestSameExpressionBothSides",
		"weak-assertion TestTrueLiteral",
	}, got)
}

// A test that checks only how often a collaborator was called passes when the
// collaborator is fed garbage. One that also checks what it received, or
// compares the recorded calls with expected ones, is real.
func TestWeakAssertions_FlagsCallCountOnlyTests(t *testing.T) {
	got := weakSubjects(t, `package p

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFieldCount(t *testing.T) {
	require.NoError(t, run(fake))
	assert.Equal(t, 1, fake.calls)
}

func TestMethodCount(t *testing.T) {
	assert.Equal(t, 2, prov.CallCount())
}

func TestLenOfCalls(t *testing.T) {
	require.Len(t, prov.Calls, 1)
}

func TestLenInsideEqual(t *testing.T) {
	assert.Equal(t, 1, len(*recorder.Invocations))
}

func TestCalledFlag(t *testing.T) {
	assert.True(t, (handlerCalled))
}

func TestCountThenContent(t *testing.T) {
	require.Len(t, prov.Calls, 1)
	assert.Equal(t, "hello", prov.Calls[0].Prompt)
}

func TestCallsComparedWithExpected(t *testing.T) {
	assert.Equal(t, want, prov.Calls)
}

func TestBareCounter(t *testing.T) {
	assert.Zero(t, calls)
}

func TestToolCallsAreOutput(t *testing.T) {
	assert.True(t, resp.HasToolCalls())
	assert.Equal(t, 2, resp.CountToolCalls())
}

func TestLenOfExtractedSlice(t *testing.T) {
	calls := extract(msgs)
	require.Len(t, calls, 1)
	assert.Equal(t, 1, len(calls))
}

func TestPredicateOverCalls(t *testing.T) {
	assert.True(t, allGreeted(prov.Calls))
}

func TestLenOfResult(t *testing.T) {
	assert.Len(t, run().Messages, 2)
}
`)

	assert.Equal(t, []string{
		"weak-assertion TestBareCounter",
		"weak-assertion TestCalledFlag",
		"weak-assertion TestFieldCount",
		"weak-assertion TestLenInsideEqual",
		"weak-assertion TestLenOfCalls",
		"weak-assertion TestMethodCount",
	}, got)
}

func TestIsVacuous_IgnoresShortArgumentLists(t *testing.T) {
	assert.False(t, isVacuous("True", nil))
	assert.False(t, isVacuous("Equal", nil))
	assert.False(t, countsCalls("Len", nil))
	assert.False(t, countsCalls("Equal", nil))
}
