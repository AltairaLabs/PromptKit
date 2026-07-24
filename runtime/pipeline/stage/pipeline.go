package stage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// StreamPipeline represents an executable pipeline of stages.
// It manages the DAG of stages, creates channels between them,
// and orchestrates execution.
type StreamPipeline struct {
	stages       []Stage
	edges        map[string][]string // stage name -> downstream stage names
	reverseEdges map[string][]string // stage name -> upstream stage names (fan-in aware)
	rootStages   map[string]struct{} // precomputed set of stages with no incoming edges

	// multiOutputStages is the precomputed set of stages implementing
	// MultiOutputStage (selective fan-out / 1:N routing, e.g. RouterStage),
	// keyed by name. Computed once at Build() time and read-only thereafter —
	// safe to share across concurrent Execute() calls. The per-Execute edge
	// channels themselves (edgeChannels[upstreamName][downstreamName]) are
	// NOT stored on the pipeline; they are built fresh per Execute() in
	// createChannels and threaded as a local through startStages /
	// getStageInputs / upstreamChannel. Storing them on *StreamPipeline would
	// make them a shared field mutated on every Execute(), racing across
	// concurrent Execute() calls on the same pipeline (see the doc comment on
	// MultiOutputStage in stage.go for the resulting single-Execute caveat).
	multiOutputStages map[string]MultiOutputStage

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

	// Apply execution timeout if configured (hard ceiling)
	execCtx := ctx
	var cancel context.CancelFunc
	if p.config.ExecutionTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, p.config.ExecutionTimeout)
		logger.Debug("Pipeline ExecutionTimeout configured",
			"timeout", p.config.ExecutionTimeout,
			"stages", len(p.stages))
	}

	// Apply idle timeout if configured (resets on activity)
	if p.config.IdleTimeout > 0 {
		var idleCancel context.CancelFunc
		var resetIdle func()
		execCtx, idleCancel, resetIdle = withIdleTimeout(execCtx, p.config.IdleTimeout)
		execCtx = contextWithIdleReset(execCtx, resetIdle)
		prevCancel := cancel
		cancel = func() {
			idleCancel()
			if prevCancel != nil {
				prevCancel()
			}
		}
		logger.Debug("Pipeline IdleTimeout configured",
			"timeout", p.config.IdleTimeout,
			"stages", len(p.stages))
	}

	// Attach the classify registry so stages resolve inference backends
	// via classify.FromContext, mirroring Arena's eval-orchestrator wiring.
	if p.config.ClassifyRegistry != nil {
		execCtx = classify.WithRegistry(execCtx, p.config.ClassifyRegistry)
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

	// Create channels between stages. Both channels and edgeChannels are local
	// to this Execute() call — see the doc comment on the multiOutputStages
	// field for why edgeChannels must not be stored on *StreamPipeline.
	channels, edgeChannels := p.createChannels()

	// Start stages and collect output. totals accumulates the message/token/cost
	// counts as output elements flow through, so the completion event reports
	// real numbers instead of zeros.
	totals := &completionTotals{}
	stageErrors := p.startStages(ctx, input, channels, edgeChannels)
	outputDone := p.startOutputCollection(channels, output, totals)

	// Wait for errors and output collection
	firstError := p.waitForStageErrors(stageErrors)
	<-outputDone

	// Emit completion event with the real totals observed during collection.
	p.emitCompletionEvent(firstError, time.Since(start), totals)
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
			logger.Error("Pipeline execution timeout triggered",
				"configured_timeout", p.config.ExecutionTimeout,
				"elapsed", elapsed,
				"stages", len(p.stages),
				"hint", "Consider increasing ExecutionTimeout or using WithExecutionTimeout(0) for long-running pipelines")
		}
	}()
}

