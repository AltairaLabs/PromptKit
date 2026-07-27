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
