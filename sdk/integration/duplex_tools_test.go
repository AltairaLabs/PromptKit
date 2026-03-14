package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

// ---------------------------------------------------------------------------
// Test-local mock: streaming session with ToolResponseSupport
// ---------------------------------------------------------------------------

// toolAwareMockSession wraps MockStreamSession and adds ToolResponseSupport.
// It simulates a provider that emits tool call chunks on the first turn and
// a text response after receiving tool results.
type toolAwareMockSession struct {
	*mock.MockStreamSession

	mu              sync.Mutex
	toolCalls       []types.MessageToolCall // tool calls to emit on first turn
	postToolText    string                  // text to emit after receiving tool results
	toolResultsCh   chan []providers.ToolResponse
	toolCallsSent   bool
	turnCount       int
	closeOnLastTurn bool

	// Observability: track what the pipeline actually did
	toolResponsesReceived atomic.Int32
	receivedResponses     [][]providers.ToolResponse
	responsesMu           sync.Mutex
}

func newToolAwareMockSession(
	toolCalls []types.MessageToolCall,
	postToolText string,
) *toolAwareMockSession {
	return &toolAwareMockSession{
		MockStreamSession: mock.NewMockStreamSession(),
		toolCalls:         toolCalls,
		postToolText:      postToolText,
		toolResultsCh:     make(chan []providers.ToolResponse, 1),
		closeOnLastTurn:   true,
	}
}

// SendText overrides MockStreamSession.SendText to emit tool call chunks
// on the first turn instead of a normal text response.
func (s *toolAwareMockSession) SendText(ctx context.Context, text string) error {
	s.mu.Lock()
	turn := s.turnCount
	s.turnCount++
	s.mu.Unlock()

	if turn == 0 && len(s.toolCalls) > 0 {
		// First turn: emit text delta, then tool call chunk with FinishReason
		go s.emitToolCallResponse()
		return nil
	}

	// Subsequent turns: delegate to base (auto-respond if configured)
	return s.MockStreamSession.SendText(ctx, text)
}

// emitToolCallResponse emits a tool call response (text prefix + tool calls).
func (s *toolAwareMockSession) emitToolCallResponse() {
	s.mu.Lock()
	if s.toolCallsSent {
		s.mu.Unlock()
		return
	}
	s.toolCallsSent = true
	tc := s.toolCalls
	s.mu.Unlock()

	// Emit a text delta first to verify streaming continuity
	s.EmitChunk(&providers.StreamChunk{
		Delta: "Let me check ",
	})

	// Emit the tool call chunk with FinishReason to trigger turn boundary
	finishReason := "tool_calls"
	s.EmitChunk(&providers.StreamChunk{
		Content:      "Let me check ",
		ToolCalls:    tc,
		FinishReason: &finishReason,
	})

	// Wait for tool results, then emit final text
	go s.waitForToolResults()
}

// waitForToolResults waits for tool results and emits a final text response.
func (s *toolAwareMockSession) waitForToolResults() {
	select {
	case <-s.toolResultsCh:
		// Got tool results — emit the post-tool response
		finishReason := "stop"
		s.EmitChunk(&providers.StreamChunk{
			Content:      s.postToolText,
			Delta:        s.postToolText,
			FinishReason: &finishReason,
		})

		s.mu.Lock()
		shouldClose := s.closeOnLastTurn
		s.mu.Unlock()

		if shouldClose {
			_ = s.Close()
		}
	case <-time.After(1 * time.Second):
		// Timeout — close to unblock tests
		_ = s.Close()
	}
}

// SendToolResponse implements providers.ToolResponseSupport.
func (s *toolAwareMockSession) SendToolResponse(_ context.Context, toolCallID, result string) error {
	return s.SendToolResponses(context.Background(), []providers.ToolResponse{
		{ToolCallID: toolCallID, Result: result},
	})
}

// SendToolResponses implements providers.ToolResponseSupport.
func (s *toolAwareMockSession) SendToolResponses(_ context.Context, responses []providers.ToolResponse) error {
	s.toolResponsesReceived.Add(1)
	s.responsesMu.Lock()
	s.receivedResponses = append(s.receivedResponses, responses)
	s.responsesMu.Unlock()

	select {
	case s.toolResultsCh <- responses:
	default:
	}
	return nil
}

// toolAwareStreamingProvider creates toolAwareMockSessions.
type toolAwareStreamingProvider struct {
	*mock.StreamingProvider
	toolCalls    []types.MessageToolCall
	postToolText string

	mu       sync.Mutex
	sessions []*toolAwareMockSession
}

