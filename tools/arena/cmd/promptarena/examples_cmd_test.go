package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

func TestWriteExamplesList_Table(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeExamplesList(&buf, "", false))
	out := buf.String()
	assert.Contains(t, out, "quick-start")
	assert.Contains(t, out, "customer-support")
}

func TestWriteExamplesList_TagFilter(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeExamplesList(&buf, "mcp", false))
	out := buf.String()
	assert.Contains(t, out, "mcp-integration")
	assert.NotContains(t, out, "quick-start")
}

func TestWriteExamplesList_JSON(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeExamplesList(&buf, "", true))

	var entries []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entries))
	require.NotEmpty(t, entries)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e["name"].(string))
	}
	assert.Contains(t, names, "quick-start")
}

func TestWriteExampleShow_Builtin(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeExampleShow(&buf, templates.NewLoader(""), "quick-start"))
	out := buf.String()
	assert.Contains(t, out, "# quick-start")
	assert.Contains(t, out, "config.arena.yaml") // a generated file path
}

func TestWriteExampleShow_Unknown(t *testing.T) {
	var buf bytes.Buffer
	require.Error(t, writeExampleShow(&buf, templates.NewLoader(""), "does-not-exist"))
}
