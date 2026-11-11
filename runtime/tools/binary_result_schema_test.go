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

// binaryExecutor returns content with parts, as a tool that renders media does.
type binaryExecutor struct {
	content json.RawMessage
	parts   []types.ContentPart
}

func (e *binaryExecutor) Name() string { return "binary" }
func (e *binaryExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return e.content, nil
}
func (e *binaryExecutor) ExecuteMultimodal(
	context.Context, *tools.ToolDescriptor, json.RawMessage,
) (json.RawMessage, []types.ContentPart, error) {
	return e.content, e.parts, nil
}

// renderRegistry registers a render tool whose output schema is an object
// with an id, run by exec.
func renderRegistry(t *testing.T, exec *binaryExecutor) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	reg.RegisterExecutor(exec)
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name: "render", Description: "render", Mode: exec.Name(),
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
	}))
	return reg
}

var chart = types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil)

// A binary result's text stand-in is not its structured result, so it is not
// held to the output schema: the call succeeds and keeps its media.
func TestRegistry_BinaryResultTextIsNotValidatedAgainstTheOutputSchema(t *testing.T) {
	exec := &binaryExecutor{content: json.RawMessage(`"rendered the chart"`), parts: []types.ContentPart{chart}}
	result, err := renderRegistry(t, exec).Execute(context.Background(), "render", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Empty(t, result.Error)
	assert.JSONEq(t, `"rendered the chart"`, string(result.Result))
	require.Len(t, result.Parts, 1)
	assert.Equal(t, types.ContentTypeImage, result.Parts[0].Type)

	async, err := renderRegistry(t, exec).ExecuteAsync(context.Background(), "render", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, async.Status)
	assert.Empty(t, async.Error)
	require.Len(t, async.Parts, 1)
}

// A structured result alongside media is still validated.
func TestRegistry_BinaryResultStructuredContentIsValidated(t *testing.T) {
	exec := &binaryExecutor{content: json.RawMessage(`{"name":"chart"}`), parts: []types.ContentPart{chart}}
	result, err := renderRegistry(t, exec).Execute(context.Background(), "render", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, result.Error, "result validation failed")

	async, err := renderRegistry(t, exec).ExecuteAsync(context.Background(), "render", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusFailed, async.Status)
}

// A text-only result is validated as before.
func TestRegistry_TextResultIsValidated(t *testing.T) {
	exec := &binaryExecutor{content: json.RawMessage(`"no chart"`)}
	result, err := renderRegistry(t, exec).Execute(context.Background(), "render", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, result.Error, "result validation failed")
}