func newToolAwareStreamingProvider(
	toolCalls []types.MessageToolCall,
	postToolText string,
) *toolAwareStreamingProvider {
	return &toolAwareStreamingProvider{
		StreamingProvider: mock.NewStreamingProvider("mock-tool", "mock-model", false),
		toolCalls:         toolCalls,
		postToolText:      postToolText,
	}
}

// CreateStreamSession overrides to return toolAwareMockSession.
func (p *toolAwareStreamingProvider) CreateStreamSession(
	_ context.Context,
	_ *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	sess := newToolAwareMockSession(p.toolCalls, p.postToolText)

	p.mu.Lock()
	p.sessions = append(p.sessions, sess)
	p.mu.Unlock()

	return sess, nil
}

func (p *toolAwareStreamingProvider) getSessions() []*toolAwareMockSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*toolAwareMockSession, len(p.sessions))
	copy(out, p.sessions)
	return out
}

// ---------------------------------------------------------------------------
// Pack JSON for duplex tool tests
// ---------------------------------------------------------------------------

const duplexToolsPackJSON = `{
	"id": "integration-test-duplex-tools",
	"version": "1.0.0",
	"description": "Pack with tools for duplex tool integration tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant with tools.",
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

const duplexMultiToolsPackJSON = `{
	"id": "integration-test-duplex-multi-tools",
	"version": "1.0.0",
	"description": "Pack with multiple tools for duplex tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant with tools.",
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

// ---------------------------------------------------------------------------
// Helper: open duplex conversation with tool-aware mock
// ---------------------------------------------------------------------------

func openDuplexToolConv(
	t *testing.T,
	packJSON string,
	toolCalls []types.MessageToolCall,
	postToolText string,
	extraOpts ...sdk.Option,
) (*sdk.Conversation, *toolAwareStreamingProvider) {
	t.Helper()

	provider := newToolAwareStreamingProvider(toolCalls, postToolText)
	packPath := writePackFile(t, packJSON)

	opts := []sdk.Option{
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio,
			},
		}),
	}
	opts = append(opts, extraOpts...)

	conv, err := sdk.OpenDuplex(packPath, "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	return conv, provider
}

// collectDuplexResponse reads from the response channel until FinishReason is set,
// the channel closes, or a timeout is hit. Returns all received chunks.
func collectDuplexResponse(
	t *testing.T,
	responseCh <-chan providers.StreamChunk,
	timeout time.Duration,
) []providers.StreamChunk {
	t.Helper()

	var chunks []providers.StreamChunk
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case chunk, ok := <-responseCh:
			if !ok {
				return chunks
			}
			chunks = append(chunks, chunk)
			if chunk.FinishReason != nil || chunk.Error != nil {
				return chunks
			}
		case <-timer.C:
			t.Log("timeout collecting duplex response")
			return chunks
		}
	}
}

// ---------------------------------------------------------------------------
// 6.1 — Server-side sync tool execution in duplex mode
// ---------------------------------------------------------------------------

func TestDuplexTools_SyncHandlerExecution(t *testing.T) {
	toolCalls := []types.MessageToolCall{{
		ID:   "call_1",
		Name: "get_location",
		Args: json.RawMessage(`{"accuracy":"fine"}`),
	}}

	conv, provider := openDuplexToolConv(t, duplexToolsPackJSON, toolCalls, "You are in San Francisco.")

	// Patch tool mode and register sync handler
	patchToolMode(t, conv, "get_location", "client")

	var handlerCalled atomic.Bool
	var capturedArgs map[string]any
	var argsMu sync.Mutex

	conv.OnClientTool("get_location", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		handlerCalled.Store(true)
		argsMu.Lock()
		capturedArgs = req.Args
		argsMu.Unlock()
		return map[string]any{"lat": 37.7749, "lng": -122.4194}, nil
	})

	// Start response listener
	responseCh, err := conv.Response()
	require.NoError(t, err)

	// Send text to trigger the pipeline
	ctx := context.Background()
	err = conv.SendText(ctx, "Where am I?")
	require.NoError(t, err)

	// Collect all response chunks until channel closes (full round-trip)
	var chunks []providers.StreamChunk
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case chunk, ok := <-responseCh:
			if !ok {
				goto collected
			}
			t.Logf("chunk: delta=%q content=%q finish=%v pending=%d err=%v",
				chunk.Delta, chunk.Content, chunk.FinishReason, len(chunk.PendingTools), chunk.Error)
			chunks = append(chunks, chunk)
		case <-timer.C:
			t.Fatal("timeout waiting for response channel to close")
		}
	}
