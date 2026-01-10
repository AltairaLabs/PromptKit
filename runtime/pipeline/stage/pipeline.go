package stage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// StreamPipeline represents an executable pipeline of stages.
// It manages the DAG of stages, creates channels between them,
// and orchestrates execution.
type StreamPipeline struct {
	stages       []Stage
	edges        map[string][]string // stage name -> downstream stage names
	config       *PipelineConfig
	eventEmitter *events.Emitter

	// Concurrency control
	wg         sync.WaitGroup
	shutdown   chan struct{}
	shutdownMu sync.RWMutex
	isShutdown bool
}

// Execute starts the pipeline execution with the given input channel.
// Returns an output channel that will receive all elements from terminal stages.
// The pipeline executes in background goroutines and closes the output channel when complete.
func (p *StreamPipeline) Execute(ctx context.Context, input <-chan StreamElement) (<-chan StreamElement, error) {
	// Check if shutting down
	if p.isShuttingDown() {
		return nil, ErrPipelineShuttingDown
	}

	// Apply execution timeout if configured
	execCtx := ctx
	var cancel context.CancelFunc
	if p.config.ExecutionTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, p.config.ExecutionTimeout)
		logger.Info("Pipeline ExecutionTimeout configured",
			"timeout", p.config.ExecutionTimeout,
			"stages", len(p.stages))
	}

	// Track execution for graceful shutdown
	p.wg.Add(1)

	// Create output channel
	output := make(chan StreamElement, p.config.ChannelBufferSize)

	// Execute pipeline in background
	go p.executeBackground(execCtx, input, output, cancel)

	return output, nil
}

// executeBackground runs the pipeline execution in a background goroutine.
// It starts all stages as goroutines and collects output CONCURRENTLY with stage execution
// to support streaming/duplex stages that run indefinitely.
//
//nolint:lll // Channel signature cannot be shortened
func (p *StreamPipeline) executeBackground(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement, cancel context.CancelFunc) {
	defer func() {
		p.wg.Done()
		if cancel != nil {
			cancel()
		}
	}()

	start := time.Now()
	if p.eventEmitter != nil {
		p.eventEmitter.PipelineStarted(len(p.stages))
	}

	p.monitorExecutionTimeout(ctx, start)

	// Create channels between stages
	channels := p.createChannels()

	// Start stages and collect output
	stageErrors := p.startStages(ctx, input, channels)
	outputDone := p.startOutputCollection(channels, output)

	// Wait for errors and output collection
	firstError := p.waitForStageErrors(stageErrors)
	<-outputDone

	// Emit completion event
	p.emitCompletionEvent(firstError, time.Since(start))
}

// monitorExecutionTimeout logs when execution timeout triggers.
func (p *StreamPipeline) monitorExecutionTimeout(ctx context.Context, start time.Time) {
	if p.config.ExecutionTimeout <= 0 {
		return
	}

	go func() {
		<-ctx.Done()
		if ctx.Err() == context.DeadlineExceeded {
			elapsed := time.Since(start)
			logger.Error("PIPELINE EXECUTION TIMEOUT TRIGGERED",
				"configured_timeout", p.config.ExecutionTimeout,
				"elapsed", elapsed,
				"stages", len(p.stages),
				"hint", "Consider increasing ExecutionTimeout or using WithExecutionTimeout(0) for long-running pipelines")
		}
	}()
}

// startStages starts all pipeline stages as goroutines and returns the error channel.
//
//nolint:lll // Channel signature cannot be shortened
func (p *StreamPipeline) startStages(ctx context.Context, input <-chan StreamElement, channels map[string]chan StreamElement) <-chan error {
	stageWg := sync.WaitGroup{}
	stageErrors := make(chan error, len(p.stages))

	for _, stage := range p.stages {
		stageInput := p.getStageInput(stage, input, channels)
		stageOutput := p.getStageOutput(stage, channels)

		stageWg.Add(1)
		go p.runStage(ctx, stage, stageInput, stageOutput, &stageWg, stageErrors)
	}

	// Close error channel when all stages complete
	go func() {
		stageWg.Wait()
		close(stageErrors)
	}()

	return stageErrors
}

