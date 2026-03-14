package integration

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// patchToolMode updates a tool descriptor's mode in the conversation's registry.
// The pack parser currently hardcodes mode: "local" for all tools, so this is
// needed to test client-mode tools in integration tests.
func patchToolMode(t *testing.T, conv *sdk.Conversation, toolName, mode string) {
	t.Helper()
	desc, err := conv.ToolRegistry().GetTool(toolName)
	require.NoError(t, err, "tool %q should exist in registry", toolName)
	desc.Mode = mode
	if mode == "client" {
		desc.ClientConfig = &tools.ClientConfig{
			Consent: &tools.ConsentConfig{
				Required: true,
				Message:  "Allow access?",
			},
			Categories: []string{"location"},
		}
	}
}

// clientToolsPackJSON defines a pack with a client-mode tool.
const clientToolsPackJSON = `{
	"id": "integration-test-client-tools",
	"version": "1.0.0",
	"description": "Pack with client tools for SDK integration tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant with client tools.",
			"tools": ["get_location"]
		}
	},
	"tools": {
		"get_location": {
			"name": "get_location",
			"description": "Get the user's GPS location",
			"mode": "client",
			"parameters": {
				"type": "object",
				"properties": {
					"accuracy": {"type": "string"}
				},
				"required": ["accuracy"]
			},
			"client": {
				"consent": {
					"required": true,
					"message": "Allow location access?"
				},
				"categories": ["location"]
			}
		}
	}
}`

// ---------------------------------------------------------------------------
// 5.1 — Client tool deferred mode: Send → pending → SendToolResult → Resume
// ---------------------------------------------------------------------------

func TestClientTools_DeferredRoundTrip(t *testing.T) {
	repo := newTestTurnRepository()
	// Turn 1: LLM calls the client tool
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check your location",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_location",
			Arguments: map[string]interface{}{"accuracy": "fine"},
		}},
	})
	// Turn 2: LLM responds with location-aware answer
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "You are in San Francisco.",
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, clientToolsPackJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	// Patch the tool descriptor to set mode to "client".
	// The pack parser currently hardcodes mode: "local" for all tools,
	// so we patch the registry after opening.
	patchToolMode(t, conv, "get_location", "client")

	// No handler registered — tool should be deferred
	ctx := context.Background()
	resp, err := conv.Send(ctx, "Where am I?")
	require.NoError(t, err)

	// Response should have pending client tools
	require.True(t, resp.HasPendingClientTools(),
		"response should have pending client tools when no handler is registered")

	clientTools := resp.ClientTools()
	require.Len(t, clientTools, 1)
	assert.Equal(t, "get_location", clientTools[0].ToolName)
	assert.Equal(t, map[string]any{"accuracy": "fine"}, clientTools[0].Args)
	assert.NotEmpty(t, clientTools[0].CallID, "CallID should be populated")

	// Provide the tool result
	err = conv.SendToolResult(ctx, clientTools[0].CallID, map[string]any{
		"lat": 37.7749,
		"lng": -122.4194,
	})
	require.NoError(t, err)

	// Resume to get final response
	resp, err = conv.Resume(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text(), "response after resume should have text")
	assert.False(t, resp.HasPendingClientTools(), "no more pending tools after resume")
}

// ---------------------------------------------------------------------------
// 5.2 — Client tool with sync handler (immediate execution)
// ---------------------------------------------------------------------------

func TestClientTools_SyncHandlerExecution(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check your location",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_location",
			Arguments: map[string]interface{}{"accuracy": "coarse"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "You are in San Francisco.",
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, clientToolsPackJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	patchToolMode(t, conv, "get_location", "client")

	// Register a sync handler
	var handlerCalled atomic.Bool
	var capturedArgs map[string]any
	var mu sync.Mutex

	conv.OnClientTool("get_location", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		handlerCalled.Store(true)
		mu.Lock()
		capturedArgs = req.Args
		mu.Unlock()
		return map[string]any{"lat": 37.7749, "lng": -122.4194}, nil
	})

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Where am I?")
	require.NoError(t, err)

	// Should NOT have pending tools — handler ran synchronously
	assert.False(t, resp.HasPendingClientTools(),
		"should not have pending tools when handler is registered")
	assert.True(t, handlerCalled.Load(), "sync handler should have been called")

	mu.Lock()
	assert.Equal(t, "coarse", capturedArgs["accuracy"])
	mu.Unlock()

	assert.NotEmpty(t, resp.Text(), "response should have text content")
}

