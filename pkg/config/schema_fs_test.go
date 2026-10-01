package config

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useSchemaFSForTest registers fsys for one test and restores the default.
func useSchemaFSForTest(t *testing.T, fsys fstest.MapFS) {
	t.Helper()
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "")
	UseSchemaFS(fsys)
	t.Cleanup(func() { UseSchemaFS(nil) })
}

const markerArenaYAML = "apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: Arena\n"

// Validation uses the registered schemas, not the hosted copy: a schema that
// only exists in the registered filesystem decides the result.
func TestUseSchemaFS_ValidatesAgainstRegisteredSchemas(t *testing.T) {
	useSchemaFSForTest(t, fstest.MapFS{
		"arena.json": {Data: []byte(`{"type":"object","required":["zz_only_in_this_schema"]}`)},
	})

	result, err := ValidateWithSchema([]byte(markerArenaYAML), ConfigTypeArena)
	require.NoError(t, err)
	require.False(t, result.Valid)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Description, "zz_only_in_this_schema")
}

// Registering a different filesystem takes effect at once: a schema cached
// from the first is never served under the second.
func TestUseSchemaFS_ReplacingTheFilesystemIsNotServedFromCache(t *testing.T) {
	useSchemaFSForTest(t, fstest.MapFS{
		"arena.json": {Data: []byte(`{"type":"object","required":["zz_marker"]}`)},
	})
	first, err := ValidateWithSchema([]byte(markerArenaYAML), ConfigTypeArena)
	require.NoError(t, err)
	require.False(t, first.Valid)

	UseSchemaFS(fstest.MapFS{"arena.json": {Data: []byte(`{"type":"object"}`)}})
	second, err := ValidateWithSchema([]byte(markerArenaYAML), ConfigTypeArena)
	require.NoError(t, err)
	assert.True(t, second.Valid, "errors: %+v", second.Errors)
}

// Error enrichment (valid values, suggestions) reads the raw schema, and has to
// read it from the registered filesystem too.
func TestUseSchemaFS_EnrichesErrorsFromRegisteredSchema(t *testing.T) {
	useSchemaFSForTest(t, fstest.MapFS{
		"arena.json": {Data: []byte(`{"type":"object","properties":{"kind":{"enum":["Arena","Scenario"]}}}`)},
	})
	result, err := ValidateWithSchema([]byte("kind: Arenaa\n"), ConfigTypeArena)
	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	assert.ElementsMatch(t, []string{"Arena", "Scenario"}, result.Errors[0].ValidValues)
}

// A type missing from the registered filesystem is an error, not a silent
// fetch of the hosted copy.
func TestUseSchemaFS_MissingSchemaIsAnError(t *testing.T) {
	useSchemaFSForTest(t, fstest.MapFS{})
	_, err := ValidateWithSchema([]byte(markerArenaYAML), ConfigTypeArena)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load schema")
	assert.Contains(t, err.Error(), "arena.json")
}

func TestUseSchemaFS_SourcePrecedence(t *testing.T) {
	fsys := fstest.MapFS{"arena.json": {Data: []byte(`{}`)}}

	t.Run("registered filesystem replaces the hosted URL", func(t *testing.T) {
		useSchemaFSForTest(t, fsys)
		assert.True(t, strings.HasPrefix(buildSchemaKey(ConfigTypeArena, ""), embeddedSchemaPrefix))
	})
	t.Run("PROMPTKIT_SCHEMA_SOURCE=local still wins", func(t *testing.T) {
		useSchemaFSForTest(t, fsys)
		t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
		assert.True(t, strings.HasPrefix(buildSchemaKey(ConfigTypeArena, ""), "file://"))
	})
	t.Run("an explicit directory wins", func(t *testing.T) {
		useSchemaFSForTest(t, fsys)
		assert.Equal(t, "file:///x/arena.json", buildSchemaKey(ConfigTypeArena, "/x"))
	})
	t.Run("nil restores the hosted default", func(t *testing.T) {
		useSchemaFSForTest(t, fsys)
		UseSchemaFS(nil)
		assert.Equal(t, SchemaBaseURL+"/arena.json", buildSchemaKey(ConfigTypeArena, ""))
	})
}
