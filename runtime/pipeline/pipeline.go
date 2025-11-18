package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"golang.org/x/sync/semaphore"
)

// Error constants
var (
	ErrPipelineShuttingDown = errors.New("pipeline is shutting down")
)

// Error message format strings
const (
	errFailedToAcquireSlot    = "failed to acquire execution slot: %w"
	errShutdownTimeout        = "shutdown timeout after %v"
	errMiddlewareChainBroken  = "Middleware did not call next() - chain is broken"
	errMiddlewareMultipleNext = "Middleware called next() multiple times"
	errValidationFailed       = "validation failed (%s): %s"
)

// PipelineRuntimeConfig defines runtime configuration options for pipeline execution.
// All fields have sensible defaults and are optional.
type PipelineRuntimeConfig struct {
	// MaxConcurrentExecutions limits the number of concurrent pipeline executions.
	// Default: 100
	MaxConcurrentExecutions int

	// StreamBufferSize sets the buffer size for streaming output channels.
	// Default: 100
	StreamBufferSize int

	// ExecutionTimeout sets the maximum duration for a single pipeline execution.
	// Set to 0 to disable timeout.
	// Default: 30 seconds
	ExecutionTimeout time.Duration

	// GracefulShutdownTimeout sets the maximum time to wait for in-flight executions during shutdown.
	// Default: 10 seconds
	GracefulShutdownTimeout time.Duration
}

// DefaultPipelineRuntimeConfig returns a PipelineRuntimeConfig with sensible default values.
func DefaultPipelineRuntimeConfig() *PipelineRuntimeConfig {
	return &PipelineRuntimeConfig{
		MaxConcurrentExecutions: 100,
		StreamBufferSize:        100,
		ExecutionTimeout:        30 * time.Second,
		GracefulShutdownTimeout: 10 * time.Second,
	}
}

// Pipeline chains middleware together in sequence.
type Pipeline struct {
	middleware []Middleware
	config     *PipelineRuntimeConfig
	semaphore  *semaphore.Weighted
	wg         sync.WaitGroup
	shutdown   chan struct{}
	shutdownMu sync.RWMutex
	isShutdown bool
}

// NewPipeline creates a new pipeline with the given middleware.
// Uses default runtime configuration.
func NewPipeline(middleware ...Middleware) *Pipeline {
	p, _ := NewPipelineWithConfigValidated(nil, middleware...)
	return p
}

// NewPipelineWithConfig creates a new pipeline with the given configuration and middleware.
// If config is nil, uses default configuration.
// Note: This function does not validate config values for backward compatibility.
// Use NewPipelineWithConfigValidated for validation.
func NewPipelineWithConfig(config *PipelineRuntimeConfig, middleware ...Middleware) *Pipeline {
	p, _ := NewPipelineWithConfigValidated(config, middleware...)
	return p
}

// NewPipelineWithConfigValidated creates a new pipeline with validation.
// Returns an error if config contains invalid values (negative numbers).
// If config is nil, uses default configuration.
// If config has zero values for some fields, they are filled with defaults.
func NewPipelineWithConfigValidated(config *PipelineRuntimeConfig, middleware ...Middleware) (*Pipeline, error) {
	if config == nil {
		config = DefaultPipelineRuntimeConfig()
	} else {
		// Validate negative values first (truly invalid)
		if config.MaxConcurrentExecutions < 0 {
			return nil, fmt.Errorf("invalid pipeline config: MaxConcurrentExecutions must be non-negative, got %d", config.MaxConcurrentExecutions)
		}
		if config.StreamBufferSize < 0 {
			return nil, fmt.Errorf("invalid pipeline config: StreamBufferSize must be non-negative, got %d", config.StreamBufferSize)
		}

		// Merge with defaults for any zero values (zero means "not set, use default")
		defaults := DefaultPipelineRuntimeConfig()
		if config.MaxConcurrentExecutions == 0 {
			config.MaxConcurrentExecutions = defaults.MaxConcurrentExecutions
		}
		if config.StreamBufferSize == 0 {
			config.StreamBufferSize = defaults.StreamBufferSize
		}
		if config.ExecutionTimeout == 0 {
			config.ExecutionTimeout = defaults.ExecutionTimeout
		}
		if config.GracefulShutdownTimeout == 0 {
			config.GracefulShutdownTimeout = defaults.GracefulShutdownTimeout
		}
	}

	return &Pipeline{
		middleware: middleware,
		config:     config,
		semaphore:  semaphore.NewWeighted(int64(config.MaxConcurrentExecutions)),
		shutdown:   make(chan struct{}),
	}, nil
}

