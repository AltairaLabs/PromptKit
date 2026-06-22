package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReferenceFilesUpToDate guards the committed agentkb/reference/*.md against
// drift from their generators. Regenerate after changing handlers/schemas/CLI:
//
//	go run ./tools/arena/cmd/promptarena gen-reference --out tools/arena/agentkb/reference
func TestReferenceFilesUpToDate(t *testing.T) {
	dir := filepath.Join("..", "..", "agentkb", "reference")
	docs, err := referenceDocs()
	require.NoError(t, err)

	for name, want := range docs {
		got, readErr := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, readErr,
			"missing %s — run: go run ./tools/arena/cmd/promptarena gen-reference --out tools/arena/agentkb/reference", name)
		require.Equal(t, string(want), string(got),
			"%s is stale — run: go run ./tools/arena/cmd/promptarena gen-reference --out tools/arena/agentkb/reference", name)
	}
}
