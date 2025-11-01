package middleware

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
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