// Shutdown gracefully shuts down the pipeline, waiting for in-flight executions to complete.
// Returns an error if shutdown times out according to GracefulShutdownTimeout.
func (p *Pipeline) Shutdown(ctx context.Context) error {
	p.shutdownMu.Lock()
	if p.isShutdown {
		p.shutdownMu.Unlock()
		return nil // Already shut down
	}
	p.isShutdown = true
	close(p.shutdown)
	p.shutdownMu.Unlock()

	// Wait for in-flight executions with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	shutdownCtx, cancel := context.WithTimeout(ctx, p.config.GracefulShutdownTimeout)
	defer cancel()

	select {
	case <-done:
		return nil
	case <-shutdownCtx.Done():
		return fmt.Errorf(errShutdownTimeout, p.config.GracefulShutdownTimeout)
	}
}

// isShuttingDown checks if the pipeline is shutting down
func (p *Pipeline) isShuttingDown() bool {
	p.shutdownMu.RLock()
	defer p.shutdownMu.RUnlock()
	return p.isShutdown
}

// ExecuteWithOptions runs the pipeline with the given role, content, and execution options.
// This method provides fine-grained control over execution including RunID, SessionID, and ConversationID.
// Returns the ExecutionResult containing messages, response, trace, and metadata.
func (p *Pipeline) ExecuteWithOptions(opts *ExecutionOptions, role, content string) (*ExecutionResult, error) {
	if opts == nil {
		opts = &ExecutionOptions{}
	}

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// Check if shutting down
	if p.isShuttingDown() {
		return nil, ErrPipelineShuttingDown
	}

	// Acquire semaphore for concurrency control
	if err := p.semaphore.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf(errFailedToAcquireSlot, err)
	}
	defer p.semaphore.Release(1)

	// Track execution for graceful shutdown
	p.wg.Add(1)
	defer p.wg.Done()

	// Apply execution timeout if configured
	execCtx := ctx
	var cancel context.CancelFunc
	if p.config.ExecutionTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, p.config.ExecutionTimeout)
		defer cancel()
	}

	// Create fresh internal execution context
	internalCtx := &ExecutionContext{
		Context:        execCtx,
		RunID:          opts.RunID,
		SessionID:      opts.SessionID,
		ConversationID: opts.ConversationID,
		Messages:       []types.Message{},
		Metadata:       make(map[string]interface{}),
		Trace: ExecutionTrace{
			LLMCalls:  []LLMCall{},
			Events:    []TraceEvent{},
			StartedAt: time.Now(),
		},
	}

	// Append the new message to the conversation (if role is provided)
	if role != "" {
		internalCtx.Messages = append(internalCtx.Messages, types.Message{
			Role:    role,
			Content: content,
		})
	}

	// Execute the middleware chain
	err := p.executeChain(internalCtx, 0)

	// Mark execution as complete
	now := time.Now()
	internalCtx.Trace.CompletedAt = &now

	// Return the first error encountered (if any)
	if internalCtx.Error != nil {
		err = internalCtx.Error
	}

	// Return immutable result
	return &ExecutionResult{
		Messages: internalCtx.Messages,
		Response: internalCtx.Response,
		Trace:    internalCtx.Trace,
		CostInfo: internalCtx.CostInfo,
		Metadata: internalCtx.Metadata,
	}, err
}

