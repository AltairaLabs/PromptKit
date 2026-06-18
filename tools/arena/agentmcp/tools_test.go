package agentmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
)

func callTool(t *testing.T, s *Server, name, argsJSON string) mcp.ToolCallResponse {
	t.Helper()
	call := mcp.ToolCallRequest{Name: name, Arguments: json.RawMessage(argsJSON)}
	resp := s.handleMessage(context.Background(), req(99, "tools/call", call))
	require.NotNil(t, resp)
	require.Nil(t, resp.Error)
	var out mcp.ToolCallResponse
	require.NoError(t, json.Unmarshal(resp.Result, &out))
	return out
}

func TestTool_GetSchema(t *testing.T) {
	s := NewServer("test")

	list := callTool(t, s, "get_schema", `{}`)
	require.NotEmpty(t, list.Content)
	assert.Contains(t, list.Content[0].Text, "scenario")

	one := callTool(t, s, "get_schema", `{"type":"scenario"}`)
	assert.Contains(t, one.Content[0].Text, "$schema")

	bad := callTool(t, s, "get_schema", `{"type":"nope"}`)
	assert.True(t, bad.IsError)
}

func TestTool_ListExamples(t *testing.T) {
	s := NewServer("test")

	all := callTool(t, s, "list_examples", `{}`)
	assert.Contains(t, all.Content[0].Text, "quick-start")

	tagged := callTool(t, s, "list_examples", `{"tag":"mcp"}`)
	assert.Contains(t, tagged.Content[0].Text, "mcp-integration")
	assert.NotContains(t, tagged.Content[0].Text, "quick-start")
}

func TestTool_ShowExample(t *testing.T) {
	s := NewServer("test")

	ok := callTool(t, s, "show_example", `{"name":"quick-start"}`)
	assert.False(t, ok.IsError)
	assert.Contains(t, ok.Content[0].Text, "config.arena.yaml")

	missing := callTool(t, s, "show_example", `{"name":"does-not-exist"}`)
	assert.True(t, missing.IsError)

	noName := callTool(t, s, "show_example", `{}`)
	assert.True(t, noName.IsError)
}
