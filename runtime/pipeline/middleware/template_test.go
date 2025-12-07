package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestTemplateMiddleware_SimpleSubstitution(t *testing.T) {
	middleware := TemplateMiddleware()

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "Hello {{name}}, welcome to {{region}}!",
		Variables: map[string]string{
			"name":   "Alice",
			"region": "us-east",
		},
		Messages: []types.Message{},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)
	assert.Equal(t, "Hello Alice, welcome to us-east!", execCtx.Prompt)
	// Note: TemplateMiddleware does NOT add system prompt to Messages.
	// The ProviderMiddleware handles system prompts appropriately for each provider.
	assert.Len(t, execCtx.Messages, 0)
}

func TestTemplateMiddleware_NoVariables(t *testing.T) {
	middleware := TemplateMiddleware()

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "Hello, world!",
		Variables:    map[string]string{},
		Messages:     []types.Message{},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)
	assert.Equal(t, "Hello, world!", execCtx.Prompt)
	assert.Len(t, execCtx.Messages, 0)
}

func TestTemplateMiddleware_MultipleOccurrences(t *testing.T) {
	middleware := TemplateMiddleware()

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "{{greeting}} {{name}}! {{greeting}} again, {{name}}!",
		Variables: map[string]string{
			"greeting": "Hello",
			"name":     "Bob",
		},
		Messages: []types.Message{},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)
	assert.Equal(t, "Hello Bob! Hello again, Bob!", execCtx.Prompt)
}

func TestTemplateMiddleware_MissingVariable(t *testing.T) {
	middleware := TemplateMiddleware()

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "Hello {{name}}, welcome to {{region}}!",
		Variables: map[string]string{
			"name": "Charlie",
			// region is missing
		},
		Messages: []types.Message{},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)
	// Placeholder should remain unchanged
	assert.Equal(t, "Hello Charlie, welcome to {{region}}!", execCtx.Prompt)
}

func TestTemplateMiddleware_UpdatesExistingSystemMessage(t *testing.T) {
	middleware := TemplateMiddleware()

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "Updated: {{value}}",
		Variables: map[string]string{
			"value": "test",
		},
		Messages: []types.Message{
			{Role: "system", Content: "Old system message"},
			{Role: "user", Content: "User message"},
		},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)
	// TemplateMiddleware does not modify Messages array, only sets Prompt
	assert.Equal(t, "Updated: test", execCtx.Prompt)
	assert.Len(t, execCtx.Messages, 2)
	assert.Equal(t, "system", execCtx.Messages[0].Role)
	assert.Equal(t, "Old system message", execCtx.Messages[0].Content)
	assert.Equal(t, "user", execCtx.Messages[1].Role)
	assert.Equal(t, "User message", execCtx.Messages[1].Content)
}

func TestTemplateMiddleware_EmptyPrompt(t *testing.T) {
	middleware := TemplateMiddleware()

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "",
		Variables:    map[string]string{},
		Messages:     []types.Message{},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)
	assert.Equal(t, "", execCtx.Prompt)
	assert.Len(t, execCtx.Messages, 0)
}

func TestTemplateMiddleware_SpecialCharacters(t *testing.T) {
	middleware := TemplateMiddleware()

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "{{special}}",
		Variables: map[string]string{
			"special": "Hello\nWorld\t!",
		},
		Messages: []types.Message{},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)
	assert.Equal(t, "Hello\nWorld\t!", execCtx.Prompt)
}

func TestTemplateMiddleware_CallsNext(t *testing.T) {
	middleware := TemplateMiddleware()

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "Test",
		Variables:    map[string]string{},
		Messages:     []types.Message{},
	}

	execCtx.Context = context.Background()
	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	assert.NoError(t, err)
}

func TestTemplateMiddleware_StreamChunk_NoOp(t *testing.T) {
	middleware := TemplateMiddleware()

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "Test",
		Variables:    map[string]string{},
		Messages:     []types.Message{},
	}
	execCtx.Context = context.Background()

	chunk := &providers.StreamChunk{
		Content:      "test content",
		FinishReason: nil,
	}

	// StreamChunk should be a no-op and return nil
	err := middleware.(*templateMiddleware).StreamChunk(execCtx, chunk)

	assert.NoError(t, err)
}

func TestTemplateMiddleware_ConversationStartedEvent(t *testing.T) {
	middleware := TemplateMiddleware()

	// Create a mock event bus to capture events
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-1", "session-1", "conv-1")

	var receivedEvent *events.Event
	done := make(chan struct{})
	bus.Subscribe(events.EventConversationStarted, func(e *events.Event) {
		receivedEvent = e
		close(done)
	})

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "You are a helpful assistant",
		Variables:    map[string]string{},
		Messages:     []types.Message{}, // Empty - first turn
		EventEmitter: emitter,
	}
	execCtx.Context = context.Background()

	err := middleware.Process(execCtx, func() error { return nil })
	assert.NoError(t, err)

	// Wait for event with timeout
	select {
	case <-done:
		// Event received
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for conversation.started event")
	}

	assert.NotNil(t, receivedEvent)
	data, ok := receivedEvent.Data.(events.ConversationStartedData)
	assert.True(t, ok)
	assert.Equal(t, "You are a helpful assistant", data.SystemPrompt)
}

func TestTemplateMiddleware_ConversationStartedEvent_NotEmittedOnSubsequentTurns(t *testing.T) {
	middleware := TemplateMiddleware()

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-1", "session-1", "conv-1")

	eventCount := 0
	bus.Subscribe(events.EventConversationStarted, func(e *events.Event) {
		eventCount++
	})

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "You are a helpful assistant",
		Variables:    map[string]string{},
		Messages: []types.Message{
			{Role: "user", Content: "First message"},
			{Role: "assistant", Content: "Response"},
		}, // More than 1 message - not first turn
		EventEmitter: emitter,
	}
	execCtx.Context = context.Background()

	err := middleware.Process(execCtx, func() error { return nil })
	assert.NoError(t, err)

	// Give time for any events to be processed
	time.Sleep(50 * time.Millisecond)

	// Should NOT emit conversation.started on subsequent turns
	assert.Equal(t, 0, eventCount)
}

func TestTemplateMiddleware_ConversationStartedEvent_NotEmittedWithEmptyPrompt(t *testing.T) {
	middleware := TemplateMiddleware()

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-1", "session-1", "conv-1")

	eventCount := 0
	bus.Subscribe(events.EventConversationStarted, func(e *events.Event) {
		eventCount++
	})

	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "", // Empty prompt
		Variables:    map[string]string{},
		Messages:     []types.Message{},
		EventEmitter: emitter,
	}
	execCtx.Context = context.Background()

	err := middleware.Process(execCtx, func() error { return nil })
	assert.NoError(t, err)

	// Give time for any events to be processed
	time.Sleep(50 * time.Millisecond)

	// Should NOT emit conversation.started with empty prompt
	assert.Equal(t, 0, eventCount)
}