// Execute runs the pipeline with the given role and content, returning the execution result.
// It creates a fresh internal ExecutionContext for each call, preventing state contamination.
// The role and content parameters are used to create the initial user message.
// If role is empty, no message is appended (useful for testing).
// Returns the ExecutionResult containing messages, response, trace, and metadata.
func (p *Pipeline) Execute(ctx context.Context, role string, content string) (*ExecutionResult, error) {
	// Check if shutting down
	if p.isShuttingDown() {
		return nil, ErrPipelineShuttingDown
	}

	// Acquire semaphore for concurrency control
	if err := p.semaphore.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf(errFailedToAcquireSlot, err)
	}
	defer p.semaphore.Release(1)

	// Track execution for graceful shutdown
	p.wg.Add(1)
	defer p.wg.Done()

	// Apply execution timeout if configured
	execCtx := ctx
	var cancel context.CancelFunc
	if p.config.ExecutionTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, p.config.ExecutionTimeout)
		defer cancel()
	}

	// Create fresh internal execution context
	internalCtx := &ExecutionContext{
		Context:  execCtx,
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
		Trace: ExecutionTrace{
			LLMCalls:  []LLMCall{},
			Events:    []TraceEvent{},
			StartedAt: time.Now(),
		},
	}

	// Append the new message to the conversation (if role is provided)
	if role != "" {
		internalCtx.Messages = append(internalCtx.Messages, types.Message{
			Role:    role,
			Content: content,
		})
	}

	// Execute the middleware chain
	err := p.executeChain(internalCtx, 0)

	// Mark execution as complete
	now := time.Now()
	internalCtx.Trace.CompletedAt = &now

	// Return the first error encountered (if any)
	if internalCtx.Error != nil {
		err = internalCtx.Error
	}

	// Return immutable result
	return &ExecutionResult{
		Messages: internalCtx.Messages,
		Response: internalCtx.Response,
		Trace:    internalCtx.Trace,
		CostInfo: internalCtx.CostInfo,
		Metadata: internalCtx.Metadata,
	}, err
}

// ExecuteWithMessage runs the pipeline with a complete Message object, returning the execution result.
// This method allows callers to provide a fully-populated message with all fields (Meta, Timestamp, etc.)
// rather than just role and content. This is useful when you need to preserve metadata or other
// message properties through the pipeline execution.
//
// The message is added to the execution context as-is, preserving all fields including:
// - Meta (metadata, raw responses, validation info)
// - Timestamp
// - ToolCalls
// - CostInfo
// - Validations
//
// Middleware can still modify the message during execution if needed.
// Returns the ExecutionResult containing messages, response, trace, and metadata.
func (p *Pipeline) ExecuteWithMessage(ctx context.Context, message types.Message) (*ExecutionResult, error) {
	// Check if shutting down
	if p.isShuttingDown() {
		return nil, ErrPipelineShuttingDown
	}

	// Acquire semaphore for concurrency control
	if err := p.semaphore.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf(errFailedToAcquireSlot, err)
	}
	defer p.semaphore.Release(1)

	// Track execution for graceful shutdown
	p.wg.Add(1)
	defer p.wg.Done()

	// Apply execution timeout if configured
	execCtx := ctx
	var cancel context.CancelFunc
	if p.config.ExecutionTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, p.config.ExecutionTimeout)
		defer cancel()
	}

	// Create fresh internal execution context
	internalCtx := &ExecutionContext{
		Context:  execCtx,
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
		Trace: ExecutionTrace{
			LLMCalls:  []LLMCall{},
			Events:    []TraceEvent{},
			StartedAt: time.Now(),
		},
	}

	// Append the complete message to the conversation
	internalCtx.Messages = append(internalCtx.Messages, message)

	// Execute the middleware chain
	err := p.executeChain(internalCtx, 0)

	// Mark execution as complete
	now := time.Now()
	internalCtx.Trace.CompletedAt = &now

	// Return the first error encountered (if any)
	if internalCtx.Error != nil {
		err = internalCtx.Error
	}

	// Return immutable result
	return &ExecutionResult{
		Messages: internalCtx.Messages,
		Response: internalCtx.Response,
		Trace:    internalCtx.Trace,
		CostInfo: internalCtx.CostInfo,
		Metadata: internalCtx.Metadata,
	}, err
}