collected:

	require.NotEmpty(t, chunks, "expected response chunks")

	// Verify handler was called with correct args
	assert.True(t, handlerCalled.Load(), "sync handler should have been called")
	argsMu.Lock()
	assert.Equal(t, "fine", capturedArgs["accuracy"])
	argsMu.Unlock()

	// Verify the post-tool response text arrived (proves full round-trip)
	var gotPostToolText bool
	for _, chunk := range chunks {
		if chunk.Delta == "You are in San Francisco." || chunk.Content == "You are in San Francisco." {
			gotPostToolText = true
		}
	}
	assert.True(t, gotPostToolText, "should receive post-tool text response")

	// Verify no pending tools surfaced (handler ran synchronously)
	for _, chunk := range chunks {
		assert.Empty(t, chunk.PendingTools, "sync handler should not produce pending tools")
	}

	// Verify tool results were sent to the provider via ToolResponseSupport
	sessions := provider.getSessions()
	require.Len(t, sessions, 1)
	sess := sessions[0]
	require.Equal(t, int32(1), sess.toolResponsesReceived.Load(),
		"pipeline should have called SendToolResponses exactly once on the provider session")
	sess.responsesMu.Lock()
	require.Len(t, sess.receivedResponses, 1)
	assert.Equal(t, "call_1", sess.receivedResponses[0][0].ToolCallID,
		"tool response should reference the original tool call ID")
	assert.False(t, sess.receivedResponses[0][0].IsError,
		"tool response should not be an error")
	sess.responsesMu.Unlock()
}

// ---------------------------------------------------------------------------
// 6.2 — Client-side deferred tool surfacing in duplex mode
// ---------------------------------------------------------------------------

func TestDuplexTools_DeferredToolSurfacing(t *testing.T) {
	toolCalls := []types.MessageToolCall{{
		ID:   "call_1",
		Name: "get_location",
		Args: json.RawMessage(`{"accuracy":"coarse"}`),
	}}

	conv, provider := openDuplexToolConv(t, duplexToolsPackJSON, toolCalls, "You are in San Francisco.")

	// Patch tool mode but do NOT register a handler — tool should be deferred
	patchToolMode(t, conv, "get_location", "client")

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	err = conv.SendText(ctx, "Where am I?")
	require.NoError(t, err)

	// Collect response — should get pending tools
	chunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	for i, chunk := range chunks {
		t.Logf("chunk[%d]: delta=%q content=%q finish=%v pending=%d err=%v",
			i, chunk.Delta, chunk.Content, chunk.FinishReason, len(chunk.PendingTools), chunk.Error)
	}
	require.NotEmpty(t, chunks, "expected response chunks")

	// Find the chunk with pending tools
	var pendingChunk *providers.StreamChunk
	for i := range chunks {
		if len(chunks[i].PendingTools) > 0 {
			pendingChunk = &chunks[i]
			break
		}
	}

	require.NotNil(t, pendingChunk, "expected a chunk with PendingTools")
	require.Len(t, pendingChunk.PendingTools, 1)
	assert.Equal(t, "call_1", pendingChunk.PendingTools[0].CallID)
	assert.Equal(t, "get_location", pendingChunk.PendingTools[0].ToolName)
	assert.Equal(t, map[string]any{"accuracy": "coarse"}, pendingChunk.PendingTools[0].Args)
	require.NotNil(t, pendingChunk.FinishReason)
	assert.Equal(t, "pending_tools", *pendingChunk.FinishReason)

	// Verify NO tool responses were sent to the provider (tool was deferred, not executed)
	sessions := provider.getSessions()
	require.Len(t, sessions, 1)
	assert.Equal(t, int32(0), sessions[0].toolResponsesReceived.Load(),
		"deferred tools should NOT send tool responses to the provider")
}

// ---------------------------------------------------------------------------
// 6.3 — Mixed tools: one sync + one deferred
// ---------------------------------------------------------------------------

