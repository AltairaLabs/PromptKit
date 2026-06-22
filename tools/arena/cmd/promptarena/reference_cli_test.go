package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCLIReference(t *testing.T) {
	s := string(generateCLIReference())
	assert.Contains(t, s, "# CLI Reference")
	assert.Contains(t, s, "promptarena run")
	assert.Contains(t, s, "promptarena validate")
	assert.Contains(t, s, "promptarena schema")
	// Hidden/internal commands should not leak.
	assert.NotContains(t, s, "completion")
}

func TestReferenceDocs_HasAllThree(t *testing.T) {
	docs, err := referenceDocs()
	require.NoError(t, err)
	for _, name := range []string{"evals-and-assertions.md", "config-fields.md", "cli.md"} {
		assert.NotEmpty(t, docs[name], "missing %s", name)
	}
}

func TestGenReferenceCommand_WritesFiles(t *testing.T) {
	dir := t.TempDir()
	prev := genReferenceOut
	genReferenceOut = dir
	t.Cleanup(func() { genReferenceOut = prev })

	require.NoError(t, genReferenceCmd.RunE(genReferenceCmd, nil))
	for _, name := range []string{"evals-and-assertions.md", "config-fields.md", "cli.md"} {
		assert.FileExists(t, filepath.Join(dir, name))
	}
	b, err := os.ReadFile(filepath.Join(dir, "cli.md"))
	require.NoError(t, err)
	assert.Contains(t, string(b), "# CLI Reference")
}

func TestGenReferenceCommand_ErrorsOnBadOutDir(t *testing.T) {
	// Make the out path's parent a regular file so MkdirAll fails.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	prev := genReferenceOut
	genReferenceOut = filepath.Join(file, "sub")
	t.Cleanup(func() { genReferenceOut = prev })

	err := genReferenceCmd.RunE(genReferenceCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create out dir")
}
