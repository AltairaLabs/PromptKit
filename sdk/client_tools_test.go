package sdk

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnClientTool_RegistersHandler(t *testing.T) {
	conv := newTestConversation()

	conv.OnClientTool("get_location", func(_ context.Context, req ClientToolRequest) (any, error) {
		assert.Equal(t, "get_location", req.ToolName)
		return map[string]any{"lat": 37.7749, "lng": -122.4194}, nil
	})

	conv.clientHandlersMu.RLock()
	_, ok := conv.clientHandlers["get_location"]
	conv.clientHandlersMu.RUnlock()
	assert.True(t, ok, "handler should be registered")
}

func TestOnClientTools_RegistersMultiple(t *testing.T) {
	conv := newTestConversation()

	conv.OnClientTools(map[string]ClientToolHandler{
		"get_location": func(_ context.Context, _ ClientToolRequest) (any, error) {
			return nil, nil
		},
		"read_contacts": func(_ context.Context, _ ClientToolRequest) (any, error) {
			return nil, nil
		},
	})

	conv.clientHandlersMu.RLock()
	assert.Len(t, conv.clientHandlers, 2)
	conv.clientHandlersMu.RUnlock()
}

func TestClientExecutor_Name(t *testing.T) {
	exec := &clientExecutor{}
	assert.Equal(t, "client", exec.Name())
}

