package stage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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

	// Concurrency control. slots holds one token per running execution when
	// MaxConcurrentPipelines is positive; nil means no limit.
	slots      chan struct{}
	wg         sync.WaitGroup
	shutdown   chan struct{}
	shutdownMu sync.RWMutex
	isShutdown bool
}

// Execute starts the pipeline execution with the given input channel.
// Returns an output channel that will receive all elements from terminal stages.
// The pipeline executes in background goroutines and closes the output channel when complete.
//
// The first stage error ends the turn. Nothing is canceled: the failed stage's
// downstream stages receive the error in-band and pass it on without acting on
// the incomplete turn, and the other stages drain as usual. The error reaches the
// caller as an element with Error set, the last element before the output closes.
// A stage that emits its own error as an error element before returning it (the
// provider stage does) delivers that error as emitted. Any other stage's error
// arrives as a *StageError, which unwraps to the stage's error. A context error
// that comes from the caller's own cancellation is not treated as a stage failure.
// One from the pipeline's own ExecutionTimeout or IdleTimeout is: it fails the
// turn, and an idle timeout's error wraps ErrIdleTimeout.
// The output closes once the terminal stages have returned.
//
// Execute returns ErrPipelineShuttingDown if the pipeline is shutting down.
// When PipelineConfig.MaxConcurrentPipelines executions are already running,
// Execute waits for one to finish. It returns ctx's error if ctx ends first, and
// ErrPipelineShuttingDown if Shutdown is called first.
func (p *StreamPipeline) Execute(ctx context.Context, input <-chan StreamElement) (<-chan StreamElement, error) {
	if err := p.acquireSlot(ctx); err != nil {
		return nil, err
	}
	// Register for graceful shutdown, or refuse if shutting down.
	if !p.beginExecution() {
		p.releaseSlot()
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
		execCtx = contextWithIdleReset(execCtx, resetIdle, p.config.IdleTimeout)
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
	// via inference.FromContext, mirroring Arena's eval-orchestrator wiring.
	if p.config.InferenceRegistry != nil {
		execCtx = inference.WithRegistry(execCtx, p.config.InferenceRegistry)
	}

	// The host's answer to "which provider did you bind to the name this pack
	// used". Guardrails and evals resolve their ancillary providers through it.
	if p.config.ProviderBinding != nil {
		execCtx = evals.WithProviderBinding(execCtx, p.config.ProviderBinding)
	}

	// Create output channel
	output := make(chan StreamElement, p.config.ChannelBufferSize)

	// Execute pipeline in background
	go p.executeBackground(ctx, execCtx, input, output, cancel)

	return output, nil
}

// executeBackground runs the pipeline execution in a background goroutine.
// It starts all stages as goroutines and collects output CONCURRENTLY with stage execution
// to support streaming/duplex stages that run indefinitely.
//
// callerCtx is the context the caller passed to Execute; ctx is the execution's
// own, which adds the pipeline's ExecutionTimeout and IdleTimeout to it. Only
// callerCtx ending is the caller's cancellation: a stage cut off by the
// pipeline's own timeout fails the turn.
func (p *StreamPipeline) executeBackground(
	callerCtx, ctx context.Context, input <-chan StreamElement, output chan<- StreamElement,
	cancel context.CancelFunc,
) {
	defer func() {
		if cancel != nil {
			cancel()
		}
		p.releaseSlot()
		p.wg.Done()
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

	// The first stage error ends the turn. It is recorded here and
	// signaled to the failed stage's downstream stages in-band, as an error
	// element on their input (see gateInput). Nothing is canceled, so the
	// other stages drain and persist exactly as before.
	failure := &turnFailure{caller: callerCtx, exec: ctx}

	// Start stages and collect output. totals accumulates the message/token/cost
	// counts as output elements flow through, so the completion event reports
	// real numbers instead of zeros.
	totals := &completionTotals{}
	stageErrors, runs := p.startStages(ctx, input, channels, edgeChannels, failure)
	outputDone := p.startOutputCollection(channels, output, totals, failure)
	<-outputDone

	// The output is complete once the leaf stages have returned. Their own
	// errors are recorded by then, and so is any upstream failure that ended
	// their input: the input gates hold a stage's input open until its
	// upstream has returned. Hand the turn's failure to the caller
	// as the last element, unless a stage already did. Do not wait for every
	// stage: in a duplex session the input stages run until the session
	// closes its input, which it does only after this output closes.
	for _, stage := range p.stages {
		if len(p.edges[stage.Name()]) == 0 {
			<-runs[stage.Name()].done
		}
	}
	if err := failure.get(); err != nil && !totals.delivered(err) {
		// Bounded by the caller's context, not the execution's: after the
		// pipeline's own timeout the execution's is already done, and the
		// failure must still reach a caller that is reading.
		deliverTurnFailure(callerCtx, output, err)
	}
	close(output)

	firstError := p.waitForStageErrors(stageErrors)
	if err := failure.get(); err != nil {
		firstError = err
	}

	// Emit completion event with the real totals observed during collection.
	p.emitCompletionEvent(firstError, time.Since(start), totals)
}

// deliverTurnFailure sends the turn's error to the caller. A consumer that
// has stopped reading would block it forever, so once ctx is done it gives up
// rather than wait.
func deliverTurnFailure(ctx context.Context, output chan<- StreamElement, err error) {
	elem := NewErrorElement(err)
	select {
	case output <- elem:
		return
	default:
	}
	select {
	case output <- elem:
	case <-ctx.Done():
	}
}

// turnFailure records the error that ends a turn: the first stage error that
// is not just the caller's own cancellation showing through.
type turnFailure struct {
	// caller is the context the caller passed to Execute. exec is the
	// execution's context: caller plus the pipeline's own timeouts.
	caller context.Context
	exec   context.Context

	mu  sync.Mutex
	err error
}

// counts reports whether a stage error fails the turn. A context error while
// the caller's context is done is that cancellation showing through, not a
// stage failure. One from the pipeline's own ExecutionTimeout or IdleTimeout,
// with the caller's context still live, is a failure.
func (f *turnFailure) counts(err error) bool {
	if err == nil {
		return false
	}
	return f.caller.Err() == nil || !isContextError(err)
}

// withTimeoutCause adds the reason the execution's context ended to a stage's
// context error when the error does not already carry it, so a stage cut off
// by the idle timer reports ErrIdleTimeout, not a bare context.Canceled.
func (f *turnFailure) withTimeoutCause(err error) error {
	if f == nil || f.exec == nil || f.caller.Err() != nil || !isContextError(err) {
		return err
	}
	cause := context.Cause(f.exec)
	if cause == nil || errors.Is(err, cause) {
		return err
	}
	return &timeoutCauseError{cause: cause, err: err}
}

// timeoutCauseError is a stage's context error with the reason the pipeline's
// own timeout ended the context. It matches both under errors.Is.
type timeoutCauseError struct {
	cause error
	err   error
}

func (e *timeoutCauseError) Error() string   { return e.cause.Error() + ": " + e.err.Error() }
func (e *timeoutCauseError) Unwrap() []error { return []error{e.cause, e.err} }

// namedCause is elem as the caller gets it: an error element carrying a bare
// context error from the pipeline's own timeout gets that timeout's cause, as
// a returned stage error does (see withTimeoutCause). totals observes the
// element before this, so the delivered-error check still matches the raw
// error the stage also returns.
func (f *turnFailure) namedCause(elem StreamElement) StreamElement {
	if elem.Error != nil {
		elem.Error = f.withTimeoutCause(elem.Error)
	}
	return elem
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// record notes a stage error that counts. The first failure wins.
func (f *turnFailure) record(err error) {
	if !f.counts(err) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err == nil {
		f.err = err
	}
}

func (f *turnFailure) get() error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// stageRun is one stage's state within an execution. err is set, before done
// closes, to the stage's error when that error fails the turn.
type stageRun struct {
	done chan struct{}
	err  error
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
func (p *StreamPipeline) startStages(
	ctx context.Context,
	input <-chan StreamElement,
	channels map[string]chan StreamElement,
	edgeChannels map[string]map[string]chan StreamElement,
	failure *turnFailure,
) (stageErrors <-chan error, runs map[string]*stageRun) {
	stageWg := sync.WaitGroup{}
	errs := make(chan error, len(p.stages))

	runs = make(map[string]*stageRun, len(p.stages))
	for _, stage := range p.stages {
		runs[stage.Name()] = &stageRun{done: make(chan struct{})}
	}

	// A shared channel read by several downstream stages (competitive Branch
	// fan-out) gets one distributor, which hands each element to whichever
	// reader takes it and gives every reader its own gated end of input.
	distributed := make(map[string]map[string]<-chan StreamElement)
	for _, stage := range p.stages {
		if p.isCompetitiveFanOut(stage.Name()) {
			distributed[stage.Name()] = distributeInput(channels[stage.Name()], runs[stage.Name()],
				p.edges[stage.Name()], runs)
		}
	}

	for _, stage := range p.stages {
		stageInputs := p.getStageInputs(stage, input, channels, edgeChannels)
		if !p.isRootStage(stage.Name()) {
			stageInputs = p.gateStageInputs(stageInputs, p.findUpstreamStages(stage.Name()),
				runs, distributed, stage.Name())
		}
		stageOutput := p.getStageOutput(stage, channels)

		stageWg.Add(1)
		go p.runStage(ctx, stage, stageInputs, stageOutput, &stageWg, errs, failure, runs[stage.Name()])
	}

	// Close error channel when all stages complete
	go func() {
		stageWg.Wait()
		close(errs)
	}()

	return errs, runs
}

// gateStageInputs wraps each upstream channel of a stage so that its end is
// seen only after the upstream stage has returned. A stage closes its output
// before it returns its error, so without the gate a downstream stage could
// take a failed upstream's closed output for the end of a complete turn and act
// on it, such as the provider stage calling the model with no system prompt.
// With it, a failed upstream's error reaches the downstream stage as the last
// element of its input, as a turn-failure signal (see isTurnFailureSignal).
//
// ups is in the same order as inputs (both come from findUpstreamStages). A
// gate stops reading once its own stage has returned. An input from a shared
// competitive fan-out channel comes from that channel's distributor instead.
func (p *StreamPipeline) gateStageInputs(
	inputs []<-chan StreamElement, ups []string, runs map[string]*stageRun,
	distributed map[string]map[string]<-chan StreamElement, self string,
) []<-chan StreamElement {
	gated := make([]<-chan StreamElement, len(inputs))
	for i, in := range inputs {
		switch {
		case i >= len(ups) || runs[ups[i]] == nil:
			gated[i] = in
		case distributed[ups[i]][self] != nil:
			gated[i] = distributed[ups[i]][self]
		default:
			gated[i] = gateInput(in, runs[ups[i]], runs[self].done)
		}
	}
	return gated
}

// isCompetitiveFanOut reports whether several downstream stages read the
// stage's shared output channel.
func (p *StreamPipeline) isCompetitiveFanOut(stageName string) bool {
	if _, ok := p.multiOutputStages[stageName]; ok {
		return false
	}
	return len(p.edges[stageName]) > 1
}

// gateInput forwards in to the returned channel and closes it once in is
// closed and the upstream stage has returned. If the upstream failed, the gate
// first sends its error as a turn-failure signal, unless the upstream only passed
// on a signal it got for that error. It does so even when the upstream put the
// error on its output as an ordinary error element: a downstream stage cannot tell
// that element from a non-fatal report. The caller still gets the error once (see
// collectOutput). consumerDone stops it when the stage reading the
// returned channel has returned.
func gateInput(in <-chan StreamElement, upstream *stageRun, consumerDone <-chan struct{}) <-chan StreamElement {
	out := make(chan StreamElement)
	go func() {
		defer close(out)
		var carried []error
		for elem := range in {
			if isTurnFailureSignal(&elem) {
				carried = append(carried, elem.Error)
			}
			select {
			case out <- elem:
			case <-consumerDone:
				return
			}
		}
		select {
		case <-upstream.done:
		case <-consumerDone:
			return
		}
		if upstream.err == nil || errorCarried(carried, upstream.err) {
			return
		}
		select {
		case out <- newTurnFailureSignal(upstream.err):
		case <-consumerDone:
		}
	}()
	return out
}

// distributeInput is the gate for a shared channel read by several downstream
// stages. Each element goes to whichever still-running reader takes it first,
// as on the shared channel itself, and no element is lost to a reader that has
// returned. Each reader's input closes once in is closed and the upstream has
// returned; if the upstream failed, every reader still reading first gets the
// turn-failure signal.
func distributeInput(
	in <-chan StreamElement, upstream *stageRun, readers []string, runs map[string]*stageRun,
) map[string]<-chan StreamElement {
	d := &distributor{
		outs:       make([]chan StreamElement, len(readers)),
		readerDone: make([]<-chan struct{}, len(readers)),
		live:       make([]bool, len(readers)),
	}
	result := make(map[string]<-chan StreamElement, len(readers))
	for i, name := range readers {
		d.outs[i] = make(chan StreamElement)
		d.readerDone[i] = runs[name].done
		d.live[i] = true
		result[name] = d.outs[i]
	}
	go d.run(in, upstream)
	return result
}

// casesPerReader is the number of select cases per reader: its send and its done.
const casesPerReader = 2

// distributor is the state of one distributeInput goroutine.
type distributor struct {
	outs       []chan StreamElement
	readerDone []<-chan struct{}
	live       []bool

	// cases holds the reflect.Select cases for the live readers, a send case
	// and a done case per reader, and index the reader of each pair. Built on
	// first use and rebuilt only when a reader stops being live, not for every
	// element.
	cases []reflect.SelectCase
	index []int
	// next is the reader the fast path offers the next element to first, so
	// readers that are all ready share the elements.
	next int
}

func (d *distributor) run(in <-chan StreamElement, upstream *stageRun) {
	defer func() {
		for _, o := range d.outs {
			close(o)
		}
	}()
	var carried []error
	for elem := range in {
		if isTurnFailureSignal(&elem) {
			carried = append(carried, elem.Error)
		}
		if !d.send(elem) {
			return
		}
	}
	<-upstream.done
	if upstream.err != nil && !errorCarried(carried, upstream.err) {
		d.signalLiveReaders(newTurnFailureSignal(upstream.err))
	}
}

// signalLiveReaders sends signal to every reader still reading.
func (d *distributor) signalLiveReaders(signal StreamElement) {
	for i := range d.outs {
		if !d.live[i] {
			continue
		}
		select {
		case d.outs[i] <- signal:
		case <-d.readerDone[i]:
		}
	}
}

// send hands elem to a live reader ready for it, marking readers that have
// returned as no longer live. It reports false when none is left. A reader
// already waiting takes elem without reflection; only when every live reader
// is busy does send block in reflect.Select until one takes it.
func (d *distributor) send(elem StreamElement) bool {
	n := len(d.outs)
	for k := range n {
		i := (d.next + k) % n
		if !d.live[i] {
			continue
		}
		select {
		case d.outs[i] <- elem:
			d.next = (i + 1) % n
			return true
		case <-d.readerDone[i]:
			d.retire(i)
		default:
		}
	}
	for {
		if d.cases == nil {
			d.buildCases()
		}
		if len(d.cases) == 0 {
			return false
		}
		v := reflect.ValueOf(elem)
		for j := 0; j < len(d.cases); j += casesPerReader {
			d.cases[j].Send = v
		}
		chosen, _, _ := reflect.Select(d.cases)
		if chosen%casesPerReader == 0 {
			return true
		}
		d.retire(d.index[chosen/casesPerReader])
	}
}

// retire marks reader i as returned, dropping the cached select cases.
func (d *distributor) retire(i int) {
	d.live[i] = false
	d.cases = nil
}

// buildCases builds the select cases for the live readers. An empty, non-nil
// slice means no reader is left.
func (d *distributor) buildCases() {
	d.cases = make([]reflect.SelectCase, 0, casesPerReader*len(d.outs))
	d.index = d.index[:0]
	for i := range d.outs {
		if !d.live[i] {
			continue
		}
		d.cases = append(d.cases,
			reflect.SelectCase{Dir: reflect.SelectSend, Chan: reflect.ValueOf(d.outs[i])},
			reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(d.readerDone[i])})
		d.index = append(d.index, i)
	}
}

// turnFailureSignal is the error of the element that tells a downstream stage
// an upstream stage failed the turn. It reads and unwraps as the failure itself
// (a *StageError); its type is what marks the element as the signal, apart
// from error elements stages emit as non-fatal reports.
type turnFailureSignal struct{ err error }

func (e *turnFailureSignal) Error() string { return e.err.Error() }
func (e *turnFailureSignal) Unwrap() error { return e.err }

func newTurnFailureSignal(err error) StreamElement {
	return NewErrorElement(&turnFailureSignal{err: err})
}

// isTurnFailureSignal reports whether elem tells that an upstream stage failed
// the turn. A stage acting on its input's end (calling a model, say) must not
// act on a turn that failed.
func isTurnFailureSignal(elem *StreamElement) bool {
	var sig *turnFailureSignal
	return elem.Error != nil && errors.As(elem.Error, &sig)
}

// errorCarried reports whether err, a stage's returned error wrapped in a
// StageError, is already among carried: the stage's own error, or a wrap of it.
//
// It matches on the stage's own error, not on whatever err wraps: an earlier
// element carrying a bare sentinel that err also wraps (io.EOF, say, reported
// by another stage) is not this failure. The timeout cause the pipeline adds
// (see withTimeoutCause) is set aside, so a stage that emits ctx.Err() and
// returns it still matches. That context error is a bare sentinel, so there
// only the same error matches, not an earlier one that wraps it (a tool's
// "lookup timed out: context deadline exceeded", say).
func errorCarried(carried []error, err error) bool {
	timedOut, inner := stageOwnError(err)
	for _, e := range carried {
		if errors.Is(e, inner) && (!timedOut || errors.Is(inner, e)) {
			return true
		}
	}
	return false
}

// stageOwnError is the error a failed stage itself returned: err's
// StageError.Err, without the timeout cause the pipeline added to it.
// timedOut reports whether there was such a cause to set aside.
func stageOwnError(err error) (timedOut bool, inner error) {
	inner = err
	var se *StageError
	if errors.As(err, &se) {
		inner = se.Err
	}
	if tc, ok := inner.(*timeoutCauseError); ok {
		return true, tc.err
	}
	return false, inner
}

// startOutputCollection starts collecting output from leaf stages. The returned
// channel closes when every leaf's output has been forwarded; closing output
// is left to the caller, which may still have the turn's error to deliver.
func (p *StreamPipeline) startOutputCollection(
	channels map[string]chan StreamElement,
	output chan<- StreamElement,
	totals *completionTotals,
	failure *turnFailure,
) <-chan struct{} {
	outputDone := make(chan struct{})
	go func() {
		p.collectOutput(channels, output, totals, failure)
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

	// errs are the errors already delivered to the caller on output elements;
	// signals are those of them that were turn-failure signals.
	errs    []error
	signals []error
}

// observe records a single output element's contribution to the totals and
// reports whether the element should reach the caller. It does not when it
// repeats a failure already delivered: a stage that emitted its error and also
// returned it puts both the error element and the turn-failure signal on the
// way to the caller, in either order when several leaves fan in. Nil-safe on
// the receiver so callers (and tests) can pass a nil accumulator to opt out.
func (t *completionTotals) observe(elem *StreamElement) bool {
	if t == nil || (elem.Message == nil && elem.Error == nil) {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if elem.Error != nil {
		if t.repeatsFailure(elem) {
			return false
		}
		t.errs = append(t.errs, elem.Error)
		if isTurnFailureSignal(elem) {
			t.signals = append(t.signals, elem.Error)
		}
	}
	if elem.Message == nil {
		return true
	}
	t.messageCount++
	if elem.Message.Role == roleAssistant && elem.Message.CostInfo != nil {
		t.inputTokens += elem.Message.CostInfo.InputTokens
		t.outputTokens += elem.Message.CostInfo.OutputTokens
		t.totalCost += elem.Message.CostInfo.TotalCost
	}
	return true
}

// repeatsFailure reports whether an error element repeats a failure the caller
// already got: a turn-failure signal whose error was delivered, or an error
// element whose error a delivered signal carries. Called with t.mu held.
func (t *completionTotals) repeatsFailure(elem *StreamElement) bool {
	if isTurnFailureSignal(elem) {
		return errorCarried(t.errs, elem.Error)
	}
	for _, s := range t.signals {
		if errorCarried([]error{elem.Error}, s) {
			return true
		}
	}
	return false
}

// delivered reports whether err already reached the caller on an output
// element — a stage that emits its error and also returns it.
func (t *completionTotals) delivered(err error) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return errorCarried(t.errs, err)
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
	errs chan<- error,
	failure *turnFailure,
	run *stageRun,
) {
	defer wg.Done()
	// Closed after the error below is set on run, so a gated downstream stage
	// sees this stage's output end only once it can tell whether it failed.
	defer close(run.done)
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
			instrumented[i] = p.orderInput(instrumentStageInput(stage.Name(), in), run.done)
		}
		err = mi.ProcessMultiple(ctx, instrumented, output)
	} else {
		var in <-chan StreamElement
		if len(inputs) > 0 {
			in = p.orderInput(instrumentStageInput(stage.Name(), inputs[0]), run.done)
		}
		err = stage.Process(ctx, in, output)
	}
	duration := time.Since(start)
	err = failure.withTimeoutCause(err)

	// Emit stage completed/failed event
	if p.eventEmitter != nil {
		if err != nil {
			p.eventEmitter.StageFailed(stage.Name(), 0, err, duration)
		} else {
			p.eventEmitter.StageCompleted(stage.Name(), 0, duration)
		}
	}

	// Report error. A stage returning with an error is abnormal (in a continuous
	// duplex session a stage returning at all ends the whole pipeline), so surface
	// it at INFO; a clean return stays at Debug to avoid noise on every unary Send.
	if err != nil {
		logger.Info("pipeline stage returned with error",
			"stage", stage.Name(), "type", stage.Type(),
			"duration", duration, "error", err)
		stageErr := NewStageError(stage.Name(), stage.Type(), err)
		failure.record(stageErr)
		if failure.counts(stageErr) {
			run.err = stageErr
		}
		errs <- stageErr
	} else {
		logger.Debug("pipeline stage completed",
			"stage", stage.Name(), "type", stage.Type(),
			"duration", duration)
	}
}

// orderInput returns a stage's input as the stage reads it: in priority order
// through a PriorityChannel when PrioritizeElements is set, otherwise unchanged.
// stageDone is closed when the reading stage has returned.
func (p *StreamPipeline) orderInput(in <-chan StreamElement, stageDone <-chan struct{}) <-chan StreamElement {
	if in == nil || p.config == nil || !p.config.PrioritizeElements {
		return in
	}
	return prioritizeInput(in, p.config.ChannelBufferSize, stageDone)
}

// prioritizeInput relays in to the returned channel through a PriorityChannel
// holding up to capacity elements (at least one). Each element the relay can
// take without waiting is queued before the next one is offered, so the reader
// gets the highest-priority element waiting. The returned channel closes once
// in is closed and the queue is empty, or when stop is closed.
func prioritizeInput(in <-chan StreamElement, capacity int, stop <-chan struct{}) <-chan StreamElement {
	out := make(chan StreamElement)
	go runPriorityRelay(in, out, NewPriorityChannel(max(capacity, 1)), stop)
	return out
}

func runPriorityRelay(in <-chan StreamElement, out chan<- StreamElement, pc *PriorityChannel, stop <-chan struct{}) {
	defer close(out)
	ctx := context.Background()
	for {
		in = queueReady(ctx, in, pc)
		head, queued := pc.peek()
		if in == nil && !queued {
			return
		}
		var recv <-chan StreamElement
		if pc.Len() < pc.capacity {
			recv = in
		}
		var send chan<- StreamElement
		if queued {
			send = out
		}
		select {
		case elem, ok := <-recv:
			if !ok {
				in = nil
				continue
			}
			_ = pc.Send(ctx, elem) // has room: never blocks
		case send <- head:
			_, _, _ = pc.Receive(ctx) // removes head: never blocks
		case <-stop:
			return
		}
	}
}

// queueReady moves every element in has ready into pc, while pc has room. It
// returns nil once in is closed.
func queueReady(ctx context.Context, in <-chan StreamElement, pc *PriorityChannel) <-chan StreamElement {
	for in != nil && pc.Len() < pc.capacity {
		select {
		case elem, ok := <-in:
			if !ok {
				return nil
			}
			_ = pc.Send(ctx, elem) // has room: never blocks
		default:
			return in
		}
	}
	return in
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

// forwardOutput hands one leaf element to the caller. A turn-failure signal
// whose error already reached the caller (a stage that emitted its error and
// also returned it) is not sent again.
func (p *StreamPipeline) forwardOutput(
	elem StreamElement, output chan<- StreamElement, totals *completionTotals, failure *turnFailure,
) {
	if !totals.observe(&elem) {
		return
	}
	output <- failure.namedCause(elem)
}

// collectOutput collects output from all leaf stages into the pipeline output
// channel. Leaf stages are read concurrently so that a slow leaf does not
// block faster ones.
func (p *StreamPipeline) collectOutput(
	channels map[string]chan StreamElement,
	output chan<- StreamElement,
	totals *completionTotals,
	failure *turnFailure,
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
				p.forwardOutput(elem, output, totals, failure)
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
				p.forwardOutput(elem, output, totals, failure)
			}
		}(ch)
	}
	wg.Wait()
}