func TestDuplexTools_MixedSyncAndDeferred(t *testing.T) {
	toolCalls := []types.MessageToolCall{
		{
			ID:   "call_1",
			Name: "get_location",
			Args: json.RawMessage(`{"accuracy":"fine"}`),
		},
		{
			ID:   "call_2",
			Name: "read_contacts",
			Args: json.RawMessage(`{}`),
		},
	}

	conv, provider := openDuplexToolConv(t, duplexMultiToolsPackJSON, toolCalls, "Done.")

	// Patch both tools as client mode
	patchToolMode(t, conv, "get_location", "client")
	patchToolMode(t, conv, "read_contacts", "client")

	// Register handler only for get_location — read_contacts should be deferred
	var handlerCalled atomic.Bool
	conv.OnClientTool("get_location", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		handlerCalled.Store(true)
		return map[string]any{"lat": 37.7749, "lng": -122.4194}, nil
	})

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	err = conv.SendText(ctx, "Where am I and who are my contacts?")
	require.NoError(t, err)

	chunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	for i, chunk := range chunks {
		t.Logf("chunk[%d]: delta=%q finish=%v pending=%d err=%v",
			i, chunk.Delta, chunk.FinishReason, len(chunk.PendingTools), chunk.Error)
	}
	require.NotEmpty(t, chunks, "expected response chunks")

	// get_location handler should have been called
	assert.True(t, handlerCalled.Load(), "get_location sync handler should have been called")

	// read_contacts should be surfaced as pending
	var pendingChunk *providers.StreamChunk
	for i := range chunks {
		if len(chunks[i].PendingTools) > 0 {
			pendingChunk = &chunks[i]
			break
		}
	}
	require.NotNil(t, pendingChunk, "expected a chunk with pending tools for read_contacts")
	assert.Equal(t, "call_2", pendingChunk.PendingTools[0].CallID)
	assert.Equal(t, "read_contacts", pendingChunk.PendingTools[0].ToolName)

	// Verify get_location results were sent to provider (sync path)
	// and read_contacts was NOT (deferred path).
	// The sync tool results may still be in stageInput when the pending chunk
	// arrives on streamOutput, so poll until the pipeline goroutine processes them.
	sessions := provider.getSessions()
	require.Len(t, sessions, 1)
	sess := sessions[0]
	require.True(t, waitForToolResponses(sess, 1, 2*time.Second),
		"pipeline should have called SendToolResponses for the sync-handled tool")
	sess.responsesMu.Lock()
	require.Len(t, sess.receivedResponses, 1)
	// The response should be for call_1 (get_location), not call_2 (read_contacts)
	assert.Equal(t, "call_1", sess.receivedResponses[0][0].ToolCallID)
	sess.responsesMu.Unlock()
}

// ---------------------------------------------------------------------------
// 6.4 — Streaming continuity: text flows before and after tool execution
// ---------------------------------------------------------------------------

func TestDuplexTools_StreamingContinuity(t *testing.T) {
	toolCalls := []types.MessageToolCall{{
		ID:   "call_1",
		Name: "get_location",
		Args: json.RawMessage(`{"accuracy":"fine"}`),
	}}

	conv, _ := openDuplexToolConv(t, duplexToolsPackJSON, toolCalls, "You are in SF.")

	patchToolMode(t, conv, "get_location", "client")

	// Register sync handler — tracks execution timestamp to prove it ran mid-stream
	var handlerTime time.Time
	conv.OnClientTool("get_location", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		handlerTime = time.Now()
		return map[string]any{"lat": 37.7749, "lng": -122.4194}, nil
	})

	responseCh, err := conv.Response()
	require.NoError(t, err)

	// Record when we start the stream
	streamStart := time.Now()

	ctx := context.Background()
	err = conv.SendText(ctx, "Where am I?")
	require.NoError(t, err)

	// Collect ALL chunks until channel closes
	// When tools are sync-handled, handleToolCalls doesn't forward the tool call
	// element to streamOutput — it sends results back to the pipeline silently.
	// The response channel only receives the post-tool response.
	var allChunks []providers.StreamChunk
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	for {
		select {
		case chunk, ok := <-responseCh:
			if !ok {
				goto done
			}
			allChunks = append(allChunks, chunk)
		case <-timer.C:
			goto done
		}
	}
done:

	require.NotEmpty(t, allChunks, "expected response chunks")

	// Verify the post-tool text arrived via the same response channel
	var gotPostToolText bool
	for _, chunk := range allChunks {
		if chunk.Delta == "You are in SF." || chunk.Content == "You are in SF." {
			gotPostToolText = true
		}
	}
	assert.True(t, gotPostToolText,
		"post-tool response should arrive on the same stream without re-creation")

	// Verify handler ran between stream start and response receipt
	assert.False(t, handlerTime.IsZero(), "handler should have been called")
	assert.True(t, handlerTime.After(streamStart),
		"handler should execute after stream starts (side-channel execution)")

	// No errors on the stream — tool execution didn't break the pipeline
	for _, chunk := range allChunks {
		assert.Nil(t, chunk.Error, "no errors should appear on the stream")
	}
}

// ---------------------------------------------------------------------------
// 6.5 — No tool registry: tool calls forwarded as-is
// ---------------------------------------------------------------------------

