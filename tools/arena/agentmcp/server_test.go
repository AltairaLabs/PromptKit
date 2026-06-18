package agentmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
)

func req(id int, method string, params any) *mcp.JSONRPCMessage {
	var raw json.RawMessage
	if params != nil {
		raw, _ = json.Marshal(params)
	}
	return &mcp.JSONRPCMessage{JSONRPC: "2.0", ID: id, Method: method, Params: raw}
}

func TestServer_Initialize(t *testing.T) {
	s := NewServer("test")
	resp := s.handleMessage(context.Background(), req(1, "initialize", nil))
	require.NotNil(t, resp)
	require.Nil(t, resp.Error)

	var init mcp.InitializeResponse
	require.NoError(t, json.Unmarshal(resp.Result, &init))
	assert.Equal(t, mcp.ProtocolVersion, init.ProtocolVersion)
	require.NotNil(t, init.Capabilities.Tools)
	require.NotNil(t, init.Capabilities.Resources)
	assert.NotEmpty(t, init.ServerInfo.Name)
}

func TestServer_ToolsList(t *testing.T) {
	s := NewServer("test")
	resp := s.handleMessage(context.Background(), req(2, "tools/list", nil))
	require.NotNil(t, resp)
	require.Nil(t, resp.Error)

	var out mcp.ToolsListResponse
	require.NoError(t, json.Unmarshal(resp.Result, &out))
	names := map[string]bool{}
	for i := range out.Tools {
		names[out.Tools[i].Name] = true
		assert.NotEmpty(t, out.Tools[i].InputSchema, "tool %s needs an input schema", out.Tools[i].Name)
	}
	assert.True(t, names["explain"], "explain tool advertised")
}

func TestServer_ToolsCall_Explain(t *testing.T) {
	s := NewServer("test")
	call := mcp.ToolCallRequest{Name: "explain", Arguments: json.RawMessage(`{"id":"mock-providers"}`)}
	resp := s.handleMessage(context.Background(), req(3, "tools/call", call))
	require.NotNil(t, resp)
	require.Nil(t, resp.Error)

	var out mcp.ToolCallResponse
	require.NoError(t, json.Unmarshal(resp.Result, &out))
	assert.False(t, out.IsError)
	require.NotEmpty(t, out.Content)
	assert.Contains(t, out.Content[0].Text, "type: mock")
}

func TestServer_ToolsCall_Explain_ListAll(t *testing.T) {
	s := NewServer("test")
	call := mcp.ToolCallRequest{Name: "explain"} // no id -> list
	resp := s.handleMessage(context.Background(), req(10, "tools/call", call))
	require.NotNil(t, resp)
	require.Nil(t, resp.Error)

	var out mcp.ToolCallResponse
	require.NoError(t, json.Unmarshal(resp.Result, &out))
	require.NotEmpty(t, out.Content)
	assert.Contains(t, out.Content[0].Text, "mock-providers")
	assert.Contains(t, out.Content[0].Text, "assertions-vs-evals")
}

func TestServer_ToolsCall_UnknownTool(t *testing.T) {
	s := NewServer("test")
	call := mcp.ToolCallRequest{Name: "nope"}
	resp := s.handleMessage(context.Background(), req(4, "tools/call", call))
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "unknown tool is an isError result, not a protocol error")

	var out mcp.ToolCallResponse
	require.NoError(t, json.Unmarshal(resp.Result, &out))
	assert.True(t, out.IsError)
}

func TestServer_NotificationGetsNoResponse(t *testing.T) {
	s := NewServer("test")
	resp := s.handleMessage(context.Background(), &mcp.JSONRPCMessage{JSONRPC: "2.0", Method: "notifications/initialized"})
	assert.Nil(t, resp)
}

func TestServer_UnknownMethod(t *testing.T) {
	s := NewServer("test")
	resp := s.handleMessage(context.Background(), req(9, "bogus/method", nil))
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, methodNotFound, resp.Error.Code)
}

func TestServer_ToolsCall_InvalidParams(t *testing.T) {
	s := NewServer("test")
	// Params is a JSON string, not a ToolCallRequest object.
	msg := &mcp.JSONRPCMessage{JSONRPC: "2.0", ID: 5, Method: "tools/call", Params: json.RawMessage(`"oops"`)}
	resp := s.handleMessage(context.Background(), msg)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, invalidParams, resp.Error.Code)
}

func TestServer_ToolsCall_Explain_BadArgs(t *testing.T) {
	s := NewServer("test")
	call := mcp.ToolCallRequest{Name: "explain", Arguments: json.RawMessage(`"notanobject"`)}
	resp := s.handleMessage(context.Background(), req(6, "tools/call", call))
	require.NotNil(t, resp)
	require.Nil(t, resp.Error)

	var out mcp.ToolCallResponse
	require.NoError(t, json.Unmarshal(resp.Result, &out))
	assert.True(t, out.IsError)
}

func TestServer_Ping(t *testing.T) {
	s := NewServer("test")
	resp := s.handleMessage(context.Background(), req(7, "ping", nil))
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
}

func TestServer_UnknownNotificationIsSilent(t *testing.T) {
	s := NewServer("test")
	// No id + unknown method = notification we don't recognise -> no response.
	resp := s.handleMessage(context.Background(), &mcp.JSONRPCMessage{JSONRPC: "2.0", Method: "notifications/cancelled"})
	assert.Nil(t, resp)
}

func TestServer_Serve_ParseErrorThenRecovers(t *testing.T) {
	in := "this is not json\n" + `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"
	var out bytes.Buffer
	require.NoError(t, NewServer("test").Serve(context.Background(), strings.NewReader(in), &out))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 2)
	var parseErr mcp.JSONRPCMessage
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &parseErr))
	require.NotNil(t, parseErr.Error)
	assert.Equal(t, parseError, parseErr.Error.Code)
}

func TestServer_Serve_StdioRoundTrip(t *testing.T) {
	// Two newline-delimited requests; expect two newline-delimited responses
	// and no response for the interleaved notification.
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	require.NoError(t, NewServer("test").Serve(context.Background(), strings.NewReader(in), &out))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 2, "two requests -> two responses, notification silent")

	var first mcp.JSONRPCMessage
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.Equal(t, float64(1), first.ID)
}