// startOutputCollection starts collecting output from leaf stages.
func (p *StreamPipeline) startOutputCollection(
	channels map[string]chan StreamElement,
	output chan<- StreamElement,
) <-chan struct{} {
	outputDone := make(chan struct{})
	go func() {
		p.collectOutput(channels, output)
		close(output)
		close(outputDone)
	}()
	return outputDone
}

// waitForStageErrors collects errors from stages and returns the first error.
func (p *StreamPipeline) waitForStageErrors(stageErrors <-chan error) error {
	var firstError error
	for err := range stageErrors {
		if err != nil && firstError == nil {
			firstError = err
		}
	}
	return firstError
}

// emitCompletionEvent emits the appropriate pipeline completion event.
func (p *StreamPipeline) emitCompletionEvent(err error, duration time.Duration) {
	if p.eventEmitter == nil {
		return
	}

	if err != nil {
		p.eventEmitter.PipelineFailed(err, duration)
	} else {
		p.eventEmitter.PipelineCompleted(duration, 0, 0, 0, 0)
	}
}

// createChannels creates all inter-stage channels based on the DAG topology.
func (p *StreamPipeline) createChannels() map[string]chan StreamElement {
	channels := make(map[string]chan StreamElement)

	// Create a channel for each stage's output
	for _, stage := range p.stages {
		channels[stage.Name()] = make(chan StreamElement, p.config.ChannelBufferSize)
	}

	return channels
}

// getStageInput returns the input channel for a stage.
// For root stages, it's the pipeline input. For others, it's determined by the DAG.
//
//nolint:lll // Channel signature cannot be shortened
func (p *StreamPipeline) getStageInput(stage Stage, pipelineInput <-chan StreamElement, channels map[string]chan StreamElement) <-chan StreamElement {
	if p.isRootStage(stage.Name()) {
		return pipelineInput
	}

	// For non-root stages, find the upstream stage
	if upstream := p.findUpstreamStage(stage.Name()); upstream != "" {
		return channels[upstream]
	}

	// Should never reach here if validation worked
	return nil
}

// isRootStage checks if a stage has no incoming edges.
func (p *StreamPipeline) isRootStage(stageName string) bool {
	for _, toStages := range p.edges {
		for _, toStage := range toStages {
			if toStage == stageName {
				return false
			}
		}
	}
	return true
}

// findUpstreamStage finds the stage that feeds into the given stage.
// Note: Fan-in (multiple stages feeding into one) is a Phase 5 enhancement.
// Current implementation supports single upstream stage per stage (sufficient for linear chains and fan-out).
// For fan-in/merge patterns, a dedicated MergeStage will be implemented in Phase 5.
func (p *StreamPipeline) findUpstreamStage(stageName string) string {
	for fromStage, toStages := range p.edges {
		for _, toStage := range toStages {
			if toStage == stageName {
				return fromStage
			}
		}
	}
	return ""
}

// getStageOutput returns the output channel for a stage.
func (p *StreamPipeline) getStageOutput(stage Stage, channels map[string]chan StreamElement) chan<- StreamElement {
	return channels[stage.Name()]
}

// runStage executes a single stage, wrapping it with events and error handling.
func (p *StreamPipeline) runStage(
	ctx context.Context,
	stage Stage,
	input <-chan StreamElement,
	output chan<- StreamElement,
	wg *sync.WaitGroup,
	errors chan<- error,
) {
	defer wg.Done()
	// Note: We don't close output here because the stage's Process() method
	// is responsible for closing it according to the Stage interface contract.

	// Emit stage started event
	start := time.Now()
	if p.eventEmitter != nil {
		p.eventEmitter.StageStarted(stage.Name(), 0, stage.Type())
	}

	// Run the stage
	err := stage.Process(ctx, input, output)
	duration := time.Since(start)

	// Emit stage completed/failed event
	if p.eventEmitter != nil {
		if err != nil {
			p.eventEmitter.StageFailed(stage.Name(), 0, err, duration)
		} else {
			p.eventEmitter.StageCompleted(stage.Name(), 0, duration)
		}
	}

	// Report error
	if err != nil {
		errors <- NewStageError(stage.Name(), stage.Type(), err)
	}
}

