package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/stretchr/testify/assert"
)

// Test middleware implementations
type noOpMiddleware struct{}

func (m *noOpMiddleware) Process(ctx *ExecutionContext, next func() error) error {
	return next()
}

func (m *noOpMiddleware) StreamChunk(ctx *ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

type funcMiddleware struct {
	fn func(*ExecutionContext, func() error) error
}

func (m *funcMiddleware) Process(ctx *ExecutionContext, next func() error) error {
	return m.fn(ctx, next)
}

func (m *funcMiddleware) StreamChunk(ctx *ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

func TestNewPipeline(t *testing.T) {
	middleware1 := &noOpMiddleware{}
	middleware2 := &noOpMiddleware{}

	p := NewPipeline(middleware1, middleware2)

	assert.NotNil(t, p)
	assert.Len(t, p.middleware, 2)
}

func TestPipeline_Execute_EmptyPipeline(t *testing.T) {
	p := NewPipeline()

	result, err := p.Execute(context.Background(), "", "")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Metadata)
	assert.NotNil(t, result.Trace)
}

func TestPipeline_Execute_SingleMiddleware(t *testing.T) {
	executed := false
	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			executed = true
			execCtx.Prompt = "Modified"
			return next()
		},
	}

	p := NewPipeline(middleware)

	result, err := p.Execute(context.Background(), "user", "test")

	assert.NoError(t, err)
	assert.True(t, executed)
	assert.NotNil(t, result)
}

func TestPipeline_Execute_MultipleMiddleware(t *testing.T) {
	var executionOrder []string

	middleware1 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		executionOrder = append(executionOrder, "m1-before")
		err := next()
		executionOrder = append(executionOrder, "m1-after")
		return err
	}}
	middleware2 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		executionOrder = append(executionOrder, "m2-before")
		err := next()
		executionOrder = append(executionOrder, "m2-after")
		return err
	}}

	middleware3 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		executionOrder = append(executionOrder, "m3-before")
		err := next()
		executionOrder = append(executionOrder, "m3-after")
		return err
	}}

	p := NewPipeline(middleware1, middleware2, middleware3)

	result, err := p.Execute(context.Background(), "", "")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, []string{
		"m1-before",
		"m2-before",
		"m3-before",
		"m3-after",
		"m2-after",
		"m1-after",
	}, executionOrder)
}

func TestPipeline_Execute_ErrorInMiddleware(t *testing.T) {
	expectedErr := errors.New("middleware error")

	middleware1 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		return next()
	}}

	middleware2 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		return expectedErr
	}}

	middleware3 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		return next() // Should not be executed
	}}

	p := NewPipeline(middleware1, middleware2, middleware3)

	result, err := p.Execute(context.Background(), "", "")

	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.NotNil(t, result) // Result is still returned even on error
}

func TestPipeline_Execute_MiddlewareModifiesContext(t *testing.T) {
	var capturedSystemPrompt string
	var capturedVariables map[string]string
	var capturedMetadata map[string]interface{}

	middleware1 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		execCtx.SystemPrompt = "Modified by m1"
		capturedSystemPrompt = execCtx.SystemPrompt
		return next()
	}}

	middleware2 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		execCtx.Variables = map[string]string{"key": "value"}
		capturedVariables = execCtx.Variables
		return next()
	}}

	middleware3 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		execCtx.Metadata["custom"] = "data"
		capturedMetadata = execCtx.Metadata
		return next()
	}}

	p := NewPipeline(middleware1, middleware2, middleware3)

	result, err := p.Execute(context.Background(), "", "")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Modified by m1", capturedSystemPrompt)
	assert.Equal(t, "value", capturedVariables["key"])
	assert.Equal(t, "data", capturedMetadata["custom"])
	// Metadata should be in result
	assert.Equal(t, "data", result.Metadata["custom"])
}

func TestPipeline_Execute_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	middleware := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		// Check if context is cancelled
		select {
		case <-execCtx.Context.Done():
			return execCtx.Context.Err()
		default:
			return next()
		}
	}}

	p := NewPipeline(middleware)

	_, err := p.Execute(ctx, "", "")

	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPipeline_Execute_SkipNextCall(t *testing.T) {
	var executionOrder []string

	middleware1 := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			executionOrder = append(executionOrder, "m1")
			// Don't call next() - short-circuit the pipeline
			return nil
		}}

	middleware2 := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			executionOrder = append(executionOrder, "m2")
			return next()
		}}

	p := NewPipeline(middleware1, middleware2)

	result, err := p.Execute(context.Background(), "", "")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, []string{"m1"}, executionOrder)
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{
		Type:    "banned_words",
		Details: "Found banned word 'test'",
	}

	msg := err.Error()

	assert.Contains(t, msg, "validation failed")
	assert.Contains(t, msg, "banned_words")
	assert.Contains(t, msg, "Found banned word 'test'")
}

func TestExecutionContext_Initialization(t *testing.T) {
	execCtx := &ExecutionContext{
		SystemPrompt: "Test prompt",
		Variables: map[string]string{
			"region": "us-east",
		},
		AllowedTools: []string{"tool1", "tool2"},
	}

	assert.Equal(t, "Test prompt", execCtx.SystemPrompt)
	assert.Equal(t, "us-east", execCtx.Variables["region"])
	assert.Len(t, execCtx.AllowedTools, 2)
	assert.Nil(t, execCtx.Metadata) // Not initialized until Execute()
}

func TestPipeline_Execute_InitializesMetadata(t *testing.T) {
	p := NewPipeline()

	result, err := p.Execute(context.Background(), "", "")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Metadata)
	assert.Empty(t, result.Metadata)
}

func TestPipeline_Execute_PreservesExistingMetadata(t *testing.T) {
	m := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			// Middleware can add to metadata
			execCtx.Metadata["new"] = "addition"
			return next()
		},
	}

	p := NewPipeline(m)

	result, err := p.Execute(context.Background(), "", "")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "addition", result.Metadata["new"])
}
