package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func TestBuildValidateReport_MapsErrors(t *testing.T) {
	result := &config.SchemaValidationResult{
		Valid: false,
		Errors: []config.SchemaValidationError{
			{Field: "spec.x", Description: "bad", Keyword: "enum", Suggestions: []string{"y"}},
		},
	}
	report := buildValidateReport("f.yaml", "arena", result)
	assert.Equal(t, "f.yaml", report.File)
	assert.Equal(t, "arena", report.Type)
	assert.False(t, report.Valid)
	require.Len(t, report.Errors, 1)
	assert.Equal(t, "spec.x", report.Errors[0].Field)
	assert.Equal(t, "enum", report.Errors[0].Keyword)
	assert.Equal(t, []string{"y"}, report.Errors[0].Suggestions)
}

func TestWriteValidateJSON_ValidFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "arena.yaml")
	require.NoError(t, os.WriteFile(file, []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  scenarios:
    - file: test.yaml
`), 0o600))

	var buf bytes.Buffer
	require.NoError(t, writeValidateJSON(&buf, file, "auto"))

	var report validateJSONReport
	require.NoError(t, json.Unmarshal(buf.Bytes(), &report))
	assert.True(t, report.Valid)
	assert.Equal(t, "arena", report.Type)
}

func TestWriteValidateJSON_FileNotFound(t *testing.T) {
	var buf bytes.Buffer
	require.Error(t, writeValidateJSON(&buf, "/no/such/file.yaml", "auto"))
}