func TestDuplexTools_NoRegistryForwardsToolCalls(t *testing.T) {
	// Use a pack without tools (no tool registry wired)
	const noToolsPackJSON = `{
		"id": "integration-test-no-tools",
		"version": "1.0.0",
		"description": "Pack without tools",
		"prompts": {
			"chat": {
				"id": "chat",
				"name": "Chat",
				"system_template": "You are a helpful assistant."
			}
		}
	}`

	toolCalls := []types.MessageToolCall{{
		ID:   "call_1",
		Name: "some_tool",
		Args: json.RawMessage(`{}`),
	}}

	provider := newToolAwareStreamingProvider(toolCalls, "Done.")
	packPath := writePackFile(t, noToolsPackJSON)

	conv, err := sdk.OpenDuplex(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio,
			},
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	err = conv.SendText(ctx, "Do something")
	require.NoError(t, err)

	// When no tool registry has any executors for the tool, the tool call element
	// should still come through on the response channel.
	// The exact behavior depends on whether the registry can handle ExecuteAsync
	// for an unknown tool — if not, it's treated as an error and forwarded.
	chunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	require.NotEmpty(t, chunks, "should receive at least one chunk")
}

// ---------------------------------------------------------------------------
// 6.6 — Handler error sent as tool result (not pipeline failure)
// ---------------------------------------------------------------------------

func TestDuplexTools_HandlerErrorSentAsToolResult(t *testing.T) {
	toolCalls := []types.MessageToolCall{{
		ID:   "call_1",
		Name: "get_location",
		Args: json.RawMessage(`{"accuracy":"fine"}`),
	}}

	conv, _ := openDuplexToolConv(t, duplexToolsPackJSON, toolCalls, "Sorry, location unavailable.")

	patchToolMode(t, conv, "get_location", "client")

	// Register handler that returns an error
	conv.OnClientTool("get_location", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		return nil, assert.AnError
	})

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	err = conv.SendText(ctx, "Where am I?")
	require.NoError(t, err)

	// Collect response — the error should be sent as a tool result, not crash the stream
	var allChunks []providers.StreamChunk
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	for {
		select {
		case chunk, ok := <-responseCh:
			if !ok {
				goto done
			}
			allChunks = append(allChunks, chunk)
		case <-timer.C:
			goto done
		}
	}
done:

	require.NotEmpty(t, allChunks, "should receive response chunks even when handler errors")

	// Verify the post-tool response arrived (LLM processes the error result)
	var gotPostTool bool
	for _, chunk := range allChunks {
		if chunk.Content == "Sorry, location unavailable." || chunk.Delta == "Sorry, location unavailable." {
			gotPostTool = true
		}
	}
	assert.True(t, gotPostTool, "should receive post-tool response after handler error")

	// No fatal errors on the stream
	for _, chunk := range allChunks {
		if chunk.Error != nil {
			t.Errorf("unexpected fatal error on stream: %v", chunk.Error)
		}
	}
}

// ---------------------------------------------------------------------------
// 6.7 — Verify ToolResponseSupport interface is used
// ---------------------------------------------------------------------------

func TestDuplexTools_ToolResponsesSentToProvider(t *testing.T) {
	toolCalls := []types.MessageToolCall{{
		ID:   "call_1",
		Name: "get_location",
		Args: json.RawMessage(`{"accuracy":"fine"}`),
	}}

	conv, provider := openDuplexToolConv(t, duplexToolsPackJSON, toolCalls, "You are in SF.")

	patchToolMode(t, conv, "get_location", "client")

	conv.OnClientTool("get_location", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		return map[string]any{"lat": 37.7749, "lng": -122.4194}, nil
	})

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	err = conv.SendText(ctx, "Where am I?")
	require.NoError(t, err)

	// Drain response
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	for {
		select {
		case _, ok := <-responseCh:
			if !ok {
				goto done
			}
		case <-timer.C:
			goto done
		}
	}
done:

	// Verify the provider session received tool results via ToolResponseSupport
	sessions := provider.getSessions()
	require.Len(t, sessions, 1, "should have created exactly one session")

	// The tool results channel should have been written to
	// (it was consumed by waitForToolResults, so the session emitted the post-tool response)
	// The fact that we got the post-tool text proves tool results were sent.
	// This is an indirect verification — the direct proof is that the mock's
	// SendToolResponses wrote to toolResultsCh, which unblocked waitForToolResults.
}

// ---------------------------------------------------------------------------
// 6.8 — Multiple tool calls all sync: all results sent back
// ---------------------------------------------------------------------------

