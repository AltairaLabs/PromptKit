package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeGo writes src to a temp dir as a Go file and returns the dir.
func writeGo(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
	return dir
}

func TestWeakAssertions_FlagsNoAssertionTest(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import "testing"

func TestDoesNothing(t *testing.T) {
	_ = 1 + 1
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "no-assertion", got[0].Kind)
	assert.Equal(t, "TestDoesNothing", got[0].Subject)
	// File/Line are part of the finding's identity (the guard script keys on
	// kind+file+subject) and are how a human reaches the flagged test, so
	// both must be pinned, not just Kind/Subject: File: path -> "" or
	// Line: fset.Position(pos).Line -> 0 would otherwise leave the suite
	// green.
	assert.Equal(t, filepath.Join(dir, "x_test.go"), got[0].File)
	assert.Equal(t, 5, got[0].Line)
}

// A path WeakAssertions cannot walk at all (no such directory) must surface
// as an error, not a silent empty result: files, _ := goTestFiles(root)
// would turn a bad path into "0 findings, exit 0," which is exactly the
// failure mode documented for a "/..." suffix passed to weak-assertions and
// the failure mode the guard script's own error handling assumes can't
// happen silently.
func TestWeakAssertions_ErrorsOnNonexistentPath(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")

	_, err := WeakAssertions([]string{missing})
	assert.Error(t, err)
}

func TestWeakAssertions_FlagsErrorOnlyTest(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnlyChecksError(t *testing.T) {
	err := doThing()
	require.NoError(t, err)
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "weak-assertion", got[0].Kind)
	assert.Equal(t, "TestOnlyChecksError", got[0].Subject)
}

