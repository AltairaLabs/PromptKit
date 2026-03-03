package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
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