// ExecuteStream runs the pipeline in streaming mode, returning a channel of stream chunks.
// It creates a fresh internal ExecutionContext for each call, preventing state contamination.
// The role and content parameters are used to create the initial user message.
// If role is empty, no message is appended (useful for testing).
// The pipeline executes in the background and closes the channel when complete.
// The final chunk will contain the ExecutionResult in the FinalResult field.
func (p *Pipeline) ExecuteStream(
	ctx context.Context,
	role string,
	content string,
) (<-chan providers.StreamChunk, error) {
	// Check if shutting down
	if p.isShuttingDown() {
		return nil, ErrPipelineShuttingDown
	}

	// Apply execution timeout if configured
	execCtx, cancel := p.applyExecutionTimeout(ctx)

	// Create fresh internal execution context
	internalCtx := p.createStreamContext(execCtx)

	// Append the new message to the conversation (if role is provided)
	if role != "" {
		internalCtx.Messages = append(internalCtx.Messages, types.Message{
			Role:    role,
			Content: content,
		})
	}

	// Execute pipeline in background
	// Note: Semaphore acquisition and waitgroup tracking now happen inside the goroutine
	// to ensure proper cleanup even if goroutine fails to start
	go p.executeStreamBackground(internalCtx, cancel)

	return internalCtx.StreamOutput, nil
}

// applyExecutionTimeout applies execution timeout to context if configured
func (p *Pipeline) applyExecutionTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if p.config.ExecutionTimeout > 0 {
		return context.WithTimeout(ctx, p.config.ExecutionTimeout)
	}
	return ctx, nil
}

// createStreamContext creates a new ExecutionContext configured for streaming
func (p *Pipeline) createStreamContext(ctx context.Context) *ExecutionContext {
	streamChan := make(chan providers.StreamChunk, p.config.StreamBufferSize)

	internalCtx := &ExecutionContext{
		Context:  ctx,
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
		Trace: ExecutionTrace{
			LLMCalls:  []LLMCall{},
			Events:    []TraceEvent{},
			StartedAt: time.Now(),
		},
		StreamMode:   true,
		Response:     &Response{},
		StreamOutput: streamChan,
	}

	// Set up chunk processing handler
	internalCtx.streamChunkHandler = p.createStreamChunkHandler(internalCtx)

	return internalCtx
}

// createStreamChunkHandler creates a function that processes stream chunks through middleware
func (p *Pipeline) createStreamChunkHandler(internalCtx *ExecutionContext) func(*providers.StreamChunk) error {
	return func(chunk *providers.StreamChunk) error {
		// Run all middleware StreamChunk hooks
		for _, m := range p.middleware {
			if err := m.StreamChunk(internalCtx, chunk); err != nil {
				return err
			}
			// Check if middleware interrupted the stream
			if internalCtx.StreamInterrupted {
				return nil
			}
		}
		return nil
	}
}

// executeStreamBackground runs the pipeline execution in the background
func (p *Pipeline) executeStreamBackground(internalCtx *ExecutionContext, cancel context.CancelFunc) {
	// Acquire semaphore for concurrency control
	// This is now done inside the goroutine to prevent resource leaks if goroutine fails to start
	if err := p.semaphore.Acquire(internalCtx.Context, 1); err != nil {
		// Send error chunk and close immediately
		internalCtx.StreamOutput <- providers.StreamChunk{
			Error:        err,
			FinishReason: strPtr("error"),
		}
		close(internalCtx.StreamOutput)
		if cancel != nil {
			cancel()
		}
		return
	}

	// Track execution for graceful shutdown
	p.wg.Add(1)

	defer func() {
		close(internalCtx.StreamOutput)
		p.semaphore.Release(1)
		p.wg.Done()
		if cancel != nil {
			cancel()
		}
	}()

	err := p.executeChain(internalCtx, 0)

	// Mark execution as complete
	now := time.Now()
	internalCtx.Trace.CompletedAt = &now

	// Use the first error encountered (if any)
	if internalCtx.Error != nil {
		err = internalCtx.Error
	}

	// Send final chunk
	p.sendFinalStreamChunk(internalCtx, err)
}

