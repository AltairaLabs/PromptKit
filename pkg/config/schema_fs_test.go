package config

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"
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
	t.Run("nil restores the embedded default", func(t *testing.T) {
		useSchemaFSForTest(t, fsys)
		UseSchemaFS(nil)
		assert.Equal(t, embeddedSchemaPrefix+"0:arena.json", buildSchemaKey(ConfigTypeArena, ""))
	})
}

// failingTransport fails every request and counts them, so a test can prove
// validation made none.
type failingTransport struct{ calls atomic.Int32 }

func (f *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls.Add(1)
	return nil, errors.New("network disabled by test")
}

// With no network, the default validates against the schemas embedded in this
// package and makes no request at all (#2070) — including the raw-schema read
// that enriches errors.
func TestDefaultSchemaSource_ValidatesOfflineAgainstEmbeddedSchemas(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "")
	UseSchemaFS(nil)
	transport := &failingTransport{}
	orig := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = orig })

	valid := []byte(markerArenaYAML + `metadata:
  name: offline
spec:
  prompt_configs:
    - id: test
      file: test.yaml
  providers:
    - file: provider.yaml
  scenarios:
    - file: scenario.yaml
  defaults:
    temperature: 0.7
`)
	result, err := ValidateWithSchema(valid, ConfigTypeArena)
	require.NoError(t, err)
	assert.True(t, result.Valid, "errors: %+v", result.Errors)

	invalid := []byte(markerArenaYAML + "spec: {}\nzz_not_a_field: 1\n")
	result, err = ValidateWithSchema(invalid, ConfigTypeArena)
	require.NoError(t, err)
	require.False(t, result.Valid)
	assert.Equal(t, keywordAdditionalProperty, result.Errors[0].Keyword)
	assert.Contains(t, result.Errors[0].ValidValues, "spec", "enrichment must read the embedded schema too")

	assert.Zero(t, transport.calls.Load(), "validation must not touch the network")
}

// The embedded copy is a mirror of schemas/v1alpha1, which go:embed cannot
// reach from this module. If they differ, the default validates against stale
// schemas — run `make schemas`.
func TestEmbeddedSchemas_MatchCommittedCopy(t *testing.T) {
	committed := os.DirFS(filepath.Join("..", "..", SchemaLocalPath))
	embedded := builtinSchemaFS.fsys

	list := func(fsys fs.FS) []string {
		var names []string
		require.NoError(t, fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				names = append(names, p)
			}
			return err
		}))
		return names
	}
	names := list(committed)
	require.NotEmpty(t, names)
	require.Equal(t, names, list(embedded), "embedded schema files differ from %s — run `make schemas`", SchemaLocalPath)
	for _, name := range names {
		want, err := fs.ReadFile(committed, name)
		require.NoError(t, err)
		got, err := fs.ReadFile(embedded, name)
		require.NoError(t, err)
		assert.Equal(t, string(want), string(got), "%s differs from %s — run `make schemas`", name, SchemaLocalPath)
	}
}

// PROMPTKIT_SCHEMA_SOURCE=remote is opt-in; when the fetch fails it falls back
// to the embedded schemas unless SchemaFallbackDisabled is set.
func TestRemoteSchemaFetchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	key := srv.URL + "/arena.json"

	t.Run("falls back to the embedded schema", func(t *testing.T) {
		schema, err := loadSchema(key, ConfigTypeArena, "")
		require.NoError(t, err)
		result, err := schema.Validate(gojsonschema.NewStringLoader(`{"apiVersion":"x","kind":"Arena"}`))
		require.NoError(t, err)
		require.False(t, result.Valid())
		assert.Contains(t, result.Errors()[0].String(), "spec", "the fallback must be the real arena schema")
	})
	t.Run("errors when fallback is disabled", func(t *testing.T) {
		SchemaFallbackDisabled.Store(true)
		t.Cleanup(func() { SchemaFallbackDisabled.Store(false) })
		_, err := loadSchema(key, ConfigTypeArena, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load schema")
	})
}

// A local directory the caller asked for is never silently swapped for the
// embedded copy.
func TestLocalSchemaFailure_DoesNotFallBack(t *testing.T) {
	_, err := loadSchema("file:///nonexistent/schemas/arena.json", ConfigTypeArena, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load schema")
}
