package sdk

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	intpipeline "github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
	"github.com/stretchr/testify/assert"
)

func newTestConversation() *Conversation {
	return &Conversation{
		pack: &pack.Pack{
			ID: "test-pack",
			Prompts: map[string]*pack.Prompt{
				"chat": {ID: "chat", SystemTemplate: "You are helpful."},
			},
		},
		prompt:     &pack.Prompt{ID: "chat", SystemTemplate: "You are helpful."},
		promptName: "chat",
		config:     &config{},
		variables:  make(map[string]string),
		handlers:   make(map[string]ToolHandler),
	}
}

func TestConversationSetVar(t *testing.T) {
	conv := newTestConversation()

	conv.SetVar("name", "Alice")
	assert.Equal(t, "Alice", conv.GetVar("name"))

	conv.SetVar("name", "Bob")
	assert.Equal(t, "Bob", conv.GetVar("name"))
}

func TestConversationSetVars(t *testing.T) {
	conv := newTestConversation()

	conv.SetVars(map[string]any{
		"name": "Alice",
		"age":  30,
		"tier": "premium",
	})

	assert.Equal(t, "Alice", conv.GetVar("name"))
	assert.Equal(t, "30", conv.GetVar("age"))
	assert.Equal(t, "premium", conv.GetVar("tier"))
}

func TestConversationSetVarsFromEnv(t *testing.T) {
	conv := newTestConversation()

	// Set test env vars
	_ = os.Setenv("TEST_SDK_NAME", "TestUser")
	_ = os.Setenv("TEST_SDK_VALUE", "123")
	defer func() {
		_ = os.Unsetenv("TEST_SDK_NAME")
		_ = os.Unsetenv("TEST_SDK_VALUE")
	}()

	conv.SetVarsFromEnv("TEST_SDK_")

	assert.Equal(t, "TestUser", conv.GetVar("name"))
	assert.Equal(t, "123", conv.GetVar("value"))
}

func TestConversationGetVarNotSet(t *testing.T) {
	conv := newTestConversation()
	assert.Equal(t, "", conv.GetVar("nonexistent"))
}

func TestConversationOnTool(t *testing.T) {
	conv := newTestConversation()

	called := false
	conv.OnTool("test_tool", func(args map[string]any) (any, error) {
		called = true
		return "result", nil
	})

	// Verify handler was registered
	conv.handlersMu.RLock()
	handler, ok := conv.handlers["test_tool"]
	conv.handlersMu.RUnlock()

	assert.True(t, ok)
	assert.NotNil(t, handler)

	// Call the handler
	result, err := handler(map[string]any{})
	assert.NoError(t, err)
	assert.Equal(t, "result", result)
	assert.True(t, called)
}

func TestConversationOnToolCtx(t *testing.T) {
	conv := newTestConversation()

	var receivedCtx context.Context
	conv.OnToolCtx("ctx_tool", func(ctx context.Context, args map[string]any) (any, error) {
		receivedCtx = ctx
		return "ctx_result", nil
	})

	// Verify handler was registered (wrapped)
	conv.handlersMu.RLock()
	handler, ok := conv.handlers["ctx_tool"]
	conv.handlersMu.RUnlock()

	assert.True(t, ok)
	result, err := handler(map[string]any{})
	assert.NoError(t, err)
	assert.Equal(t, "ctx_result", result)
	assert.NotNil(t, receivedCtx)
}

func TestConversationOnTools(t *testing.T) {
	conv := newTestConversation()

	conv.OnTools(map[string]ToolHandler{
		"tool1": func(args map[string]any) (any, error) { return "r1", nil },
		"tool2": func(args map[string]any) (any, error) { return "r2", nil },
	})

	conv.handlersMu.RLock()
	assert.Len(t, conv.handlers, 2)
	_, ok1 := conv.handlers["tool1"]
	_, ok2 := conv.handlers["tool2"]
	conv.handlersMu.RUnlock()

	assert.True(t, ok1)
	assert.True(t, ok2)
}

func TestConversationMessages(t *testing.T) {
	conv := newTestConversation()

	// No state - should return nil
	assert.Nil(t, conv.Messages())

	// With state
	conv.state = &statestore.ConversationState{
		Messages: []types.Message{
			{Role: "user"},
			{Role: "assistant"},
		},
	}

	msgs := conv.Messages()
	assert.Len(t, msgs, 2)

	// Verify it's a copy
	msgs[0].Role = "modified"
	assert.Equal(t, "user", conv.state.Messages[0].Role)
}

func TestConversationClear(t *testing.T) {
	conv := newTestConversation()
	conv.state = &statestore.ConversationState{
		Messages:   []types.Message{{Role: "user"}},
		TokenCount: 100,
	}

	conv.Clear()

	assert.Nil(t, conv.state.Messages)
	assert.Equal(t, 0, conv.state.TokenCount)
}