// ExecuteSync runs the pipeline synchronously and returns the accumulated result.
// This is a convenience method for request/response mode where you want a single result.
// It converts the streaming execution into a blocking call.
//
// The returned error is the last error element on the output. When a stage
// failed the turn, that is the failure (see Execute for its form). The result is
// non-nil alongside the error and holds whatever the pipeline produced.
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

// collectResultMessage appends msg to result.Messages and, for an assistant
// message, makes it the response and aggregates its cost.
func collectResultMessage(result *ExecutionResult, msg *types.Message) {
	result.Messages = append(result.Messages, *msg)
	// Set response to last assistant message
	if msg.Role != roleAssistant {
		return
	}
	result.Response = &Response{
		Role:      msg.Role,
		Content:   msg.Content,
		Parts:     msg.Parts,
		ToolCalls: msg.ToolCalls,
	}
	// Propagate cost info from provider response to result via the
	// single shared aggregator so Breakdown/Quantities are never dropped.
	if msg.CostInfo != nil {
		result.CostInfo = base.AggregateCost(&result.CostInfo, msg.CostInfo)
	}
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
			collectResultMessage(result, elem.Message)
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

// acquireSlot takes an execution slot when MaxConcurrentPipelines is set,
// waiting for one to free up.
func (p *StreamPipeline) acquireSlot(ctx context.Context) error {
	if p.slots == nil {
		return nil
	}
	select {
	case p.slots <- struct{}{}:
		return nil
	default:
	}
	select {
	case p.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.shutdown:
		return ErrPipelineShuttingDown
	}
}

// releaseSlot frees the slot acquireSlot took.
func (p *StreamPipeline) releaseSlot() {
	if p.slots != nil {
		<-p.slots
	}
}

// beginExecution registers an execution with the graceful-shutdown WaitGroup,
// or reports false when the pipeline is shutting down. The check and the Add
// share one read lock: Shutdown sets isShutdown under the write lock before it
// Waits, so an Add can never run concurrently with that Wait, which
// sync.WaitGroup forbids. Checking and adding separately left a window where an
// Execute that passed the check added after Shutdown had started waiting.
func (p *StreamPipeline) beginExecution() bool {
	p.shutdownMu.RLock()
	defer p.shutdownMu.RUnlock()
	if p.isShutdown {
		return false
	}
	p.wg.Add(1)
	return true
}
