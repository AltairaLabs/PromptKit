package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A result checked with CheckResult fails or passes exactly as the same
// result an executor returns to ExecuteAsync: the same status, error text and
// content.
func TestRegistry_CheckResultMatchesAnExecutedCall(t *testing.T) {
	for name, content := range map[string]json.RawMessage{
		"invalid": json.RawMessage(`{"id":7}`),
		"valid":   json.RawMessage(`{"id":"r-1"}`),
	} {
		t.Run(name, func(t *testing.T) {
			exec := &binaryExecutor{content: content}
			reg := renderRegistry(t, exec)
			ran, err := reg.ExecuteAsync(context.Background(), "render", json.RawMessage(`{}`))
			require.NoError(t, err)

			checked, err := reg.CheckResult("render", content, nil)
			require.NoError(t, err)
			assert.Equal(t, ran, checked)
		})
	}
	invalid, err := renderRegistry(t, &binaryExecutor{}).CheckResult("render", json.RawMessage(`{"id":7}`), nil)
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusFailed, invalid.Status)
	assert.Equal(t, "result validation failed: coercion failed: tool render validation error (result_invalid): "+
		"result validation failed: [id: Invalid type. Expected: string, given: integer]", invalid.Error)
	assert.Nil(t, invalid.Content)

	valid, err := renderRegistry(t, &binaryExecutor{}).CheckResult("render", json.RawMessage(`{"id":"r-1"}`), nil)
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, valid.Status)
	assert.Equal(t, `{"id":"r-1"}`, string(valid.Content))
}

// A result that carries media, or one for a tool in mcp mode, is not held to
// the output schema, as on a call the registry runs.
func TestRegistry_CheckResultSkipsMediaAndMCPResults(t *testing.T) {
	reg := renderRegistry(t, &binaryExecutor{})
	media, err := reg.CheckResult("render", json.RawMessage(`"rendered"`), []types.ContentPart{chart})
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, media.Status)
	assert.Equal(t, `"rendered"`, string(media.Content))
	assert.Equal(t, []types.ContentPart{chart}, media.Parts)

	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name: "lookup", Description: "lookup", Mode: "mcp",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["id"]}`),
	}))
	mcp, err := reg.CheckResult("lookup", json.RawMessage(`"plain text"`), nil)
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, mcp.Status)
	assert.Equal(t, `"plain text"`, string(mcp.Content))
}

// A result for a tool the registry does not know is refused with the
// registry's error.
func TestRegistry_CheckResultRefusesAnUnknownTool(t *testing.T) {
	_, err := renderRegistry(t, &binaryExecutor{}).CheckResult("missing", json.RawMessage(`{}`), nil)
	assert.EqualError(t, err, "tool not found: missing")
}