func TestConversationClearNilState(t *testing.T) {
	conv := newTestConversation()
	// Should not panic with nil state
	conv.Clear()
}

func TestConversationFork(t *testing.T) {
	conv := newTestConversation()
	conv.SetVar("name", "Alice")
	conv.OnTool("tool1", func(args map[string]any) (any, error) { return nil, nil })
	conv.state = &statestore.ConversationState{
		ID:         "original",
		Messages:   []types.Message{{Role: "user"}},
		TokenCount: 50,
	}

	fork := conv.Fork()

	// Verify fork has same data
	assert.Equal(t, "Alice", fork.GetVar("name"))
	fork.handlersMu.RLock()
	_, hasHandler := fork.handlers["tool1"]
	fork.handlersMu.RUnlock()
	assert.True(t, hasHandler)

	// Verify fork state is independent
	assert.Contains(t, fork.state.ID, "fork")
	assert.Len(t, fork.Messages(), 1)

	// Modify fork - original should be unchanged
	fork.SetVar("name", "Bob")
	assert.Equal(t, "Alice", conv.GetVar("name"))
	assert.Equal(t, "Bob", fork.GetVar("name"))
}

func TestConversationClose(t *testing.T) {
	conv := newTestConversation()

	err := conv.Close()
	assert.NoError(t, err)
	assert.True(t, conv.closed)

	// Second close should be no-op
	err = conv.Close()
	assert.NoError(t, err)
}

func TestConversationSendWhenClosed(t *testing.T) {
	conv := newTestConversation()
	_ = conv.Close()

	_, err := conv.Send(context.Background(), "hello")
	assert.Error(t, err)
	assert.Equal(t, ErrConversationClosed, err)
}

func TestConversationSendMessageTypes(t *testing.T) {
	conv := newTestConversation()

	t.Run("string message", func(t *testing.T) {
		// Without a provider, Send should still work but return empty response
		resp, err := conv.Send(context.Background(), "hello")
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("types.Message", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		msg.AddTextPart("hello")
		resp, err := conv.Send(context.Background(), msg)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := conv.Send(context.Background(), 123)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be string or *types.Message")
	})
}

func TestConversationID(t *testing.T) {
	conv := newTestConversation()
	conv.id = "test-id-123"

	assert.Equal(t, "test-id-123", conv.ID())
}

func TestConversationToolRegistry(t *testing.T) {
	conv := newTestConversation()
	// Currently returns nil - placeholder
	assert.Nil(t, conv.ToolRegistry())
}

func TestConversationStream(t *testing.T) {
	conv := newTestConversation()

	ch := conv.Stream(context.Background(), "hello")
	chunk := <-ch

	// Stream falls back to Send, which works without a provider
	// but returns an empty response
	assert.NoError(t, chunk.Error)
	assert.Equal(t, ChunkDone, chunk.Type)
}

func TestApplyContentParts(t *testing.T) {
	conv := newTestConversation()
	autoDetail := "auto"

	t.Run("image URL part", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		parts := []any{
			imageURLPart{url: "https://example.com/image.jpg", detail: &autoDetail},
		}

		err := conv.applyContentParts(msg, parts)
		assert.NoError(t, err)
		assert.Len(t, msg.Parts, 1)
	})

	t.Run("image data part", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		parts := []any{
			imageDataPart{data: []byte("fake-image-data"), mimeType: "image/png", detail: &autoDetail},
		}

		err := conv.applyContentParts(msg, parts)
		assert.NoError(t, err)
		assert.Len(t, msg.Parts, 1)
	})

	t.Run("file part", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		parts := []any{
			filePart{name: "test.txt", data: []byte("file content")},
		}

		err := conv.applyContentParts(msg, parts)
		assert.NoError(t, err)
		assert.Len(t, msg.Parts, 1)
	})

	t.Run("unknown part type", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		parts := []any{
			struct{ unknown string }{unknown: "type"},
		}

		err := conv.applyContentParts(msg, parts)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown content part type")
	})

	t.Run("empty parts", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		err := conv.applyContentParts(msg, []any{})
		assert.NoError(t, err)
		assert.Len(t, msg.Parts, 0)
	})
}

func TestBuildToolRegistry(t *testing.T) {
	t.Run("no handlers returns nil", func(t *testing.T) {
		conv := newTestConversation()
		registry, descriptors := conv.buildToolRegistry()
		assert.Nil(t, registry)
		assert.Nil(t, descriptors)
	})

	t.Run("handler without pack tool is skipped", func(t *testing.T) {
		conv := newTestConversation()
		conv.OnTool("unknown_tool", func(args map[string]any) (any, error) {
			return nil, nil
		})

		registry, descriptors := conv.buildToolRegistry()
		// Registry created but no descriptors since tool not in pack
		assert.NotNil(t, registry)
		assert.Len(t, descriptors, 0)
	})

	t.Run("handler with pack tool creates descriptor", func(t *testing.T) {
		conv := newTestConversation()
		conv.pack.Tools = map[string]*pack.Tool{
			"test_tool": {
				Name:        "test_tool",
				Description: "A test tool",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"input": map[string]any{"type": "string"},
					},
				},
			},
		}

		conv.OnTool("test_tool", func(args map[string]any) (any, error) {
			return "result", nil
		})

		registry, descriptors := conv.buildToolRegistry()
		assert.NotNil(t, registry)
		assert.Len(t, descriptors, 1)
		assert.Equal(t, "test_tool", descriptors[0].Name)
	})
}