// startStages starts all pipeline stages as goroutines and returns the error channel.
// edgeChannels is the per-Execute() map of dedicated MultiOutputStage edge
// channels built by createChannels; it is threaded through as a local
// parameter (like channels) rather than stored on the pipeline.
//
//nolint:lll // Channel signature cannot be shortened
func (p *StreamPipeline) startStages(
	ctx context.Context,
	input <-chan StreamElement,
	channels map[string]chan StreamElement,
	edgeChannels map[string]map[string]chan StreamElement,
) <-chan error {
	stageWg := sync.WaitGroup{}
	stageErrors := make(chan error, len(p.stages))

	for _, stage := range p.stages {
		stageInputs := p.getStageInputs(stage, input, channels, edgeChannels)
		stageOutput := p.getStageOutput(stage, channels)

		stageWg.Add(1)
		go p.runStage(ctx, stage, stageInputs, stageOutput, &stageWg, stageErrors)
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
	totals *completionTotals,
) <-chan struct{} {
	outputDone := make(chan struct{})
	go func() {
		p.collectOutput(channels, output, totals)
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

// completionTotals accumulates the message/token/cost counts observed as output
// elements flow through collectOutput, so the pipeline.completed event carries
// real numbers instead of placeholder zeros. It is written from the (possibly
// concurrent) leaf fan-in goroutines, so access is mutex-guarded; the lock is
// only taken for elements that carry a Message, which are rare relative to
// audio/text stream elements.
type completionTotals struct {
	mu           sync.Mutex
	messageCount int
	inputTokens  int
	outputTokens int
	totalCost    float64
}

// observe records a single output element's contribution to the totals. Nil-safe
// on the receiver so callers (and tests) can pass a nil accumulator to opt out.
func (t *completionTotals) observe(elem *StreamElement) {
	if t == nil || elem.Message == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messageCount++
	if elem.Message.Role == roleAssistant && elem.Message.CostInfo != nil {
		t.inputTokens += elem.Message.CostInfo.InputTokens
		t.outputTokens += elem.Message.CostInfo.OutputTokens
		t.totalCost += elem.Message.CostInfo.TotalCost
	}
}

// snapshot returns the accumulated totals. Nil-safe (returns zeros).
func (t *completionTotals) snapshot() (messages, inputTokens, outputTokens int, cost float64) {
	if t == nil {
		return 0, 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.messageCount, t.inputTokens, t.outputTokens, t.totalCost
}

// emitCompletionEvent emits the appropriate pipeline completion event.
func (p *StreamPipeline) emitCompletionEvent(err error, duration time.Duration, totals *completionTotals) {
	if p.eventEmitter == nil {
		return
	}

	if err != nil {
		p.eventEmitter.PipelineFailed(err, duration)
	} else {
		messages, inputTokens, outputTokens, cost := totals.snapshot()
		p.eventEmitter.PipelineCompleted(duration, cost, inputTokens, outputTokens, messages)
	}
}

// createChannels creates all inter-stage channels based on the DAG topology.
// It also builds and registers per-edge channels for any MultiOutputStage
// (selective fan-out) BEFORE stages start, since RegisterOutput must be
// called before Process runs. Both returned maps are local to this Execute()
// call — see the doc comment on the multiOutputStages field.
func (p *StreamPipeline) createChannels() (
	map[string]chan StreamElement,
	map[string]map[string]chan StreamElement,
) {
	channels := make(map[string]chan StreamElement)

	// Create a channel for each stage's output
	for _, stage := range p.stages {
		channels[stage.Name()] = make(chan StreamElement, p.config.ChannelBufferSize)
	}

	edgeChannels := p.registerMultiOutputEdges()

	return channels, edgeChannels
}

// registerMultiOutputEdges creates one dedicated channel per downstream edge
// for each stage implementing MultiOutputStage (selective fan-out / 1:N
// routing) and registers it with the stage via RegisterOutput. It iterates
// p.multiOutputStages — the single source of truth for "which stages are
// multi-output", computed once at Build() — rather than re-type-asserting
// p.stages on every Execute(). Downstream stages read these dedicated
// channels instead of the upstream's shared channels[upstream] entry (see
// getStageInputs / upstreamChannel) — that shared entry becomes an orphan the
// stage still closes via Process's `defer close(output)`, but nothing reads
// it (the stage is not a leaf, so collectOutput does not collect it either).
//
// RegisterOutput call-site caveat: this registers fresh channels onto the
// MultiOutputStage instance itself (e.g. RouterStage.outputs) on every
// Execute(), not onto anything owned by this per-Execute call. A pipeline
// containing a MultiOutputStage is therefore only safe for a single (or
// strictly sequential, non-concurrent) Execute() — concurrent Execute() calls
// on the same pipeline would race on the stage's own output registry. This
// matches the only real usage today: the long-running duplex streaming
// session, Execute'd once. See the MultiOutputStage doc comment in stage.go.
func (p *StreamPipeline) registerMultiOutputEdges() map[string]map[string]chan StreamElement {
	edgeChannels := make(map[string]map[string]chan StreamElement, len(p.multiOutputStages))
	for name, mo := range p.multiOutputStages {
		downstreams := p.edges[name]
		perEdge := make(map[string]chan StreamElement, len(downstreams))
		for _, d := range downstreams {
			ch := make(chan StreamElement, p.config.ChannelBufferSize)
			perEdge[d] = ch
			mo.RegisterOutput(d, ch)
		}
		edgeChannels[name] = perEdge
	}
	return edgeChannels
}

// getStageInputs returns the input channel(s) for a stage.
// For root stages, it's the pipeline input (single channel). For others, it's
// determined by the DAG — one channel per upstream stage, in reverseEdges order.
//
//nolint:lll // Channel signature cannot be shortened
func (p *StreamPipeline) getStageInputs(
	stage Stage,
	pipelineInput <-chan StreamElement,
	channels map[string]chan StreamElement,
	edgeChannels map[string]map[string]chan StreamElement,
) []<-chan StreamElement {
	if p.isRootStage(stage.Name()) {
		return []<-chan StreamElement{pipelineInput}
	}

	ups := p.findUpstreamStages(stage.Name())
	inputs := make([]<-chan StreamElement, 0, len(ups))
	for _, u := range ups {
		inputs = append(inputs, p.upstreamChannel(u, stage.Name(), channels, edgeChannels))
	}
	return inputs
}

// upstreamChannel resolves the channel a downstream stage should read from for
// a given upstream. If the upstream implements MultiOutputStage (selective
// fan-out), the downstream reads its dedicated per-edge channel from
// edgeChannels (built fresh per Execute() by createChannels and threaded in
// as a local parameter); otherwise it reads the upstream's shared output
// channel, preserving existing linear and competitive-Branch fan-out
// behavior unchanged for every stage that is not a MultiOutputStage.
func (p *StreamPipeline) upstreamChannel(
	upstream, downstream string,
	channels map[string]chan StreamElement,
	edgeChannels map[string]map[string]chan StreamElement,
) <-chan StreamElement {
	if _, ok := p.multiOutputStages[upstream]; ok {
		if ch, ok := edgeChannels[upstream][downstream]; ok {
			return ch
		}
	}
	return channels[upstream]
}

// isRootStage checks if a stage has no incoming edges (O(1) lookup).
func (p *StreamPipeline) isRootStage(stageName string) bool {
	_, ok := p.rootStages[stageName]
	return ok
}

// findUpstreamStages returns all stages that feed into the given stage, using a
// precomputed reverse-edge map for O(1) lookup. A stage with more than one
// upstream is a fan-in node (see MultiInputStage); single-upstream is the
// common linear/fan-out case.
func (p *StreamPipeline) findUpstreamStages(stageName string) []string {
	if p.reverseEdges != nil {
		return p.reverseEdges[stageName]
	}
	// Fallback to linear scan (should not happen with properly built pipelines).
	var ups []string
	for fromStage, toStages := range p.edges {
		for _, toStage := range toStages {
			if toStage == stageName {
				ups = append(ups, fromStage)
			}
		}
	}
	return ups
}

// getStageOutput returns the output channel for a stage.
func (p *StreamPipeline) getStageOutput(stage Stage, channels map[string]chan StreamElement) chan<- StreamElement {
	return channels[stage.Name()]
}

// runStage executes a single stage, wrapping it with events and error handling.
// It dispatches to Stage.Process for the common single-upstream case, and to
// MultiInputStage.ProcessMultiple when the stage has more than one upstream
// channel and implements that interface (fan-in).
func (p *StreamPipeline) runStage(
	ctx context.Context,
	stage Stage,
	inputs []<-chan StreamElement,
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

	// Run the stage. Wrap input(s) with Prometheus metrics so operators can see
	// element and audio-byte flow per stage. The wrapper is a zero-overhead
	// passthrough when DefaultStreamMetrics() returns nil (no host has registered).
	var err error
	if mi, ok := stage.(MultiInputStage); ok && len(inputs) > 1 {
		instrumented := make([]<-chan StreamElement, len(inputs))
		for i, in := range inputs {
			instrumented[i] = instrumentStageInput(stage.Name(), in)
		}
		err = mi.ProcessMultiple(ctx, instrumented, output)
	} else {
		var in <-chan StreamElement
		if len(inputs) > 0 {
			in = inputs[0]
		}
		err = stage.Process(ctx, instrumentStageInput(stage.Name(), in), output)
	}
	duration := time.Since(start)

	// Emit stage completed/failed event
	if p.eventEmitter != nil {
		if err != nil {
			p.eventEmitter.StageFailed(stage.Name(), 0, err, duration)
		} else {
			p.eventEmitter.StageCompleted(stage.Name(), 0, duration)
		}
	}

	// Report error. In a continuous duplex session a stage returning at all ends
	// the whole pipeline (stages are meant to run for the session's lifetime), so
	// log the first stage to return at INFO — it names what tore the session down.
	if err != nil {
		logger.Info("pipeline stage returned with error",
			"stage", stage.Name(), "type", stage.Type(),
			"duration", duration, "error", err)
		errors <- NewStageError(stage.Name(), stage.Type(), err)
	} else {
		logger.Info("pipeline stage returned",
			"stage", stage.Name(), "type", stage.Type(),
			"duration", duration)
	}
}

// instrumentStageInput wraps an input channel with Prometheus counters
// that track element count and audio bytes flowing INTO a stage. Returns
// the original channel unchanged when no metrics are registered.
func instrumentStageInput(stageName string, input <-chan StreamElement) <-chan StreamElement {
	metrics := providers.DefaultStreamMetrics()
	if metrics == nil {
		return input
	}
	instrumented := make(chan StreamElement, cap(input))
	go func() {
		defer close(instrumented)
		for elem := range input {
			metrics.PipelineStageElementInc(stageName)
			if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
				metrics.PipelineStageAudioBytesAdd(stageName, len(elem.Audio.Samples))
			}
			instrumented <- elem
		}
	}()
	return instrumented
}

// collectOutput collects output from all leaf stages into the pipeline output
// channel. Leaf stages are read concurrently so that a slow leaf does not
// block faster ones.
func (p *StreamPipeline) collectOutput(
	channels map[string]chan StreamElement,
	output chan<- StreamElement,
	totals *completionTotals,
) {
	// Identify leaf stages (stages with no outgoing edges).
	var leafChans []<-chan StreamElement
	for _, stage := range p.stages {
		if len(p.edges[stage.Name()]) == 0 {
			leafChans = append(leafChans, channels[stage.Name()])
		}
	}

	if len(leafChans) <= 1 {
		// Fast path: single leaf — no extra goroutines needed.
		for _, ch := range leafChans {
			for elem := range ch {
				totals.observe(&elem)
				output <- elem
			}
		}
		return
	}

	// Fan-in: one goroutine per leaf forwards to the shared output channel.
	var wg sync.WaitGroup
	wg.Add(len(leafChans))
	for _, ch := range leafChans {
		go func(src <-chan StreamElement) {
			defer wg.Done()
			for elem := range src {
				totals.observe(&elem)
				output <- elem
			}
		}(ch)
	}
	wg.Wait()
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
	Messages     []types.Message              // All messages in the conversation
	Response     *Response                    // The final response
	Trace        ExecutionTrace               // Execution trace
	CostInfo     types.CostInfo               // Cost information
	PendingTools []tools.PendingToolExecution // Tool calls suspended awaiting out-of-band completion
	Metadata     map[string]interface{}       // Additional metadata
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
					Role:      elem.Message.Role,
					Content:   elem.Message.Content,
					Parts:     elem.Message.Parts,
					ToolCalls: elem.Message.ToolCalls,
				}
				// Propagate cost info from provider response to result via the
				// single shared aggregator so Breakdown/Quantities are never dropped.
				if elem.Message.CostInfo != nil {
					result.CostInfo = base.AggregateCost(&result.CostInfo, elem.Message.CostInfo)
				}
			}
		}

		// Collect text into response content
		if elem.Text != nil && result.Response != nil {
			result.Response.Content += *elem.Text
		}

		if len(elem.Meta.PendingTools) > 0 {
			result.PendingTools = elem.Meta.PendingTools
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