// ---------------------------------------------------------------------------
// 5.3 — Client tool rejection
// ---------------------------------------------------------------------------

func TestClientTools_Rejection(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check your location",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_location",
			Arguments: map[string]interface{}{"accuracy": "fine"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "I understand you declined location access.",
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, clientToolsPackJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	patchToolMode(t, conv, "get_location", "client")

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Where am I?")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())

	// Reject the tool
	clientTools := resp.ClientTools()
	conv.RejectClientTool(ctx, clientTools[0].CallID, "user declined location access")

	// Resume after rejection
	resp, err = conv.Resume(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text(), "response after rejection should have text")
	assert.False(t, resp.HasPendingClientTools())
}

// ---------------------------------------------------------------------------
// 5.4 — Streaming deferred flow: Stream → pending ChunkClientTool → SendToolResult → ResumeStream
// ---------------------------------------------------------------------------

func TestClientTools_StreamingDeferredRoundTrip(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check your location",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_location",
			Arguments: map[string]interface{}{"accuracy": "fine"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "You are in San Francisco.",
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, clientToolsPackJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	patchToolMode(t, conv, "get_location", "client")

	ctx := context.Background()

	// Stream the first request — should yield ChunkClientTool
	var clientToolChunks []sdk.StreamChunk
	var doneChunk *sdk.StreamChunk
	for chunk := range conv.Stream(ctx, "Where am I?") {
		require.NoError(t, chunk.Error, "unexpected streaming error")
		if chunk.Type == sdk.ChunkClientTool {
			clientToolChunks = append(clientToolChunks, chunk)
		}
		if chunk.Type == sdk.ChunkDone {
			doneChunk = &chunk
		}
	}

	require.NotEmpty(t, clientToolChunks, "expected ChunkClientTool in stream")
	require.NotNil(t, doneChunk, "expected ChunkDone in stream")

	// Done chunk response should have pending tools
	require.True(t, doneChunk.Message.HasPendingClientTools(),
		"ChunkDone response should report pending client tools")

	pendingTool := clientToolChunks[0].ClientTool
	assert.Equal(t, "get_location", pendingTool.ToolName)
	assert.NotEmpty(t, pendingTool.CallID)

	// Provide the tool result
	err = conv.SendToolResult(ctx, pendingTool.CallID, map[string]any{
		"lat": 37.7749,
		"lng": -122.4194,
	})
	require.NoError(t, err)

	// ResumeStream to get final response
	var resumeText string
	var resumeDone *sdk.StreamChunk
	for chunk := range conv.ResumeStream(ctx) {
		require.NoError(t, chunk.Error, "unexpected error in ResumeStream")
		if chunk.Type == sdk.ChunkText {
			resumeText += chunk.Text
		}
		if chunk.Type == sdk.ChunkDone {
			resumeDone = &chunk
		}
	}

	require.NotNil(t, resumeDone, "expected ChunkDone from ResumeStream")
	assert.NotEmpty(t, resumeText, "resumed stream should produce text")
	assert.False(t, resumeDone.Message.HasPendingClientTools(),
		"no more pending tools after ResumeStream")
}

// ---------------------------------------------------------------------------
// 5.5 — Consent metadata propagation on PendingClientTool
// ---------------------------------------------------------------------------

func TestClientTools_ConsentMetadata(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check your location",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_location",
			Arguments: map[string]interface{}{"accuracy": "fine"},
		}},
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, clientToolsPackJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	patchToolMode(t, conv, "get_location", "client")

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Where am I?")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())

	clientTools := resp.ClientTools()
	require.Len(t, clientTools, 1)

	// Verify consent metadata from patchToolMode is propagated
	assert.Equal(t, "Allow access?", clientTools[0].ConsentMsg,
		"ConsentMsg should come from the tool descriptor's ClientConfig")
	assert.Equal(t, []string{"location"}, clientTools[0].Categories,
		"Categories should come from the tool descriptor's ClientConfig")
}

// ---------------------------------------------------------------------------
// 5.6 — OnClientTools batch registration
// ---------------------------------------------------------------------------

// multiClientToolsPackJSON defines a pack with two client-mode tools.
const multiClientToolsPackJSON = `{
	"id": "integration-test-multi-client-tools",
	"version": "1.0.0",
	"description": "Pack with multiple client tools",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant.",
			"tools": ["get_location", "read_contacts"]
		}
	},
	"tools": {
		"get_location": {
			"name": "get_location",
			"description": "Get the user's GPS location",
			"mode": "client",
			"parameters": {
				"type": "object",
				"properties": {
					"accuracy": {"type": "string"}
				},
				"required": ["accuracy"]
			}
		},
		"read_contacts": {
			"name": "read_contacts",
			"description": "Read the user's contacts",
			"mode": "client",
			"parameters": {
				"type": "object",
				"properties": {
					"limit": {"type": "integer"}
				}
			}
		}
	}
}`

