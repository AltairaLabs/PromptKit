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

func TestNewPipelineWithConfig_ValidatesMaxConcurrentExecutions(t *testing.T) {
	tests := []struct {
		name        string
		config      *PipelineRuntimeConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "zero MaxConcurrentExecutions (filled with default)",
			config: &PipelineRuntimeConfig{
				MaxConcurrentExecutions: 0,
				StreamBufferSize:        100,
			},
			expectError: false, // Zero values are filled with defaults
		},
		{
			name: "negative MaxConcurrentExecutions",
			config: &PipelineRuntimeConfig{
				MaxConcurrentExecutions: -5,
				StreamBufferSize:        100,
			},
			expectError: true,
			errorMsg:    "MaxConcurrentExecutions must be non-negative",
		},
		{
			name: "valid MaxConcurrentExecutions",
			config: &PipelineRuntimeConfig{
				MaxConcurrentExecutions: 10,
				StreamBufferSize:        100,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewPipelineWithConfigValidated(tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, p)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, p)
				// Verify defaults were applied for zero values
				if tt.config.MaxConcurrentExecutions == 0 {
					assert.Equal(t, 100, p.config.MaxConcurrentExecutions, "zero MaxConcurrentExecutions should be filled with default")
				}
			}
		})
	}
}

func TestNewPipelineWithConfig_ValidatesStreamBufferSize(t *testing.T) {
	tests := []struct {
		name        string
		config      *PipelineRuntimeConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "zero StreamBufferSize (filled with default)",
			config: &PipelineRuntimeConfig{
				MaxConcurrentExecutions: 10,
				StreamBufferSize:        0,
			},
			expectError: false, // Zero values are filled with defaults
		},
		{
			name: "negative StreamBufferSize",
			config: &PipelineRuntimeConfig{
				MaxConcurrentExecutions: 10,
				StreamBufferSize:        -10,
			},
			expectError: true,
			errorMsg:    "StreamBufferSize must be non-negative",
		},
		{
			name: "valid StreamBufferSize",
			config: &PipelineRuntimeConfig{
				MaxConcurrentExecutions: 10,
				StreamBufferSize:        50,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewPipelineWithConfigValidated(tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, p)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, p)
				// Verify defaults were applied for zero values
				if tt.config.StreamBufferSize == 0 {
					assert.Equal(t, 100, p.config.StreamBufferSize, "zero StreamBufferSize should be filled with default")
				}
			}
		})
	}
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

func TestPipeline_Execute_MiddlewareShortCircuit(t *testing.T) {
	// Test that middleware can intentionally short-circuit without warnings
	var executionOrder []string

	// First middleware runs normally
	middleware1 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		executionOrder = append(executionOrder, "m1")
		return next()
	}}

	// Second middleware short-circuits (e.g., auth rejection, validation failure)
	middleware2 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		executionOrder = append(executionOrder, "m2-shortcircuit")
		// Set ShortCircuit flag to indicate intentional early exit
		execCtx.ShortCircuit = true
		return nil // No error, but don't call next()
	}}

	// Third middleware should not run
	middleware3 := &funcMiddleware{fn: func(execCtx *ExecutionContext, next func() error) error {
		executionOrder = append(executionOrder, "m3")
		return next()
	}}

	p := NewPipeline(middleware1, middleware2, middleware3)

	result, err := p.Execute(context.Background(), "", "")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, []string{"m1", "m2-shortcircuit"}, executionOrder)
	// Middleware 3 should not have executed
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

func TestPipeline_ExecuteStream_ResourceCleanupOnCancelledContext(t *testing.T) {
	// Test that semaphore acquisition happens inside goroutine, so cancelled
	// contexts are handled gracefully without resource leaks
	config := &PipelineRuntimeConfig{
		MaxConcurrentExecutions: 1, // Only allow one execution
		StreamBufferSize:        10,
	}

	middleware := &funcMiddleware{
		fn: func(execCtx *ExecutionContext, next func() error) error {
			return next()
		},
	}

	p, err := NewPipelineWithConfigValidated(config, middleware)
	assert.NoError(t, err)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// ExecuteStream now returns the channel immediately (doesn't wait for Acquire)
	// The error will be sent through the channel instead
	stream, err := p.ExecuteStream(ctx, "user", "test")
	assert.NoError(t, err) // No error on main thread anymore
	assert.NotNil(t, stream)

	// Read from stream - should get error chunk due to cancelled context
	chunk := <-stream
	assert.NotNil(t, chunk.Error)

	// Channel should be closed after error
	_, ok := <-stream
	assert.False(t, ok, "channel should be closed")

	// Now try another execution with valid context - should succeed (no leaked resources)
	validCtx := context.Background()
	stream2, err2 := p.ExecuteStream(validCtx, "user", "test2")
	assert.NoError(t, err2)
	assert.NotNil(t, stream2)

	// Drain the stream
	for range stream2 {
		// Just consume
	}
}

func TestPipeline_ExecuteStream_NoLeakOnPanic(t *testing.T) {
	// This test demonstrates that if something unexpected happens between
	// semaphore.Acquire and go routine launch, resources could leak.
	// The fix moves Acquire/wg.Add into the goroutine.

	config := &PipelineRuntimeConfig{
		MaxConcurrentExecutions: 2,
		StreamBufferSize:        10,
	}

	p, err := NewPipelineWithConfigValidated(config)
	assert.NoError(t, err)

	// First execution should work
	stream1, err := p.ExecuteStream(context.Background(), "user", "test1")
	assert.NoError(t, err)
	assert.NotNil(t, stream1)

	// Drain it
	for range stream1 {
	}

	// Second execution should also work (no leaked resources)
	stream2, err := p.ExecuteStream(context.Background(), "user", "test2")
	assert.NoError(t, err)
	assert.NotNil(t, stream2)

	for range stream2 {
	}
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
