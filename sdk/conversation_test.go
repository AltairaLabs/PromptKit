package sdk

import (
"context"
"os"
"testing"

"github.com/AltairaLabs/PromptKit/runtime/statestore"
"github.com/AltairaLabs/PromptKit/runtime/types"
"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
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
// Currently returns placeholder error
_, err := conv.Send(context.Background(), "hello")
assert.Error(t, err) // Expected - Send not implemented
})

t.Run("types.Message", func(t *testing.T) {
msg := &types.Message{Role: "user"}
msg.AddTextPart("hello")
_, err := conv.Send(context.Background(), msg)
assert.Error(t, err) // Expected - Send not implemented
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

// Currently falls back to Send which returns error
assert.Error(t, chunk.Error)
}