func TestClientExecutor_Execute(t *testing.T) {
	conv := newTestConversation()

	conv.OnClientTool("get_location", func(_ context.Context, req ClientToolRequest) (any, error) {
		assert.Equal(t, "get_location", req.ToolName)
		lat := req.Args["accuracy"]
		return map[string]any{"lat": 37.7749, "accuracy": lat}, nil
	})

	exec := &clientExecutor{
		handlers:   conv.clientHandlers,
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	desc := &tools.ToolDescriptor{
		Name: "get_location",
		Mode: "client",
	}
	args := json.RawMessage(`{"accuracy": "fine"}`)

	result, err := exec.Execute(context.Background(), desc, args)
	require.NoError(t, err)

	var resultMap map[string]any
	require.NoError(t, json.Unmarshal(result, &resultMap))
	assert.Equal(t, 37.7749, resultMap["lat"])
	assert.Equal(t, "fine", resultMap["accuracy"])
}

func TestClientExecutor_WithConsentInfo(t *testing.T) {
	conv := newTestConversation()

	var capturedReq ClientToolRequest
	conv.OnClientTool("get_location", func(_ context.Context, req ClientToolRequest) (any, error) {
		capturedReq = req
		return map[string]any{"lat": 0.0}, nil
	})

	exec := &clientExecutor{
		handlers:   conv.clientHandlers,
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	desc := &tools.ToolDescriptor{
		Name: "get_location",
		Mode: "client",
		ClientConfig: &tools.ClientConfig{
			Consent: &tools.ConsentConfig{
				Required: true,
				Message:  "Allow location access?",
			},
			Categories: []string{"location", "sensors"},
		},
	}

	_, err := exec.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)

	assert.Equal(t, "Allow location access?", capturedReq.ConsentMsg)
	assert.Equal(t, []string{"location", "sensors"}, capturedReq.Categories)
	assert.NotNil(t, capturedReq.Descriptor)
}

func TestClientExecutor_NoHandler(t *testing.T) {
	exec := &clientExecutor{
		handlers: make(map[string]ClientToolHandler),
	}

	desc := &tools.ToolDescriptor{
		Name: "unknown_tool",
		Mode: "client",
	}

	_, err := exec.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no client handler registered")
}

func TestClientExecutor_InvalidArgs(t *testing.T) {
	conv := newTestConversation()
	conv.OnClientTool("test", func(_ context.Context, _ ClientToolRequest) (any, error) {
		return nil, nil
	})

	exec := &clientExecutor{
		handlers:   conv.clientHandlers,
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	desc := &tools.ToolDescriptor{Name: "test", Mode: "client"}
	_, err := exec.Execute(context.Background(), desc, json.RawMessage(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse client tool arguments")
}

func TestClientExecutor_HandlerError(t *testing.T) {
	conv := newTestConversation()
	conv.OnClientTool("failing", func(_ context.Context, _ ClientToolRequest) (any, error) {
		return nil, assert.AnError
	})

	exec := &clientExecutor{
		handlers:   conv.clientHandlers,
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	desc := &tools.ToolDescriptor{Name: "failing", Mode: "client"}
	_, err := exec.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}

func TestClientExecutor_NoClientConfig(t *testing.T) {
	conv := newTestConversation()

	var capturedReq ClientToolRequest
	conv.OnClientTool("simple", func(_ context.Context, req ClientToolRequest) (any, error) {
		capturedReq = req
		return "ok", nil
	})

	exec := &clientExecutor{
		handlers:   conv.clientHandlers,
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	// No ClientConfig at all
	desc := &tools.ToolDescriptor{Name: "simple", Mode: "client"}
	_, err := exec.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)

	assert.Empty(t, capturedReq.ConsentMsg)
	assert.Nil(t, capturedReq.Categories)
}

func TestClientExecutor_LateRegistration(t *testing.T) {
	// Test that handlers registered after executor creation are found
	// via the mutex accessor
	conv := newTestConversation()

	exec := &clientExecutor{
		handlers:   make(map[string]ClientToolHandler), // Empty snapshot
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	// Register handler after executor is created
	conv.OnClientTool("late_tool", func(_ context.Context, _ ClientToolRequest) (any, error) {
		return "found", nil
	})

	desc := &tools.ToolDescriptor{Name: "late_tool", Mode: "client"}
	result, err := exec.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)

	var val string
	require.NoError(t, json.Unmarshal(result, &val))
	assert.Equal(t, "found", val)
}

// --- Phase 3: Deferred mode tests ---

func TestClientExecutor_DeferredMode_NoPendingWhenHandlerExists(t *testing.T) {
	conv := newTestConversation()
	conv.OnClientTool("get_location", func(_ context.Context, _ ClientToolRequest) (any, error) {
		return map[string]any{"lat": 1.0}, nil
	})

	exec := &clientExecutor{
		handlers:   conv.clientHandlers,
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	desc := &tools.ToolDescriptor{Name: "get_location", Mode: "client"}
	result, err := exec.ExecuteAsync(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, result.Status)
	assert.NotEmpty(t, result.Content)
	assert.Nil(t, result.PendingInfo)
}

func TestClientExecutor_DeferredMode_PendingWhenNoHandler(t *testing.T) {
	exec := &clientExecutor{
		handlers: make(map[string]ClientToolHandler),
	}

	desc := &tools.ToolDescriptor{
		Name: "get_location",
		Mode: "client",
		ClientConfig: &tools.ClientConfig{
			Consent: &tools.ConsentConfig{
				Required: true,
				Message:  "Allow location?",
			},
			Categories: []string{"location"},
		},
	}

	result, err := exec.ExecuteAsync(context.Background(), desc, json.RawMessage(`{"accuracy":"fine"}`))
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusPending, result.Status)
	require.NotNil(t, result.PendingInfo)
	assert.Equal(t, "client_tool_deferred", result.PendingInfo.Reason)
	assert.Equal(t, "Allow location?", result.PendingInfo.Message)
	assert.Equal(t, "get_location", result.PendingInfo.ToolName)
	assert.NotEmpty(t, result.PendingInfo.Args)
}

func TestClientExecutor_DeferredMode_PendingNoClientConfig(t *testing.T) {
	exec := &clientExecutor{
		handlers: make(map[string]ClientToolHandler),
	}

	desc := &tools.ToolDescriptor{Name: "simple_tool", Mode: "client"}
	result, err := exec.ExecuteAsync(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusPending, result.Status)
	require.NotNil(t, result.PendingInfo)
	assert.Contains(t, result.PendingInfo.Message, "simple_tool")
}

func TestClientExecutor_DeferredMode_InvalidArgs(t *testing.T) {
	exec := &clientExecutor{
		handlers: make(map[string]ClientToolHandler),
	}

	desc := &tools.ToolDescriptor{Name: "test", Mode: "client"}
	_, err := exec.ExecuteAsync(context.Background(), desc, json.RawMessage(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse client tool arguments")
}

func TestPendingClientTool_ResponseMethods(t *testing.T) {
	t.Run("no pending client tools", func(t *testing.T) {
		resp := &Response{}
		assert.False(t, resp.HasPendingClientTools())
		assert.Empty(t, resp.ClientTools())
	})

	t.Run("with pending client tools", func(t *testing.T) {
		resp := &Response{
			clientTools: []PendingClientTool{
				{CallID: "call-1", ToolName: "get_location", Args: map[string]any{"accuracy": "fine"}},
				{CallID: "call-2", ToolName: "read_contacts"},
			},
		}
		assert.True(t, resp.HasPendingClientTools())
		assert.Len(t, resp.ClientTools(), 2)
		assert.Equal(t, "call-1", resp.ClientTools()[0].CallID)
		assert.Equal(t, "read_contacts", resp.ClientTools()[1].ToolName)
	})
}

func TestBuildResponse_PopulatesPendingClientTools(t *testing.T) {
	conv := newTestConversation()

	result := &rtpipeline.ExecutionResult{
		Messages: []types.Message{
			{Role: "assistant", Content: "I need your location"},
		},
		Response: &rtpipeline.Response{
			Role:    "assistant",
			Content: "I need your location",
		},
		Metadata: make(map[string]any),
		PendingTools: []tools.PendingToolExecution{
			{
				CallID:   "call-1",
				ToolName: "get_location",
				Args:     map[string]any{"accuracy": "fine"},
				PendingInfo: &tools.PendingToolInfo{
					Reason:  "client_tool_deferred",
					Message: "Allow location?",
					Metadata: map[string]any{
						"categories": []string{"location"},
					},
				},
			},
		},
	}

	resp := conv.buildResponse(result, time.Now())
	require.True(t, resp.HasPendingClientTools())
	require.Len(t, resp.ClientTools(), 1)

	ct := resp.ClientTools()[0]
	assert.Equal(t, "call-1", ct.CallID)
	assert.Equal(t, "get_location", ct.ToolName)
	assert.Equal(t, map[string]any{"accuracy": "fine"}, ct.Args)
	assert.Equal(t, "Allow location?", ct.ConsentMsg)
	assert.Equal(t, []string{"location"}, ct.Categories)
}

func TestSendToolResult(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	err := conv.SendToolResult(context.Background(), "call-1", map[string]any{"lat": 37.7})
	require.NoError(t, err)

	resolutions := conv.resolvedStore.PopAll()
	require.Len(t, resolutions, 1)
	assert.Equal(t, "call-1", resolutions[0].ID)
	assert.False(t, resolutions[0].Rejected)
	assert.NotEmpty(t, resolutions[0].ResultJSON)

	var resultMap map[string]any
	require.NoError(t, json.Unmarshal(resolutions[0].ResultJSON, &resultMap))
	assert.Equal(t, 37.7, resultMap["lat"])
}

func TestSendToolResult_InvalidJSON(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	// Functions can't be JSON-serialized
	err := conv.SendToolResult(context.Background(), "call-1", func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to serialize")
}

func TestRejectClientTool(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	conv.RejectClientTool(context.Background(), "call-2", "user declined")

	resolutions := conv.resolvedStore.PopAll()
	require.Len(t, resolutions, 1)
	assert.Equal(t, "call-2", resolutions[0].ID)
	assert.True(t, resolutions[0].Rejected)
	assert.Equal(t, "user declined", resolutions[0].RejectionReason)
}

func TestResume_NoResolutions(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	_, err := conv.Resume(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no resolved tool results")
}

func TestClientExecutor_ExecuteAsync_HandlerError(t *testing.T) {
	conv := newTestConversation()
	conv.OnClientTool("failing", func(_ context.Context, _ ClientToolRequest) (any, error) {
		return nil, assert.AnError
	})

	exec := &clientExecutor{
		handlers:   conv.clientHandlers,
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	desc := &tools.ToolDescriptor{Name: "failing", Mode: "client"}
	_, err := exec.ExecuteAsync(context.Background(), desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}

func TestClientExecutor_ExecuteAsync_LateRegistration(t *testing.T) {
	conv := newTestConversation()

	exec := &clientExecutor{
		handlers:   make(map[string]ClientToolHandler),
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	// Register handler after executor is created
	conv.OnClientTool("late_tool", func(_ context.Context, _ ClientToolRequest) (any, error) {
		return "found_async", nil
	})

	desc := &tools.ToolDescriptor{Name: "late_tool", Mode: "client"}
	result, err := exec.ExecuteAsync(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, result.Status)

	var val string
	require.NoError(t, json.Unmarshal(result.Content, &val))
	assert.Equal(t, "found_async", val)
}

func TestClientExecutor_ExecuteAsync_WithConsent(t *testing.T) {
	conv := newTestConversation()

	var capturedReq ClientToolRequest
	conv.OnClientTool("sensor", func(_ context.Context, req ClientToolRequest) (any, error) {
		capturedReq = req
		return "ok", nil
	})

	exec := &clientExecutor{
		handlers:   conv.clientHandlers,
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	desc := &tools.ToolDescriptor{
		Name: "sensor",
		Mode: "client",
		ClientConfig: &tools.ClientConfig{
			Consent: &tools.ConsentConfig{
				Required: true,
				Message:  "Allow sensor access?",
			},
			Categories: []string{"sensors"},
		},
	}

	result, err := exec.ExecuteAsync(context.Background(), desc, json.RawMessage(`{"type":"gyro"}`))
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, result.Status)
	assert.Equal(t, "Allow sensor access?", capturedReq.ConsentMsg)
	assert.Equal(t, []string{"sensors"}, capturedReq.Categories)
}

func TestResume_BuildsToolMessages(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	// Add a rejected resolution to cover that branch
	conv.RejectClientTool(context.Background(), "call-reject", "user declined")

	// Add a successful resolution
	err := conv.SendToolResult(context.Background(), "call-ok", map[string]any{"data": "result"})
	require.NoError(t, err)

	// Resume sends tool result messages through the pipeline.
	// The mock pipeline processes them and returns a response.
	resp, err := conv.Resume(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestSendToolResult_WhenClosed(t *testing.T) {
	conv := newTestConversation()
	conv.ctxHandlers = make(map[string]ToolHandlerCtx)
	conv.clientHandlers = make(map[string]ClientToolHandler)
	conv.resolvedStore = sdktools.NewResolvedStore()
	_ = conv.Close()

	err := conv.SendToolResult(context.Background(), "call-1", map[string]any{"data": "x"})
	assert.Equal(t, ErrConversationClosed, err)
}

func TestSendToolResultMultimodal_WhenClosed(t *testing.T) {
	conv := newTestConversation()
	conv.ctxHandlers = make(map[string]ToolHandlerCtx)
	conv.clientHandlers = make(map[string]ClientToolHandler)
	conv.resolvedStore = sdktools.NewResolvedStore()
	_ = conv.Close()

	err := conv.SendToolResultMultimodal(context.Background(), "call-1", []types.ContentPart{types.NewTextPart("hi")})
	assert.Equal(t, ErrConversationClosed, err)
}

func TestRejectClientTool_WhenClosed(t *testing.T) {
	conv := newTestConversation()
	conv.ctxHandlers = make(map[string]ToolHandlerCtx)
	conv.clientHandlers = make(map[string]ClientToolHandler)
	conv.resolvedStore = sdktools.NewResolvedStore()
	_ = conv.Close()

	// Should not panic; silently returns
	conv.RejectClientTool(context.Background(), "call-1", "denied")

	// Verify nothing was added to the resolved store
	resolutions := conv.resolvedStore.PopAll()
	assert.Empty(t, resolutions)
}

func TestResumeStream_NoResolutions(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	ch := conv.ResumeStream(context.Background())
	chunk := <-ch
	require.Error(t, chunk.Error)
	assert.Contains(t, chunk.Error.Error(), "no resolved tool results")
}

func TestResumeStream_WithResolutions(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	err := conv.SendToolResult(context.Background(), "call-1", map[string]any{"lat": 37.7})
	require.NoError(t, err)

	ch := conv.ResumeStream(context.Background())

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	// Should get at least a ChunkDone
	require.NotEmpty(t, chunks)
	lastChunk := chunks[len(chunks)-1]
	assert.Equal(t, ChunkDone, lastChunk.Type)
	assert.NotNil(t, lastChunk.Message)
}

func TestSendToolResultMultimodal(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	text := "screenshot of the result"
	imgData := "base64data"
	parts := []types.ContentPart{
		types.NewTextPart(text),
		types.NewImagePartFromData(imgData, "image/png", nil),
	}

	err := conv.SendToolResultMultimodal(context.Background(), "call-mm", parts)
	require.NoError(t, err)

	resolutions := conv.resolvedStore.PopAll()
	require.Len(t, resolutions, 1)
	assert.Equal(t, "call-mm", resolutions[0].ID)
	require.Len(t, resolutions[0].Parts, 2)
	assert.Equal(t, "text", resolutions[0].Parts[0].Type)
	assert.Equal(t, "image", resolutions[0].Parts[1].Type)
}

func TestSendToolResultMultimodal_EmptyParts(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	err := conv.SendToolResultMultimodal(context.Background(), "call-mm", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parts must not be empty")

	err = conv.SendToolResultMultimodal(context.Background(), "call-mm", []types.ContentPart{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parts must not be empty")
}

func TestBuildToolResultMessages_MultimodalParts(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	text := "here is the image"
	imgData := "base64imagedata"
	parts := []types.ContentPart{
		types.NewTextPart(text),
		types.NewImagePartFromData(imgData, "image/jpeg", nil),
	}

	conv.resolvedStore.Add(&sdktools.ToolResolution{
		ID:    "call-mm",
		Parts: parts,
	})

	msgs, err := conv.buildToolResultMessages()
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	toolResult := msgs[0].ToolResult
	require.NotNil(t, toolResult)
	require.Len(t, toolResult.Parts, 2)
	assert.Equal(t, "text", toolResult.Parts[0].Type)
	assert.Equal(t, &text, toolResult.Parts[0].Text)
	assert.Equal(t, "image", toolResult.Parts[1].Type)
	assert.Equal(t, "image/jpeg", toolResult.Parts[1].Media.MIMEType)
}

func TestBuildToolResultMessages_TextOnlyWrapsAsContentPart(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	err := conv.SendToolResult(context.Background(), "call-text", map[string]any{"status": "ok"})
	require.NoError(t, err)

	msgs, err := conv.buildToolResultMessages()
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	toolResult := msgs[0].ToolResult
	require.NotNil(t, toolResult)
	require.Len(t, toolResult.Parts, 1)
	assert.Equal(t, "text", toolResult.Parts[0].Type)
	assert.Contains(t, *toolResult.Parts[0].Text, "status")
}

func TestBuildToolResultMessages_RejectionWrapsAsContentPart(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	conv.RejectClientTool(context.Background(), "call-rej", "not allowed")

	msgs, err := conv.buildToolResultMessages()
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	toolResult := msgs[0].ToolResult
	require.NotNil(t, toolResult)
	require.Len(t, toolResult.Parts, 1)
	assert.Equal(t, "text", toolResult.Parts[0].Type)
	assert.Contains(t, *toolResult.Parts[0].Text, "Tool rejected: not allowed")
}

func TestClientExecutor_ExecuteAsync_ReturnsContentParts(t *testing.T) {
	conv := newTestConversation()

	text := "captured image"
	conv.OnClientTool("camera", func(_ context.Context, _ ClientToolRequest) (any, error) {
		return []types.ContentPart{
			types.NewTextPart(text),
			types.NewImagePartFromData("base64img", "image/png", nil),
		}, nil
	})

	exec := &clientExecutor{
		handlers:   conv.clientHandlers,
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	desc := &tools.ToolDescriptor{Name: "camera", Mode: "client"}
	result, err := exec.ExecuteAsync(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, result.Status)
	require.Len(t, result.Parts, 2)
	assert.Equal(t, "text", result.Parts[0].Type)
	assert.Equal(t, "image", result.Parts[1].Type)
	// Content should be empty since parts take precedence
	assert.Empty(t, result.Content)
}

func TestClientExecutor_ExecuteAsync_NonContentPartsJSON(t *testing.T) {
	conv := newTestConversation()
	conv.OnClientTool("simple", func(_ context.Context, _ ClientToolRequest) (any, error) {
		return map[string]any{"result": "ok"}, nil
	})

	exec := &clientExecutor{
		handlers:   conv.clientHandlers,
		handlersMu: &clientHandlersMuAccessor{conv: conv},
	}

	desc := &tools.ToolDescriptor{Name: "simple", Mode: "client"}
	result, err := exec.ExecuteAsync(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, result.Status)
	assert.NotEmpty(t, result.Content)
	assert.Empty(t, result.Parts)
}

func TestBuildToolResultMessages_ErrorBranch(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()

	// Add a resolution with error
	conv.resolvedStore.Add(&sdktools.ToolResolution{
		ID:    "call-err",
		Error: assert.AnError,
	})

	msgs, err := conv.buildToolResultMessages()
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	// The message content should contain "Tool error"
	assert.Contains(t, msgs[0].GetContent(), "Tool error")
}
