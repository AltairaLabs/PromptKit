package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSchemaLoader(t *testing.T) {
	// Create a temp schema file for the file-path branches.
	tmpDir := t.TempDir()
	schemaFile := filepath.Join(tmpDir, "custom.schema.json")
	fileContent := `{"version":"9.9.9","title":"custom"}`
	require.NoError(t, os.WriteFile(schemaFile, []byte(fileContent), 0o600))

	tests := []struct {
		name          string
		envValue      string
		setEnv        bool
		packSchemaURL string
		wantSource    string
		wantErr       bool
	}{
		{
			name:       "unset env uses embedded",
			setEnv:     false,
			wantSource: embeddedSchema,
		},
		{
			name:       "local uses embedded",
			setEnv:     true,
			envValue:   "local",
			wantSource: embeddedSchema,
		},
		{
			name:          "remote with pack schema URL",
			setEnv:        true,
			envValue:      "remote",
			packSchemaURL: "https://example.com/custom.schema.json",
			wantSource:    "https://example.com/custom.schema.json",
		},
		{
			name:       "remote without pack schema URL falls back to default",
			setEnv:     true,
			envValue:   "remote",
			wantSource: DefaultSchemaURL,
		},
		{
			name:       "absolute file path loads file content",
			setEnv:     true,
			envValue:   schemaFile,
			wantSource: fileContent,
		},
		{
			name:     "absolute file path that does not exist errors",
			setEnv:   true,
			envValue: filepath.Join(tmpDir, "does-not-exist.json"),
			wantErr:  true,
		},
		{
			name:       "bare URL treated as reference",
			setEnv:     true,
			envValue:   "https://schemas.example.com/promptpack.schema.json",
			wantSource: "https://schemas.example.com/promptpack.schema.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(SchemaSourceEnvVar, tt.envValue)
			} else {
				// Ensure the env var is not inherited from the environment.
				t.Setenv(SchemaSourceEnvVar, "")
			}

			loader, err := GetSchemaLoader(tt.packSchemaURL)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, loader)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, loader)
			assert.Equal(t, tt.wantSource, loader.JsonSource())
		})
	}
}

func TestGetEmbeddedSchema(t *testing.T) {
	got := GetEmbeddedSchema()
	assert.Equal(t, embeddedSchema, got)
	assert.NotEmpty(t, got)
}

func TestGetEmbeddedSchemaVersion(t *testing.T) {
	version, err := GetEmbeddedSchemaVersion()
	require.NoError(t, err)
	assert.NotEmpty(t, version)
}

func TestGetEmbeddedSchemaVersion_ParseError(t *testing.T) {
	// Swap the embedded schema for invalid JSON, restoring it afterward.
	orig := embeddedSchema
	t.Cleanup(func() { embeddedSchema = orig })
	embeddedSchema = "not-json{"

	_, err := GetEmbeddedSchemaVersion()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse embedded schema")
}

func TestExtractSchemaURL(t *testing.T) {
	tests := []struct {
		name     string
		packJSON string
		want     string
	}{
		{
			name:     "extracts $schema",
			packJSON: `{"$schema":"https://promptpack.org/schema/latest/promptpack.schema.json","name":"x"}`,
			want:     "https://promptpack.org/schema/latest/promptpack.schema.json",
		},
		{
			name:     "missing $schema returns empty",
			packJSON: `{"name":"x"}`,
			want:     "",
		},
		{
			name:     "invalid JSON returns empty",
			packJSON: `{not valid json`,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSchemaURL([]byte(tt.packJSON))
			assert.Equal(t, tt.want, got)
		})
	}
}