func TestGetVariables(t *testing.T) {
	conv := newTestConversation()
	conv.SetVar("key1", "value1")
	conv.SetVar("key2", "value2")

	vars := conv.getVariables()

	assert.Equal(t, "value1", vars["key1"])
	assert.Equal(t, "value2", vars["key2"])

	// Verify it's a copy
	vars["key1"] = "modified"
	assert.Equal(t, "value1", conv.GetVar("key1"))
}

func TestApplyPromptParameters(t *testing.T) {
	t.Run("no parameters", func(t *testing.T) {
		conv := newTestConversation()
		conv.prompt.Parameters = nil

		cfg := &intpipeline.Config{
			MaxTokens:   100,
			Temperature: 0.5,
		}

		conv.applyPromptParameters(cfg)
		assert.Equal(t, 100, cfg.MaxTokens)
		assert.Equal(t, float32(0.5), cfg.Temperature)
	})

	t.Run("with max tokens", func(t *testing.T) {
		conv := newTestConversation()
		maxTokens := 2048
		conv.prompt.Parameters = &pack.Parameters{
			MaxTokens: &maxTokens,
		}

		cfg := &intpipeline.Config{
			MaxTokens:   100,
			Temperature: 0.5,
		}

		conv.applyPromptParameters(cfg)
		assert.Equal(t, 2048, cfg.MaxTokens)
		assert.Equal(t, float32(0.5), cfg.Temperature)
	})

	t.Run("with temperature", func(t *testing.T) {
		conv := newTestConversation()
		temp := 0.9
		conv.prompt.Parameters = &pack.Parameters{
			Temperature: &temp,
		}

		cfg := &intpipeline.Config{
			MaxTokens:   100,
			Temperature: 0.5,
		}

		conv.applyPromptParameters(cfg)
		assert.Equal(t, 100, cfg.MaxTokens)
		assert.Equal(t, float32(0.9), cfg.Temperature)
	})
}

func TestAddMessageToHistory(t *testing.T) {
	conv := newTestConversation()

	msg := &types.Message{Role: "user"}
	msg.AddTextPart("Hello")

	conv.addMessageToHistory(msg)

	assert.NotNil(t, conv.state)
	assert.Len(t, conv.state.Messages, 1)
	assert.Equal(t, "user", conv.state.Messages[0].Role)
}

func TestEncodeBase64(t *testing.T) {
	data := []byte("hello world")
	encoded := encodeBase64(data)
	assert.Equal(t, "aGVsbG8gd29ybGQ=", encoded)
}

func TestHandlerAdapter(t *testing.T) {
	t.Run("name returns handler name", func(t *testing.T) {
		adapter := &handlerAdapter{
			name:    "test_handler",
			handler: func(args map[string]any) (any, error) { return nil, nil },
		}
		assert.Equal(t, "test_handler", adapter.Name())
	})

	t.Run("execute calls handler", func(t *testing.T) {
		called := false
		adapter := &handlerAdapter{
			name: "test",
			handler: func(args map[string]any) (any, error) {
				called = true
				return map[string]string{"result": "success"}, nil
			},
		}

		result, err := adapter.Execute(nil, []byte(`{"input": "test"}`))
		assert.NoError(t, err)
		assert.True(t, called)
		assert.Contains(t, string(result), "success")
	})

	t.Run("execute returns handler error", func(t *testing.T) {
		adapter := &handlerAdapter{
			name: "test",
			handler: func(args map[string]any) (any, error) {
				return nil, errors.New("handler error")
			},
		}

		_, err := adapter.Execute(nil, []byte(`{}`))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "handler error")
	})

	t.Run("execute returns parse error", func(t *testing.T) {
		adapter := &handlerAdapter{
			name:    "test",
			handler: func(args map[string]any) (any, error) { return nil, nil },
		}

		_, err := adapter.Execute(nil, []byte(`invalid json`))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse")
	})
}

func TestCheckClosed(t *testing.T) {
	t.Run("returns nil when not closed", func(t *testing.T) {
		conv := newTestConversation()
		err := conv.checkClosed()
		assert.NoError(t, err)
	})

	t.Run("returns error when closed", func(t *testing.T) {
		conv := newTestConversation()
		conv.closed = true
		err := conv.checkClosed()
		assert.Equal(t, ErrConversationClosed, err)
	})
}
