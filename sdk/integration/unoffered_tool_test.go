package integration

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// unofferedToolsPackJSON declares two pack tools. Only lookup_order is in the
// prompt's tools, so refund is registered but never offered.
const unofferedToolsPackJSON = `{
	"id": "integration-test-unoffered-tools",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a support agent.",
			"tools": ["lookup_order"]
		}
	},
	"tools": {
		"lookup_order": {
			"name": "lookup_order",
			"description": "Look up an order",
			"mode": "local",
			"parameters": {"type": "object", "properties": {"order_id": {"type": "string"}}}
		},
		"refund": {
			"name": "refund",
			"description": "Issue a refund",
			"mode": "local",
			"parameters": {"type": "object", "properties": {"order_id": {"type": "string"}}}
		}
	}
}`

// A realtime (ASM) session refuses a call to a registered tool it was never
// offered, and runs the calls to the tools it holds: the prompt's tool and the
// tool the host declared on the session (#2713).
func TestDuplexToolParity_UnofferedToolIsRefused(t *testing.T) {
	calls := []types.MessageToolCall{
		{ID: "c1", Name: "lookup_order", Args: json.RawMessage(`{"order_id":"ORD-1"}`)},
		{ID: "c2", Name: "refund", Args: json.RawMessage(`{"order_id":"ORD-1"}`)},
		{ID: "c3", Name: "host_ping", Args: json.RawMessage(`{}`)},
	}
	conv, provider := openDuplexToolConv(t, unofferedToolsPackJSON, calls, "ok",
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
			Tools:  []providers.StreamingToolDefinition{{Name: "host_ping", Description: "Ping the host"}},
		}))
	var mu sync.Mutex
	ran := map[string]int{}
	record := func(name string) func(map[string]any) (any, error) {
		return func(map[string]any) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			ran[name]++
			return name + " ran", nil
		}
	}
	conv.OnTool("lookup_order", record("lookup_order"))
	conv.OnTool("refund", record("refund"))
	conv.OnToolAsync("host_ping",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{} },
		record("host_ping"))

	got := firstToolResponses(t, conv, provider, "refund ORD-1")

	require.Len(t, got, 3)
	assert.ElementsMatch(t, []providers.ToolResponse{
		{ToolCallID: "c1", Result: "lookup_order ran"},
		{ToolCallID: "c2", Result: "Tool refund was not run: it is not available this turn", IsError: true},
		{ToolCallID: "c3", Result: "host_ping ran"},
	}, got)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, map[string]int{"lookup_order": 1, "host_ping": 1}, ran, "the unoffered tool never runs")
}