// A test that checks a real value must NOT be reported — otherwise the guard
// would fire on most of the suite and get switched off.
func TestWeakAssertions_IgnoresRealAssertion(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChecksAValue(t *testing.T) {
	got, err := doThing()
	require.NoError(t, err)
	require.Equal(t, 42, got)
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// t.Error / t.Fatal are assertions without testify; not weak.
func TestWeakAssertions_IgnoresTErrorStyle(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import "testing"

func TestUsesTError(t *testing.T) {
	if got := doThing(); got != 42 {
		t.Errorf("got %d", got)
	}
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Subtests are analysed as their own units, so a weak subtest inside a strong
// parent is still reported.
func TestWeakAssertions_AnalysesSubtestsSeparately(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParent(t *testing.T) {
	t.Run("strong", func(t *testing.T) {
		require.Equal(t, 1, one())
	})
	t.Run("weak", func(t *testing.T) {
		require.NoError(t, doThing2())
	})
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "weak-assertion", got[0].Kind)
	assert.Equal(t, "TestParent/weak", got[0].Subject)
}

// A parent's own assertions — the ones living outside any t.Run — must be
// classified too, not just its subtests.
func TestWeakAssertions_ReportsParentOwnAssertionOutsideSubtests(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParent(t *testing.T) {
	require.NoError(t, setup())
	t.Run("strong", func(t *testing.T) {
		require.Equal(t, 1, one())
	})
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "weak-assertion", got[0].Kind)
	assert.Equal(t, "TestParent", got[0].Subject)
}

// A parent that exists purely to host t.Run calls, with no assertion of its
// own, asserts nothing by design and must never be reported — pinned
// explicitly rather than left implied by another test's incidental shape.
func TestWeakAssertions_IgnoresParentThatOnlyHostsSubtests(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParent(t *testing.T) {
	t.Run("strong", func(t *testing.T) {
		require.Equal(t, 1, one())
	})
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Nested t.Run calls must be isolated from their ancestors (an inner
// subtest's assertions must not leak into its parent's classification) and
// named by their full path, since a shallow "parent/leaf" name collides
// across siblings at different depths.
func TestWeakAssertions_NestedSubtestsIsolatedAndFullyNamed(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParent(t *testing.T) {
	t.Run("outer", func(t *testing.T) {
		t.Run("inner", func(t *testing.T) {
			require.NoError(t, doThing())
		})
	})
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "weak-assertion", got[0].Kind)
	assert.Equal(t, "TestParent/outer/inner", got[0].Subject)
}

// Factoring a fixture closure out of two subtests (pure DRY, no behavior
// change) must not flip the parent from unreported to reported. The parent's
// only "own scope" assertion is the require.NoError inside the shared
// newFixture closure — fixture setup, not the parent's assertion surface —
// and both subtests carry real assertions of their own, so nothing here
// should be flagged.
func TestWeakAssertions_IgnoresFixtureClosureSharedBySubtests(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParent(t *testing.T) {
	newFixture := func(t *testing.T) int {
		v, err := setup()
		require.NoError(t, err)
		return v
	}

	t.Run("one", func(t *testing.T) {
		require.Equal(t, 1, newFixture(t))
	})
	t.Run("two", func(t *testing.T) {
		require.Equal(t, 2, newFixture(t))
	})
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// A weak assertion the parent makes directly, in its own statements outside
// any nested func literal, must still be flagged even when the parent also
// hosts a fixture closure — the fixture-closure suppression must not swallow
// genuine direct assertions living alongside it.
func TestWeakAssertions_StillFlagsDirectWeakAssertionBesideFixtureClosure(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParent(t *testing.T) {
	newFixture := func(t *testing.T) int {
		v, err := setup()
		require.NoError(t, err)
		return v
	}
	require.NoError(t, teardown())

	t.Run("one", func(t *testing.T) {
		require.Equal(t, 1, newFixture(t))
	})
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "weak-assertion", got[0].Kind)
	assert.Equal(t, "TestParent", got[0].Subject)
}

// The fixture-closure suppression (skipping a nested func literal's body
// when the parent delegates to t.Run subtests) must only apply when the
// parent actually has subtests. A closure that runs itself immediately, with
// no t.Run anywhere in the body, is not fixture setup for anything — it is
// the test's own assertion surface — and must still be flagged. This pins
// classifyBody's "len(children) > 0" gate specifically: hardcoding it to
// true (treating every closure as fixture setup regardless of whether any
// subtest exists to delegate to) would silently reclassify this weak
// assertion as kindNoAssertion instead of kindWeakAssertion, with no fixture
// elsewhere in this file able to notice, since every other closure fixture
// here is always paired with at least one t.Run.
func TestWeakAssertions_FlagsWeakAssertionInsideClosureWithoutSubtests(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParent(t *testing.T) {
	func() {
		require.NoError(t, teardown())
	}()
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "weak-assertion", got[0].Kind)
	assert.Equal(t, "TestParent", got[0].Subject)
}

// A locally-defined assertion helper (assertX/requireX/checkX/verifyX/mustX,
// called as a bare identifier) verifies something internally, usually via
// t.Fatalf, so a test that only calls one must not be reported. False
// positives here would get the guard switched off, so this errs toward
// under-reporting: any of these prefixes counts as a real assertion.
func TestWeakAssertions_IgnoresLocalAssertionHelpers(t *testing.T) {
	dir := writeGo(t, "x_test.go", `package p

import "testing"

func TestUsesAssertHelper(t *testing.T) {
	assertCard(t, got())
}

func TestUsesRequireHelper(t *testing.T) {
	requireThing(t, got())
}

func TestUsesCheckHelper(t *testing.T) {
	checkThing(t, got())
}

func TestUsesVerifyHelper(t *testing.T) {
	verifyThing(t, got())
}

func TestUsesMustHelper(t *testing.T) {
	mustThing(t, got())
}

func TestUsesUpperCaseHelper(t *testing.T) {
	AssertCard(t, got())
}
`)

	got, err := WeakAssertions([]string{dir})
	require.NoError(t, err)
	assert.Empty(t, got)
}