// collectOutput collects output from all leaf stages into the pipeline output channel.
func (p *StreamPipeline) collectOutput(channels map[string]chan StreamElement, output chan<- StreamElement) {
	// Find leaf stages (stages with no outgoing edges)
	for _, stage := range p.stages {
		if len(p.edges[stage.Name()]) == 0 {
			// This is a leaf stage - collect its output
			stageChan := channels[stage.Name()]
			for elem := range stageChan {
				output <- elem
			}
		}
	}
}

// ExecuteSync runs the pipeline synchronously and returns the accumulated result.
// This is a convenience method for request/response mode where you want a single result.
// It converts the streaming execution into a blocking call.
func (p *StreamPipeline) ExecuteSync(ctx context.Context, input ...StreamElement) (*ExecutionResult, error) {
	// Create input channel
	inputChan := make(chan StreamElement, len(input))
	//nolint:gocritic // rangeValCopy acceptable - elements intentionally copied to channel
	for _, elem := range input {
		inputChan <- elem
	}
	close(inputChan)

	// Execute as stream
	output, err := p.Execute(ctx, inputChan)
	if err != nil {
		return nil, err
	}

	// Accumulate all output elements
	return p.accumulateResult(output)
}

// ExecutionResult represents the final result of a pipeline execution.
// This matches the existing pipeline.ExecutionResult for compatibility.
type ExecutionResult struct {
	Messages []types.Message        // All messages in the conversation
	Response *Response              // The final response
	Trace    ExecutionTrace         // Execution trace
	CostInfo types.CostInfo         // Cost information
	Metadata map[string]interface{} // Additional metadata
}

// Response represents a response message (for compatibility with existing pipeline).
type Response struct {
	Role          string
	Content       string
	Parts         []types.ContentPart
	ToolCalls     []types.MessageToolCall
	FinalResponse string
}

// ExecutionTrace captures execution history (for compatibility).
type ExecutionTrace struct {
	StartedAt   time.Time
	CompletedAt *time.Time
	Duration    time.Duration
}

// accumulateResult collects all elements from output and builds an ExecutionResult.
func (p *StreamPipeline) accumulateResult(output <-chan StreamElement) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
		Trace: ExecutionTrace{
			StartedAt: time.Now(),
		},
	}

	var lastError error

	for elem := range output {
		// Collect messages
		if elem.Message != nil {
			result.Messages = append(result.Messages, *elem.Message)
			// Set response to last assistant message
			if elem.Message.Role == roleAssistant {
				result.Response = &Response{
					Role:    elem.Message.Role,
					Content: elem.Message.Content,
					Parts:   elem.Message.Parts,
				}
			}
		}

		// Collect text into response content
		if elem.Text != nil && result.Response != nil {
			result.Response.Content += *elem.Text
		}

		// Merge metadata
		for k, v := range elem.Metadata {
			result.Metadata[k] = v
		}

		// Track errors
		if elem.Error != nil {
			lastError = elem.Error
		}
	}

	now := time.Now()
	result.Trace.CompletedAt = &now
	result.Trace.Duration = now.Sub(result.Trace.StartedAt)

	return result, lastError
}

// Shutdown gracefully shuts down the pipeline, waiting for in-flight executions to complete.
func (p *StreamPipeline) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%w: %v", ErrShutdownTimeout, p.config.GracefulShutdownTimeout)
	}
}

// isShuttingDown checks if the pipeline is shutting down.
func (p *StreamPipeline) isShuttingDown() bool {
	p.shutdownMu.RLock()
	defer p.shutdownMu.RUnlock()
	return p.isShutdown
}