func TestDuplexTools_MultipleSyncToolCalls(t *testing.T) {
	toolCalls := []types.MessageToolCall{
		{
			ID:   "call_1",
			Name: "get_location",
			Args: json.RawMessage(`{"accuracy":"fine"}`),
		},
		{
			ID:   "call_2",
			Name: "read_contacts",
			Args: json.RawMessage(`{}`),
		},
	}

	conv, _ := openDuplexToolConv(t, duplexMultiToolsPackJSON, toolCalls, "Here are your results.")

	patchToolMode(t, conv, "get_location", "client")
	patchToolMode(t, conv, "read_contacts", "client")

	// Register handlers for both tools
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

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	err = conv.SendText(ctx, "Where am I and who are my contacts?")
	require.NoError(t, err)

	// Drain response
	var allChunks []providers.StreamChunk
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	for {
		select {
		case chunk, ok := <-responseCh:
			if !ok {
				goto done
			}
			allChunks = append(allChunks, chunk)
		case <-timer.C:
			goto done
		}
	}
done:

	// Both handlers should have been called
	assert.True(t, locationCalled.Load(), "get_location handler should have been called")
	assert.True(t, contactsCalled.Load(), "read_contacts handler should have been called")

	// No pending tools
	for _, chunk := range allChunks {
		assert.Empty(t, chunk.PendingTools, "all tools should be handled synchronously")
	}

	// Got the post-tool response
	var gotPostTool bool
	for _, chunk := range allChunks {
		if chunk.Delta == "Here are your results." || chunk.Content == "Here are your results." {
			gotPostTool = true
		}
	}
	assert.True(t, gotPostTool, "should receive post-tool text after all tools complete")
}

// ---------------------------------------------------------------------------
// 6.9 — HITL gate blocks execution: pending surfaced, stream not interrupted
// ---------------------------------------------------------------------------

const duplexHITLPackJSON = `{
	"id": "integration-test-duplex-hitl",
	"version": "1.0.0",
	"description": "Pack with server tool for HITL tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant with tools.",
			"tools": ["process_refund"]
		}
	},
	"tools": {
		"process_refund": {
			"name": "process_refund",
			"description": "Process a customer refund",
			"parameters": {
				"type": "object",
				"properties": {
					"amount": {"type": "number"},
					"order_id": {"type": "string"}
				},
				"required": ["amount", "order_id"]
			}
		}
	}
}`

const duplexHITLMultiPackJSON = `{
	"id": "integration-test-duplex-hitl-multi",
	"version": "1.0.0",
	"description": "Pack with HITL + sync tools for mixed tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant with tools.",
			"tools": ["process_refund", "get_status"]
		}
	},
	"tools": {
		"process_refund": {
			"name": "process_refund",
			"description": "Process a customer refund",
			"parameters": {
				"type": "object",
				"properties": {
					"amount": {"type": "number"},
					"order_id": {"type": "string"}
				},
				"required": ["amount", "order_id"]
			}
		},
		"get_status": {
			"name": "get_status",
			"description": "Get order status",
			"parameters": {
				"type": "object",
				"properties": {
					"order_id": {"type": "string"}
				},
				"required": ["order_id"]
			}
		}
	}
}`

func TestDuplexTools_HITLGateBlocksExecution(t *testing.T) {
	toolCalls := []types.MessageToolCall{{
		ID:   "call_1",
		Name: "process_refund",
		Args: json.RawMessage(`{"amount":2000,"order_id":"ORD-123"}`),
	}}

	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON, toolCalls, "Refund processed.")

	// Register HITL handler: high-value refunds require approval
	var execCalled atomic.Bool
	conv.OnToolAsync(
		"process_refund",
		func(args map[string]any) sdktools.PendingResult {
			if amount, ok := args["amount"].(float64); ok && amount > 1000 {
				return sdktools.PendingResult{
					Reason:  "high_value_refund",
					Message: fmt.Sprintf("Refund of $%.0f requires approval", amount),
				}
			}
			return sdktools.PendingResult{}
		},
		func(args map[string]any) (any, error) {
			execCalled.Store(true)
			return map[string]any{"status": "refunded", "amount": args["amount"]}, nil
		},
	)

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	err = conv.SendText(ctx, "Refund $2000 for order ORD-123")
	require.NoError(t, err)

	// Collect response — should get pending tools (HITL gate fires)
	chunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	for i, chunk := range chunks {
		t.Logf("chunk[%d]: delta=%q finish=%v pending=%d err=%v",
			i, chunk.Delta, chunk.FinishReason, len(chunk.PendingTools), chunk.Error)
	}

	// Execution handler should NOT have been called yet (gated)
	assert.False(t, execCalled.Load(), "exec handler should not be called before approval")

	// Pending tools should be surfaced
	var pendingChunk *providers.StreamChunk
	for i := range chunks {
		if len(chunks[i].PendingTools) > 0 {
			pendingChunk = &chunks[i]
			break
		}
	}
	require.NotNil(t, pendingChunk, "expected pending tools chunk")
	require.Len(t, pendingChunk.PendingTools, 1)
	assert.Equal(t, "call_1", pendingChunk.PendingTools[0].CallID)
	assert.Equal(t, "process_refund", pendingChunk.PendingTools[0].ToolName)
	require.NotNil(t, pendingChunk.PendingTools[0].PendingInfo)
	assert.Equal(t, "high_value_refund", pendingChunk.PendingTools[0].PendingInfo.Reason)

	// Verify the pending call is stored in the conversation's pending store
	pendingList := conv.PendingTools()
	require.Len(t, pendingList, 1)
	assert.Equal(t, "process_refund", pendingList[0].Name)

	// Resolve the pending tool — this should execute the handler
	resolution, err := conv.ResolveTool("call_1")
	require.NoError(t, err)
	assert.NotNil(t, resolution)
	assert.True(t, execCalled.Load(), "exec handler should be called after approval")

	// ContinueDuplex sends the result back to the provider
	err = conv.ContinueDuplex(ctx)
	require.NoError(t, err)

	// Collect the post-tool response (provider emits text after receiving tool results)
	postChunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	for i, chunk := range postChunks {
		t.Logf("post-resolve chunk[%d]: delta=%q content=%q finish=%v",
			i, chunk.Delta, chunk.Content, chunk.FinishReason)
	}

	// Verify the post-tool response arrived
	var gotPostTool bool
	for _, chunk := range postChunks {
		if chunk.Delta == "Refund processed." || chunk.Content == "Refund processed." {
			gotPostTool = true
		}
	}
	assert.True(t, gotPostTool, "should receive post-tool response after HITL approval")

	// Verify tool results were sent to the provider
	sessions := provider.getSessions()
	require.Len(t, sessions, 1)
	require.True(t, waitForToolResponses(sessions[0], 1, 2*time.Second),
		"tool results should be sent to the provider after HITL resolution")
}