func TestClientTools_OnClientToolsBatchRegistration(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check your location",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_location",
			Arguments: map[string]interface{}{"accuracy": "fine"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "You are in San Francisco.",
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, multiClientToolsPackJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	patchToolMode(t, conv, "get_location", "client")
	patchToolMode(t, conv, "read_contacts", "client")

	// Register both handlers via batch method
	var locationCalled, contactsCalled atomic.Bool
	conv.OnClientTools(map[string]sdk.ClientToolHandler{
		"get_location": func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
			locationCalled.Store(true)
			return map[string]any{"lat": 37.7749, "lng": -122.4194}, nil
		},
		"read_contacts": func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
			contactsCalled.Store(true)
			return []map[string]any{{"name": "Alice"}}, nil
		},
	})

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Where am I?")
	require.NoError(t, err)

	// get_location handler should have been called (the mock only calls get_location)
	assert.True(t, locationCalled.Load(), "get_location handler should be called")
	assert.False(t, resp.HasPendingClientTools(),
		"should not have pending tools when handler is registered via batch")
	assert.NotEmpty(t, resp.Text())
}

// ---------------------------------------------------------------------------
// 5.7 — Handler error propagation
// ---------------------------------------------------------------------------

func TestClientTools_HandlerErrorSentAsToolResult(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check your location",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_location",
			Arguments: map[string]interface{}{"accuracy": "fine"},
		}},
	})
	// Turn 2: LLM receives the error result and responds gracefully
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "Sorry, I could not access your location.",
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, clientToolsPackJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	patchToolMode(t, conv, "get_location", "client")

	// Register a handler that returns an error — the pipeline captures this
	// as an error tool result and feeds it back to the LLM, rather than
	// failing the entire pipeline.
	conv.OnClientTool("get_location", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		return nil, fmt.Errorf("GPS unavailable")
	})

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Where am I?")
	require.NoError(t, err, "handler errors are sent as tool results, not pipeline failures")
	assert.NotEmpty(t, resp.Text(), "LLM should respond after receiving error tool result")
	assert.False(t, resp.HasPendingClientTools())
}

// ---------------------------------------------------------------------------
// 5.8 — Resume without SendToolResult errors
// ---------------------------------------------------------------------------

func TestClientTools_ResumeWithoutToolResult(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check your location",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_location",
			Arguments: map[string]interface{}{"accuracy": "fine"},
		}},
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, clientToolsPackJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	patchToolMode(t, conv, "get_location", "client")

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Where am I?")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())

	// Resume without providing any tool results — should error
	_, err = conv.Resume(ctx)
	assert.Error(t, err, "Resume() without SendToolResult should fail")
	assert.Contains(t, err.Error(), "no resolved tool results",
		"error should indicate missing tool results")
}

// ---------------------------------------------------------------------------
// 5.9 — Streaming rejection flow
// ---------------------------------------------------------------------------

func TestClientTools_StreamingRejection(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check your location",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_location",
			Arguments: map[string]interface{}{"accuracy": "fine"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "I understand you declined location access.",
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, clientToolsPackJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	patchToolMode(t, conv, "get_location", "client")

	ctx := context.Background()

	// Stream to get pending tool
	var pendingTool *sdk.PendingClientTool
	for chunk := range conv.Stream(ctx, "Where am I?") {
		require.NoError(t, chunk.Error)
		if chunk.Type == sdk.ChunkClientTool {
			pendingTool = chunk.ClientTool
		}
	}
	require.NotNil(t, pendingTool, "expected a ChunkClientTool")

	// Reject and resume via streaming
	conv.RejectClientTool(ctx, pendingTool.CallID, "user declined")

	var resumeText string
	var resumeDone *sdk.StreamChunk
	for chunk := range conv.ResumeStream(ctx) {
		require.NoError(t, chunk.Error)
		if chunk.Type == sdk.ChunkText {
			resumeText += chunk.Text
		}
		if chunk.Type == sdk.ChunkDone {
			resumeDone = &chunk
		}
	}

	require.NotNil(t, resumeDone)
	assert.NotEmpty(t, resumeText, "resumed stream should produce text after rejection")
	assert.False(t, resumeDone.Message.HasPendingClientTools())
}
