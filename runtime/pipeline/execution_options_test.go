package pipeline_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureIDsMiddleware captures RunID, SessionID, and ConversationID from ExecutionContext
type captureIDsMiddleware struct {
	runID          string
	sessionID      string
	conversationID string
}

func (m *captureIDsMiddleware) Process(ctx *pipeline.ExecutionContext, next func() error) error {
	m.runID = ctx.RunID
	m.sessionID = ctx.SessionID
	m.conversationID = ctx.ConversationID
	return next()
}

func (m *captureIDsMiddleware) StreamChunk(ctx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

func TestExecuteWithOptions_SetsIDs(t *testing.T) {
	// Create middleware to capture IDs
	capturer := &captureIDsMiddleware{}

	// Create pipeline with capture middleware
	p := pipeline.NewPipeline(capturer)
	defer p.Shutdown(context.Background())

	// Execute with options
	opts := &pipeline.ExecutionOptions{
		RunID:          "test-run-123",
		SessionID:      "session-456",
		ConversationID: "conv-789",
	}

	_, err := p.ExecuteWithOptions(opts, "user", "Hello")
	require.NoError(t, err)

	// Verify IDs were set
	assert.Equal(t, "test-run-123", capturer.runID)
	assert.Equal(t, "session-456", capturer.sessionID)
	assert.Equal(t, "conv-789", capturer.conversationID)
}

func TestExecuteWithOptions_NilOptions(t *testing.T) {
	// Create middleware to capture IDs
	capturer := &captureIDsMiddleware{}

	// Create pipeline with capture middleware
	p := pipeline.NewPipeline(capturer)
	defer p.Shutdown(context.Background())

	// Execute with nil options
	_, err := p.ExecuteWithOptions(nil, "user", "Hello")
	require.NoError(t, err)

	// Verify IDs are empty (defaults)
	assert.Empty(t, capturer.runID)
	assert.Empty(t, capturer.sessionID)
	assert.Empty(t, capturer.conversationID)
}

func TestExecuteWithOptions_EmptyOptions(t *testing.T) {
	// Create middleware to capture IDs
	capturer := &captureIDsMiddleware{}

	// Create pipeline with capture middleware
	p := pipeline.NewPipeline(capturer)
	defer p.Shutdown(context.Background())

	// Execute with empty options
	opts := &pipeline.ExecutionOptions{}
	_, err := p.ExecuteWithOptions(opts, "user", "Hello")
	require.NoError(t, err)

	// Verify IDs are empty
	assert.Empty(t, capturer.runID)
	assert.Empty(t, capturer.sessionID)
	assert.Empty(t, capturer.conversationID)
}

func TestExecuteWithOptions_CustomContext(t *testing.T) {
	// Create middleware to check context
	var contextChecked bool
	checkMiddleware := &testMiddleware{
		processFn: func(ctx *pipeline.ExecutionContext, next func() error) error {
			// Verify context is set
			assert.NotNil(t, ctx.Context)
			contextChecked = true
			return next()
		},
	}

	// Create pipeline
	p := pipeline.NewPipeline(checkMiddleware)
	defer p.Shutdown(context.Background())

	// Execute with custom context
	customCtx := context.WithValue(context.Background(), "test", "value")
	opts := &pipeline.ExecutionOptions{
		Context: customCtx,
	}

	_, err := p.ExecuteWithOptions(opts, "user", "Hello")
	require.NoError(t, err)
	assert.True(t, contextChecked)
}

func TestExecuteWithOptions_BackwardCompatibility(t *testing.T) {
	// Verify that existing Execute method still works without IDs
	capturer := &captureIDsMiddleware{}

	p := pipeline.NewPipeline(capturer)
	defer p.Shutdown(context.Background())

	// Execute with old method
	_, err := p.Execute(context.Background(), "user", "Hello")
	require.NoError(t, err)

	// IDs should be empty (backward compatible)
	assert.Empty(t, capturer.runID)
	assert.Empty(t, capturer.sessionID)
	assert.Empty(t, capturer.conversationID)
}

// testMiddleware is a helper for testing
type testMiddleware struct {
	processFn func(*pipeline.ExecutionContext, func() error) error
}

func (m *testMiddleware) Process(ctx *pipeline.ExecutionContext, next func() error) error {
	if m.processFn != nil {
		return m.processFn(ctx, next)
	}
	return next()
}

func (m *testMiddleware) StreamChunk(ctx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}
