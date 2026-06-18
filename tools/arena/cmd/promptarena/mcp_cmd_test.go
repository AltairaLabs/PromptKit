package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunMCP_InitializeRoundTrip(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n"
	var out bytes.Buffer

	mcpCmd.SetContext(context.Background())
	mcpCmd.SetIn(strings.NewReader(in))
	mcpCmd.SetOut(&out)
	t.Cleanup(func() { mcpCmd.SetIn(nil); mcpCmd.SetOut(nil) })

	require.NoError(t, runMCP(mcpCmd, nil))

	var resp struct {
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp))
	assert.NotEmpty(t, resp.Result.ServerInfo.Name)
}
