package file

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomic_NoLeftoverTemp(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "state.json")

	require.NoError(t, writeFileAtomic(dst, []byte(`{"x":1}`), FSyncOnSave))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, `{"x":1}`, string(got))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no .tmp leftovers")
}

func TestAppendJSONLine_AppendsOnePerLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")

	require.NoError(t, appendJSONLine(path, map[string]any{"a": 1}, FSyncOff))
	require.NoError(t, appendJSONLine(path, map[string]any{"a": 2}, FSyncOff))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`+"\n"+`{"a":2}`+"\n", string(data))
}

func TestCountLines_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	n, err := countLines(path)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestCountLines_ManyLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\nc\n"), fileMode))
	n, err := countLines(path)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
}

func TestCountLines_LastLineNoNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\nc"), fileMode))
	n, err := countLines(path)
	require.NoError(t, err)
	assert.Equal(t, 3, n, "trailing-newline-less file still counts last line")
}

func TestScanJSONLines_MissingFileReturnsNil(t *testing.T) {
	got, err := scanJSONLines[map[string]any](filepath.Join(t.TempDir(), "nope.jsonl"))
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestScanJSONLines_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	require.NoError(t, appendJSONLine(path, map[string]any{"i": 1}, FSyncOff))
	require.NoError(t, appendJSONLine(path, map[string]any{"i": 2}, FSyncOff))

	got, err := scanJSONLines[map[string]any](path)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, float64(1), got[0]["i"])
	assert.Equal(t, float64(2), got[1]["i"])
}

func TestAppendJSONBytes_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "log.jsonl")

	require.NoError(t, appendJSONBytes(path, []byte("hello"), FSyncOff))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(data))
}

func TestWriteFileAtomic_FSyncOff(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "x.json")
	require.NoError(t, writeFileAtomic(dst, []byte(`{}`), FSyncOff))
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "{}", string(got))
}

func TestCountLines_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	require.NoError(t, os.WriteFile(path, nil, fileMode))
	n, err := countLines(path)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "empty file counts as zero lines")
}

func TestAppendJSONLine_UnmarshallableValue(t *testing.T) {
	// A channel cannot be JSON-marshalled.
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	err := appendJSONLine(path, make(chan int), FSyncOff)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal")
}

func TestWriteFileAtomic_RenameFails(t *testing.T) {
	// Renaming over a non-empty directory fails on most platforms; use that
	// to drive the rename error branch.
	dir := t.TempDir()
	dst := filepath.Join(dir, "subdir")
	require.NoError(t, os.MkdirAll(filepath.Join(dst, "child"), dirMode))
	err := writeFileAtomic(dst, []byte("x"), FSyncOff)
	require.Error(t, err)
}

func TestWriteFileAtomic_MkdirFails(t *testing.T) {
	dir := t.TempDir()
	clash := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(clash, []byte("blocker"), fileMode))
	err := writeFileAtomic(filepath.Join(clash, "child", "x.json"), []byte("x"), FSyncOff)
	require.Error(t, err)
}

func TestAppendJSONBytes_OnUnwriteableDir(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where the parent dir is expected — appendJSONBytes
	// will try to MkdirAll, which will refuse to overwrite the file as a dir.
	clash := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(clash, []byte("blocker"), fileMode))
	err := appendJSONBytes(filepath.Join(clash, "log.jsonl"), []byte("x"), FSyncOff)
	require.Error(t, err)
}

func TestScanJSONLines_DecodeError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("not-json\n"), fileMode))
	_, err := scanJSONLines[map[string]any](path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestCountLines_SingleLineWithoutNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("solo"), fileMode))
	n, err := countLines(path)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestConvLock_StableIdentity(t *testing.T) {
	s, err := NewStore(Options{Root: t.TempDir()})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	l1 := s.convLock("conv-1")
	l2 := s.convLock("conv-1")
	assert.Same(t, l1, l2, "convLock returns the same mutex for the same id")
	l3 := s.convLock("conv-2")
	assert.NotSame(t, l1, l3, "different ids get different mutexes")
}
