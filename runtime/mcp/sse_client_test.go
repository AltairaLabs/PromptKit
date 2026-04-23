package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are scaffolding tests exercising the SSE client stubs before the
// real transport implementation lands. Each stub is replaced by a real
// implementation in a follow-up task, at which point these assertions are
// superseded by transport-level tests with a fake SSE server.

func TestSSEClient_Stub_NewSSEClient(t *testing.T) {
	c := NewSSEClient(ServerConfig{Name: "x", URL: "https://x"})
	require.NotNil(t, c)
	assert.Equal(t, "x", c.config.Name)
	assert.Equal(t, "https://x", c.config.URL)
	assert.Equal(t, DefaultClientOptions(), c.options)
}

func TestSSEClient_Stub_MethodsReturnNotImplemented(t *testing.T) {
	c := NewSSEClient(ServerConfig{Name: "x", URL: "https://x"})
	ctx := context.Background()

	_, err := c.Initialize(ctx)
	assert.ErrorIs(t, err, errSSENotImplemented)

	_, err = c.ListTools(ctx)
	assert.ErrorIs(t, err, errSSENotImplemented)

	_, err = c.CallTool(ctx, "Read", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, errSSENotImplemented)

	assert.NoError(t, c.Close())
	assert.False(t, c.IsAlive())
}
