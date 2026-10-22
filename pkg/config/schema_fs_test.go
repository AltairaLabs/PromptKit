package config

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	t.Run("PROMPTKIT_SCHEMA_SOURCE=remote still wins", func(t *testing.T) {
		useSchemaFSForTest(t, fsys)
		t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "remote")
		assert.Equal(t, SchemaBaseURL+"/arena.json", buildSchemaKey(ConfigTypeArena, ""))
	})
	t.Run("nil leaves no source", func(t *testing.T) {
		useSchemaFSForTest(t, fsys)
		UseSchemaFS(nil)
		assert.Empty(t, buildSchemaKey(ConfigTypeArena, ""))
	})
}

// failingTransport fails every request and counts them, so a test can prove
// validation made none.
type failingTransport struct{ calls atomic.Int32 }

func (f *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls.Add(1)
	return nil, errors.New("network disabled by test")
}

// PromptKit ships no config schemas: with nothing registered and no
// PROMPTKIT_SCHEMA_SOURCE, validation is an error naming the fix, and makes no
// network request trying to find a copy.
func TestNoSchemaSource_IsAnError(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "")
	UseSchemaFS(nil)
	transport := &failingTransport{}
	orig := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = orig })

	_, err := ValidateWithSchema([]byte(markerArenaYAML), ConfigTypeArena)
	require.ErrorIs(t, err, errNoSchemas)
	assert.Contains(t, err.Error(), "UseSchemaFS")
	require.ErrorIs(t, ValidateProvider([]byte("kind: Provider\n")), errNoSchemas)
	assert.Zero(t, transport.calls.Load(), "validation must not touch the network")
}

// A schema read after its filesystem was unregistered is an error, not a panic.
func TestReadEmbeddedSchema_AfterUnregister(t *testing.T) {
	useSchemaFSForTest(t, fstest.MapFS{"arena.json": {Data: []byte(`{}`)}})
	key := buildSchemaKey(ConfigTypeArena, "")
	UseSchemaFS(nil)
	_, err := readEmbeddedSchema(key)
	require.Error(t, err)
}

// PROMPTKIT_SCHEMA_SOURCE=remote fetches the hosted copy; a failed fetch is an
// error, since there is no built-in copy to fall back to.
func TestRemoteSchemaFetchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := loadSchema(srv.URL + "/arena.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load schema")
}

// A local directory the caller asked for that holds no schema is an error.
func TestLocalSchemaFailure_DoesNotFallBack(t *testing.T) {
	_, err := loadSchema("file:///nonexistent/schemas/arena.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load schema")
}
