//go:build integration_sse

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Run with:
//
//	docker run --rm -p 8080:8080 ghcr.io/altairalabs/codegen-sandbox:latest
//	go test ./runtime/mcp/... -tags=integration_sse -run TestSSEClient_AgainstSandbox -v -count=1
func TestSSEClient_AgainstSandbox(t *testing.T) {
	url := os.Getenv("PROMPTKIT_SANDBOX_URL")
	if url == "" {
		url = "http://localhost:8080"
	}

	c := NewSSEClient(ServerConfig{Name: "sandbox", URL: url})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.Initialize(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ServerInfo.Name)

	tools, err := c.ListTools(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, tools, "sandbox should expose at least one tool")

	out, err := c.CallTool(ctx, "Read", json.RawMessage(`{"path":"/etc/hostname"}`))
	require.NoError(t, err)
	assert.False(t, out.IsError)
	assert.NotEmpty(t, out.Content)

	require.NoError(t, c.Close())
}