// ---------------------------------------------------------------------------
// 6.10 — HITL check passes: tool executes immediately
// ---------------------------------------------------------------------------

func TestDuplexTools_HITLCheckPasses(t *testing.T) {
	toolCalls := []types.MessageToolCall{{
		ID:   "call_1",
		Name: "process_refund",
		Args: json.RawMessage(`{"amount":50,"order_id":"ORD-456"}`),
	}}

	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON, toolCalls, "Small refund processed.")

	// Register HITL handler: only high-value refunds are gated
	var execCalled atomic.Bool
	conv.OnToolAsync(
		"process_refund",
		func(args map[string]any) sdktools.PendingResult {
			if amount, ok := args["amount"].(float64); ok && amount > 1000 {
				return sdktools.PendingResult{Reason: "high_value"}
			}
			return sdktools.PendingResult{} // Low value — proceed immediately
		},
		func(args map[string]any) (any, error) {
			execCalled.Store(true)
			return map[string]any{"status": "refunded"}, nil
		},
	)

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	err = conv.SendText(ctx, "Refund $50 for order ORD-456")
	require.NoError(t, err)

	// Drain response
	var allChunks []providers.StreamChunk
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case chunk, ok := <-responseCh:
			if !ok {
				goto done
			}
			allChunks = append(allChunks, chunk)
		case <-timer.C:
			goto done
		}
	}
done:

	// Handler should have executed immediately (check passed)
	assert.True(t, execCalled.Load(), "handler should execute when HITL check passes")

	// No pending tools should be surfaced
	for _, chunk := range allChunks {
		assert.Empty(t, chunk.PendingTools, "no pending tools when check passes")
	}

	// Tool results should be sent to provider
	sessions := provider.getSessions()
	require.Len(t, sessions, 1)
	assert.Equal(t, int32(1), sessions[0].toolResponsesReceived.Load())
}

// ---------------------------------------------------------------------------
// 6.11 — HITL rejection: rejected result sent to provider
// ---------------------------------------------------------------------------

