package agentkb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaNames_IncludeCoreTypes(t *testing.T) {
	names, err := SchemaNames()
	require.NoError(t, err)
	for _, want := range []string{"scenario", "provider", "promptconfig", "tool"} {
		assert.Contains(t, names, want)
	}
	assert.NotContains(t, names, "common", "common is a $ref dir, not a type")
}

func TestSchema_ReturnsBytesAndRejectsUnknown(t *testing.T) {
	b, err := Schema("scenario")
	require.NoError(t, err)
	assert.NotEmpty(t, b)
	assert.Contains(t, string(b), "$schema")

	_, err = Schema("nope")
	require.Error(t, err)
}

// Parity: the embedded schemas must byte-match the generated source of truth, so
// the binary can never ship a schema that disagrees with its own validate.
func TestSchemas_ByteMatchGeneratedSource(t *testing.T) {
	names, err := SchemaNames()
	require.NoError(t, err)
	for _, name := range names {
		embedded, err := Schema(name)
		require.NoError(t, err)
		src, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "v1alpha1", name+".json"))
		require.NoError(t, err, "missing source schema for %s", name)
		assert.Equal(t, string(src), string(embedded), "embedded %s drifted from schemas/v1alpha1", name)
	}
}