// sendFinalStreamChunk builds and sends the final chunk with execution results
func (p *Pipeline) sendFinalStreamChunk(internalCtx *ExecutionContext, err error) {
	result := &ExecutionResult{
		Messages: internalCtx.Messages,
		Response: internalCtx.Response,
		Trace:    internalCtx.Trace,
		CostInfo: internalCtx.CostInfo,
		Metadata: internalCtx.Metadata,
	}

	finalChunk := providers.StreamChunk{
		FinishReason: strPtr("stop"),
		FinalResult:  result,
	}

	if err != nil {
		finalChunk.Error = err
		finalChunk.FinishReason = strPtr("error")
	} else if internalCtx.StreamInterrupted {
		finalChunk.FinishReason = strPtr("interrupted")
	}

	internalCtx.StreamOutput <- finalChunk
}

// ExecuteStreamWithMessage runs the pipeline in streaming mode with a complete Message object.
// This method allows callers to provide a fully-populated message with all fields (Meta, Timestamp, etc.)
// rather than just role and content. The message is added to the execution context as-is,
// preserving all fields including Meta, Timestamp, ToolCalls, CostInfo, and Validations.
//
// The pipeline executes in the background and closes the channel when complete.
// The final chunk will contain the ExecutionResult in the FinalResult field.
// Returns a channel of StreamChunk objects that will be closed when execution completes.
func (p *Pipeline) ExecuteStreamWithMessage(
	ctx context.Context,
	message types.Message,
) (<-chan providers.StreamChunk, error) {
	// Check if shutting down
	if p.isShuttingDown() {
		return nil, ErrPipelineShuttingDown
	}

	// Acquire semaphore for concurrency control
	if err := p.semaphore.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf(errFailedToAcquireSlot, err)
	}

	// Track execution for graceful shutdown
	p.wg.Add(1)

	// Apply execution timeout if configured
	execCtx, cancel := p.applyExecutionTimeout(ctx)

	// Create fresh internal execution context
	internalCtx := p.createStreamContext(execCtx)

	// Append the complete message to the conversation
	internalCtx.Messages = append(internalCtx.Messages, message)

	// Execute pipeline in background
	go p.executeStreamBackground(internalCtx, cancel)

	return internalCtx.StreamOutput, nil
}

// executeChain executes the middleware chain using the Process(ctx, next) pattern.
// Each middleware explicitly calls next() to continue the chain.
func (p *Pipeline) executeChain(execCtx *ExecutionContext, index int) error {
	if index >= len(p.middleware) {
		return nil // End of chain
	}

	// Track if next() was called
	nextCalled := false
	nextCalledMultipleTimes := false

	// Create monitored next function for this middleware
	next := func() error {
		if nextCalled {
			nextCalledMultipleTimes = true
		}
		nextCalled = true
		return p.executeChain(execCtx, index+1)
	}

	// Call Process() for this middleware
	err := p.middleware[index].Process(execCtx, next)

	// Check for common middleware mistakes and log warnings
	middlewareName := fmt.Sprintf("%T", p.middleware[index])

	if !nextCalled && err == nil && index < len(p.middleware)-1 {
		// Middleware didn't call next() and didn't return an error
		// Only warn if ShortCircuit flag is not set (intentional short-circuits are fine)
		if !execCtx.ShortCircuit {
			logger.Warn(errMiddlewareChainBroken,
				"middleware", middlewareName,
				"position", index)
		}
	}

	if nextCalledMultipleTimes {
		// Middleware called next() multiple times
		logger.Warn(errMiddlewareMultipleNext,
			"middleware", middlewareName,
			"position", index)
	}

	// Capture first error
	if err != nil && execCtx.Error == nil {
		execCtx.Error = err
	}

	return err
}

// ValidationError represents a validation failure.
type ValidationError struct {
	Type     string
	Details  string
	Failures []types.ValidationResult // All failed validations (for aggregation)
}

// Error returns the error message for this validation error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf(errValidationFailed, e.Type, e.Details)
}

func strPtr(s string) *string {
	return &s
}