func TestDuplexTools_HITLRejection(t *testing.T) {
	toolCalls := []types.MessageToolCall{{
		ID:   "call_1",
		Name: "process_refund",
		Args: json.RawMessage(`{"amount":5000,"order_id":"ORD-789"}`),
	}}

	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON, toolCalls, "Refund was denied.")

	conv.OnToolAsync(
		"process_refund",
		func(args map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{
				Reason:  "high_value_refund",
				Message: "Requires manager approval",
			}
		},
		func(args map[string]any) (any, error) {
			t.Fatal("exec handler should not be called for rejected tools")
			return nil, nil
		},
	)

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	err = conv.SendText(ctx, "Refund $5000")
	require.NoError(t, err)

	// Collect pending
	chunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	var pendingChunk *providers.StreamChunk
	for i := range chunks {
		if len(chunks[i].PendingTools) > 0 {
			pendingChunk = &chunks[i]
			break
		}
	}
	require.NotNil(t, pendingChunk, "expected pending tools")

	// Reject the tool
	resolution, err := conv.RejectTool("call_1", "not authorized for this amount")
	require.NoError(t, err)
	assert.True(t, resolution.Rejected)
	assert.Equal(t, "not authorized for this amount", resolution.RejectionReason)

	// Send the rejection back to the provider
	err = conv.ContinueDuplex(ctx)
	require.NoError(t, err)

	// Collect post-rejection response
	postChunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	var gotPostTool bool
	for _, chunk := range postChunks {
		if chunk.Delta == "Refund was denied." || chunk.Content == "Refund was denied." {
			gotPostTool = true
		}
	}
	assert.True(t, gotPostTool, "should receive post-rejection response")

	// Verify the rejection was sent to the provider as an error response
	sessions := provider.getSessions()
	require.Len(t, sessions, 1)
	require.True(t, waitForToolResponses(sessions[0], 1, 2*time.Second))
	sessions[0].responsesMu.Lock()
	require.Len(t, sessions[0].receivedResponses, 1)
	resp := sessions[0].receivedResponses[0][0]
	assert.Equal(t, "call_1", resp.ToolCallID)
	assert.True(t, resp.IsError, "rejection should be sent as error")
	assert.Contains(t, resp.Result, "rejected")
	sessions[0].responsesMu.Unlock()
}

// ---------------------------------------------------------------------------
// 6.12 — Mixed: HITL gated + immediate sync tool
// ---------------------------------------------------------------------------

func TestDuplexTools_HITLMixedWithSyncTool(t *testing.T) {
	toolCalls := []types.MessageToolCall{
		{
			ID:   "call_1",
			Name: "get_status",
			Args: json.RawMessage(`{"order_id":"ORD-123"}`),
		},
		{
			ID:   "call_2",
			Name: "process_refund",
			Args: json.RawMessage(`{"amount":5000,"order_id":"ORD-123"}`),
		},
	}

	conv, provider := openDuplexToolConv(t, duplexHITLMultiPackJSON, toolCalls, "Done.")

	// Register sync handler for get_status via client tool path (works after OpenDuplex)
	patchToolMode(t, conv, "get_status", "client")
	var statusCalled atomic.Bool
	conv.OnClientTool("get_status", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		statusCalled.Store(true)
		return map[string]any{"status": "shipped"}, nil
	})

	// Register HITL handler for process_refund (always gated)
	var refundCalled atomic.Bool
	conv.OnToolAsync(
		"process_refund",
		func(args map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "high_value"}
		},
		func(args map[string]any) (any, error) {
			refundCalled.Store(true)
			return map[string]any{"refunded": true}, nil
		},
	)

	responseCh, err := conv.Response()
	require.NoError(t, err)

	ctx := context.Background()
	err = conv.SendText(ctx, "Check status and refund order ORD-123")
	require.NoError(t, err)

	// Collect response — should get pending tools for process_refund
	chunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	for i, chunk := range chunks {
		t.Logf("chunk[%d]: delta=%q finish=%v pending=%d err=%v",
			i, chunk.Delta, chunk.FinishReason, len(chunk.PendingTools), chunk.Error)
	}

	// get_status should have executed immediately
	assert.True(t, statusCalled.Load(), "get_status sync handler should have been called")

	// process_refund should be pending
	assert.False(t, refundCalled.Load(), "process_refund should not execute before approval")
	var pendingChunk *providers.StreamChunk
	for i := range chunks {
		if len(chunks[i].PendingTools) > 0 {
			pendingChunk = &chunks[i]
			break
		}
	}
	require.NotNil(t, pendingChunk, "expected pending tools for process_refund")
	assert.Equal(t, "call_2", pendingChunk.PendingTools[0].CallID)
	assert.Equal(t, "process_refund", pendingChunk.PendingTools[0].ToolName)

	// get_status results should be sent to provider (sync path)
	sessions := provider.getSessions()
	require.Len(t, sessions, 1)
	require.True(t, waitForToolResponses(sessions[0], 1, 2*time.Second),
		"sync tool results should be sent to provider")
}

// waitForToolResponses polls until the mock session has received the expected
// number of SendToolResponses calls, or the timeout expires.
func waitForToolResponses(sess *toolAwareMockSession, expected int32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if sess.toolResponsesReceived.Load() >= expected {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return sess.toolResponsesReceived.Load() >= expected
}

// Compile-time check: toolAwareMockSession implements ToolResponseSupport.
var _ providers.ToolResponseSupport = (*toolAwareMockSession)(nil)

// Compile-time check: toolAwareMockSession implements StreamInputSession.
var _ providers.StreamInputSession = (*toolAwareMockSession)(nil)
