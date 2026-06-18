package agentmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
)

func TestResources_List(t *testing.T) {
	s := NewServer("test")
	resp := s.handleMessage(context.Background(), req(1, "resources/list", nil))
	require.NotNil(t, resp)
	require.Nil(t, resp.Error)

	var out resourcesListResult
	require.NoError(t, json.Unmarshal(resp.Result, &out))
	require.NotEmpty(t, out.Resources)

	uris := map[string]bool{}
	for _, r := range out.Resources {
		uris[r.URI] = true
		assert.NotEmpty(t, r.Name)
	}
	assert.True(t, uris["promptarena://concepts/mock-providers"], "concept resource listed")
	assert.True(t, uris["promptarena://schemas/scenario"], "schema resource listed")
	assert.True(t, uris["promptarena://catalog"], "catalog resource listed")
}

func readResource(t *testing.T, s *Server, uri string) *mcp.JSONRPCMessage {
	t.Helper()
	return s.handleMessage(context.Background(), req(2, "resources/read", map[string]string{"uri": uri}))
}

func TestResources_Read(t *testing.T) {
	s := NewServer("test")

	cases := map[string]string{
		"promptarena://concepts/mock-providers": "type: mock",
		"promptarena://schemas/scenario":        "$schema",
		"promptarena://catalog":                 "quick-start",
	}
	for uri, want := range cases {
		t.Run(uri, func(t *testing.T) {
			resp := readResource(t, s, uri)
			require.NotNil(t, resp)
			require.Nil(t, resp.Error)
			var out resourceReadResult
			require.NoError(t, json.Unmarshal(resp.Result, &out))
			require.NotEmpty(t, out.Contents)
			assert.Equal(t, uri, out.Contents[0].URI)
			assert.Contains(t, out.Contents[0].Text, want)
		})
	}
}

func TestResources_Read_UnknownURI(t *testing.T) {
	s := NewServer("test")
	resp := readResource(t, s, "promptarena://nope/x")
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
}

func TestResources_Read_BadParams(t *testing.T) {
	s := NewServer("test")
	msg := &mcp.JSONRPCMessage{JSONRPC: "2.0", ID: 3, Method: "resources/read", Params: json.RawMessage(`"oops"`)}
	resp := s.handleMessage(context.Background(), msg)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, invalidParams, resp.Error.Code)
}
