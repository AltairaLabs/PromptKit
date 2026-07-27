package main

import (
	"bytes"
	"encoding/json"
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
	// No paths given: falls back to "." rather than erroring. Run from a temp
	// dir with no Go files so the scan is a trivial no-op.
	dir := t.TempDir()
	t.Chdir(dir)

	var buf bytes.Buffer
	require.NoError(t, run([]string{"weak-assertions"}, &buf))

	var got []Finding
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Empty(t, got)
}
