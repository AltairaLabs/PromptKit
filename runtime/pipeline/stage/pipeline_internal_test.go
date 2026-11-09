package stage

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsRootStage tests the precomputed root stage O(1) lookup.
func TestIsRootStage(t *testing.T) {
	// Build a pipeline: A -> B -> C (A is root, B and C are not)
	stageA := &testPassthroughStage{name: "stageA"}
	stageB := &testPassthroughStage{name: "stageB"}
	stageC := &testPassthroughStage{name: "stageC"}

	pipeline, err := NewPipelineBuilder().
		Chain(stageA, stageB, stageC).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !pipeline.isRootStage("stageA") {
		t.Error("stageA should be a root stage")
	}
	if pipeline.isRootStage("stageB") {
		t.Error("stageB should not be a root stage")
	}
	if pipeline.isRootStage("stageC") {
		t.Error("stageC should not be a root stage")
	}
	if pipeline.isRootStage("nonexistent") {
		t.Error("nonexistent stage should not be a root stage")
	}
}

// testPassthroughStage is a minimal stage that passes elements through.
type testPassthroughStage struct {
	name string
}

func (s *testPassthroughStage) Name() string    { return s.name }
func (s *testPassthroughStage) Type() StageType { return StageTypeTransform }
func (s *testPassthroughStage) Process(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
	defer close(out)
	for elem := range in {
		out <- elem
	}
	return nil
}

// syncLogBuffer is a goroutine-safe log sink: the timeout monitor logs from
// its own goroutine while the test reads.
type syncLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLogSync(t *testing.T) *syncLogBuffer {
	t.Helper()
	b := &syncLogBuffer{}
	logger.SetOutput(b)
	t.Cleanup(func() { logger.SetOutput(nil) })
	return b
}

const executionTimeoutLog = "Pipeline execution timeout triggered"

// When the pipeline's own ExecutionTimeout cuts a turn off, the operator gets
// an error log naming the configured timeout. A caller's own cancellation, or
// a pipeline with no timeout configured, logs nothing.
func TestExecuteSync_ExecutionTimeoutIsLogged(t *testing.T) {
	t.Run("timeout fires", func(t *testing.T) {
		logs := captureLogSync(t)
		config := DefaultPipelineConfig()
		config.ExecutionTimeout = 5 * time.Millisecond
		config.IdleTimeout = 0
		p, err := NewPipelineBuilderWithConfig(config).Chain(waitForCtxStage("slow")).Build()
		require.NoError(t, err)

		_, execErr := p.ExecuteSync(context.Background(), userElement("hi"))
		require.ErrorIs(t, execErr, context.DeadlineExceeded)

		require.Eventually(t, func() bool {
			return strings.Contains(logs.String(), executionTimeoutLog)
		}, 2*time.Second, time.Millisecond, "timeout log never written; got: %s", logs.String())
		assert.Contains(t, logs.String(), "configured_timeout=5ms")
		assert.Contains(t, logs.String(), "stages=1")
	})

	t.Run("caller cancel is not a timeout", func(t *testing.T) {
		logs := captureLogSync(t)
		config := DefaultPipelineConfig()
		config.ExecutionTimeout = time.Minute
		config.IdleTimeout = 0
		ctx, cancel := context.WithCancel(context.Background())
		canceler := NewStageFunc("canceler", StageTypeTransform,
			func(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				for range in { //nolint:revive // drain
				}
				cancel()
				<-ctx.Done()
				return ctx.Err()
			})
		p, err := NewPipelineBuilderWithConfig(config).Chain(canceler).Build()
		require.NoError(t, err)

		_, _ = p.ExecuteSync(ctx, userElement("hi"))
		// The monitor goroutine wakes on the same Done; give it the chance to
		// (wrongly) log before checking it did not.
		time.Sleep(20 * time.Millisecond)
		assert.NotContains(t, logs.String(), executionTimeoutLog)
	})

	t.Run("caller deadline with no timeout configured", func(t *testing.T) {
		logs := captureLogSync(t)
		config := DefaultPipelineConfig()
		config.ExecutionTimeout = 0
		config.IdleTimeout = 0
		p, err := NewPipelineBuilderWithConfig(config).Chain(waitForCtxStage("slow")).Build()
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		_, _ = p.ExecuteSync(ctx, userElement("hi"))
		time.Sleep(20 * time.Millisecond)
		assert.NotContains(t, logs.String(), executionTimeoutLog)
	})
}

// TestWaitForStageErrors tests the waitForStageErrors helper function.
func TestWaitForStageErrors(t *testing.T) {
	pipeline := &StreamPipeline{}

	t.Run("empty error channel", func(t *testing.T) {
		errChan := make(chan error)
		close(errChan)

		result := pipeline.waitForStageErrors(errChan)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("single error", func(t *testing.T) {
		errChan := make(chan error, 1)
		testErr := errors.New("test error")
		errChan <- testErr
		close(errChan)

		result := pipeline.waitForStageErrors(errChan)
		if result != testErr {
			t.Errorf("expected %v, got %v", testErr, result)
		}
	})

	t.Run("multiple errors returns first", func(t *testing.T) {
		errChan := make(chan error, 3)
		firstErr := errors.New("first error")
		secondErr := errors.New("second error")
		errChan <- firstErr
		errChan <- secondErr
		errChan <- nil
		close(errChan)

		result := pipeline.waitForStageErrors(errChan)
		if result != firstErr {
			t.Errorf("expected %v, got %v", firstErr, result)
		}
	})

	t.Run("nil errors followed by real error", func(t *testing.T) {
		errChan := make(chan error, 3)
		testErr := errors.New("test error")
		errChan <- nil
		errChan <- nil
		errChan <- testErr
		close(errChan)

		result := pipeline.waitForStageErrors(errChan)
		if result != testErr {
			t.Errorf("expected %v, got %v", testErr, result)
		}
	})
}

// TestFindUpstreamStages_ReverseEdges verifies O(1) lookup via precomputed reverseEdges.
func TestFindUpstreamStages_ReverseEdges(t *testing.T) {
	stageA := &testPassthroughStage{name: "stageA"}
	stageB := &testPassthroughStage{name: "stageB"}
	stageC := &testPassthroughStage{name: "stageC"}

	pipeline, err := NewPipelineBuilder().
		Chain(stageA, stageB, stageC).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// reverseEdges should be populated by Build.
	if pipeline.reverseEdges == nil {
		t.Fatal("reverseEdges should not be nil")
	}

	if upstream := pipeline.findUpstreamStages("stageB"); len(upstream) != 1 || upstream[0] != "stageA" {
		t.Errorf("expected [stageA] upstream of stageB, got %v", upstream)
	}
	if upstream := pipeline.findUpstreamStages("stageC"); len(upstream) != 1 || upstream[0] != "stageB" {
		t.Errorf("expected [stageB] upstream of stageC, got %v", upstream)
	}
	if upstream := pipeline.findUpstreamStages("stageA"); len(upstream) != 0 {
		t.Errorf("expected empty upstream for root stageA, got %v", upstream)
	}
	if upstream := pipeline.findUpstreamStages("nonexistent"); len(upstream) != 0 {
		t.Errorf("expected empty upstream for nonexistent, got %v", upstream)
	}
}

// TestFindUpstreamStages_FallbackNilReverseEdges tests the fallback linear scan
// when reverseEdges is nil (should not happen in practice).
func TestFindUpstreamStages_FallbackNilReverseEdges(t *testing.T) {
	pipeline := &StreamPipeline{
		edges: map[string][]string{
			"A": {"B"},
			"B": {"C"},
		},
		reverseEdges: nil, // force fallback
	}

	if upstream := pipeline.findUpstreamStages("B"); len(upstream) != 1 || upstream[0] != "A" {
		t.Errorf("fallback: expected [A] upstream of B, got %v", upstream)
	}
	if upstream := pipeline.findUpstreamStages("X"); len(upstream) != 0 {
		t.Errorf("fallback: expected empty for X, got %v", upstream)
	}
}

// TestCollectOutput_ConcurrentLeafStages verifies that collectOutput fans
// in from multiple leaf stages concurrently.
func TestCollectOutput_ConcurrentLeafStages(t *testing.T) {
	// Build a fan-out pipeline: A -> B, A -> C (B and C are leaves)
	stageA := &testPassthroughStage{name: "stageA"}
	stageB := &testPassthroughStage{name: "stageB"}
	stageC := &testPassthroughStage{name: "stageC"}

	pipeline, err := NewPipelineBuilder().
		AddStage(stageA).
		AddStage(stageB).
		AddStage(stageC).
		Connect("stageA", "stageB").
		Connect("stageA", "stageC").
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Create channels mimicking the stage output channels.
	channels := map[string]chan StreamElement{
		"stageA": make(chan StreamElement, 10),
		"stageB": make(chan StreamElement, 10),
		"stageC": make(chan StreamElement, 10),
	}

	// stageA is not a leaf (has outgoing edges), close its channel.
	close(channels["stageA"])

	// stageB and stageC are leaves; push test elements.
	text1 := "from-B"
	text2 := "from-C"
	channels["stageB"] <- StreamElement{Text: &text1}
	close(channels["stageB"])
	channels["stageC"] <- StreamElement{Text: &text2}
	close(channels["stageC"])

	output := make(chan StreamElement, 10)
	go func() {
		pipeline.collectOutput(channels, &callerOutput{ch: output}, &completionTotals{}, nil)
		close(output)
	}()

	var collected []string
	for elem := range output {
		if elem.Text != nil {
			collected = append(collected, *elem.Text)
		}
	}

	if len(collected) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(collected))
	}
}

// A turn that fails emits pipeline.failed carrying the stage's error, and no
// pipeline.completed; a turn that succeeds emits pipeline.completed and no
// pipeline.failed. Observers (Arena, Omnia) count runs from these events.
func TestExecuteSync_EmitsExactlyOneTerminalEvent(t *testing.T) {
	stageErr := errors.New("boom")
	tests := []struct {
		name       string
		stage      Stage
		wantFailed bool
	}{
		{name: "success", stage: &testPassthroughStage{name: "ok"}},
		{
			name: "failure",
			stage: NewStageFunc("bad", StageTypeTransform,
				func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
					defer close(out)
					for range in { //nolint:revive // drain
					}
					return stageErr
				}),
			wantFailed: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := events.NewEventBus()
			t.Cleanup(bus.Close)
			var mu sync.Mutex
			var completed []*events.PipelineCompletedData
			var failed []*events.PipelineFailedData
			done := make(chan struct{}, 2)
			bus.Subscribe(events.EventPipelineCompleted, func(e *events.Event) {
				mu.Lock()
				completed = append(completed, e.Data.(*events.PipelineCompletedData))
				mu.Unlock()
				done <- struct{}{}
			})
			bus.Subscribe(events.EventPipelineFailed, func(e *events.Event) {
				mu.Lock()
				failed = append(failed, e.Data.(*events.PipelineFailedData))
				mu.Unlock()
				done <- struct{}{}
			})

			p, err := NewPipelineBuilder().
				WithEventEmitter(events.NewEmitter(bus, "run", "sess", "conv")).
				Chain(tt.stage).Build()
			require.NoError(t, err)
			_, execErr := p.ExecuteSync(context.Background(), userElement("hi"))

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("no terminal pipeline event")
			}
			time.Sleep(10 * time.Millisecond) // let a wrongly-emitted second event land
			mu.Lock()
			defer mu.Unlock()
			if tt.wantFailed {
				require.ErrorIs(t, execErr, stageErr)
				require.Len(t, failed, 1)
				assert.ErrorIs(t, failed[0].Error, stageErr)
				assert.Empty(t, completed)
				return
			}
			require.NoError(t, execErr)
			require.Len(t, completed, 1)
			assert.Empty(t, failed)
		})
	}
}

// blockingStage blocks until context is cancelled.
type blockingStage struct {
	name string
}

func (s *blockingStage) Name() string    { return s.name }
func (s *blockingStage) Type() StageType { return StageTypeTransform }
func (s *blockingStage) Process(ctx context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
	defer close(out)
	<-ctx.Done()
	return ctx.Err()
}

// fakeExecDeadline stands in for the ExecutionTimeout's deadline (see
// StreamPipeline.execTimeoutFunc). The deadline ends only when the test calls
// expire, with the cause the pipeline gave it. timeout is the duration the
// pipeline asked for.
//
// Unlike a real deadline, the context it ends reports context.Canceled, not
// context.DeadlineExceeded, from Err. The pipeline classifies a timeout by its
// cause, so the tests that use it do not depend on the difference.
type fakeExecDeadline struct {
	mu      sync.Mutex
	timeout time.Duration
	cancel  context.CancelCauseFunc
	cause   error
}

func (f *fakeExecDeadline) withTimeoutCause(
	parent context.Context, d time.Duration, cause error,
) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	f.mu.Lock()
	f.timeout, f.cancel, f.cause = d, cancel, cause
	f.mu.Unlock()
	return ctx, func() { cancel(nil) }
}

func (f *fakeExecDeadline) appliedTimeout() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.timeout
}

// expire ends the deadline. It reports false when the pipeline never applied
// one.
func (f *fakeExecDeadline) expire() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancel == nil {
		return false
	}
	f.cancel(f.cause)
	return true
}

// outputErrors reads output to its end and returns its error elements. The
// bound only limits how long a failing test waits for an output that never
// closes.
func outputErrors(t *testing.T, output <-chan StreamElement) []error {
	t.Helper()
	var errs []error
	bound := time.After(idleProbeBound)
	for {
		select {
		case e, ok := <-output:
			if !ok {
				return errs
			}
			if e.Error != nil {
				errs = append(errs, e.Error)
			}
		case <-bound:
			t.Fatal("output never closed: the timeout did not end the execution")
			return nil
		}
	}
}

func TestExecute_IdleTimeoutCancelsWhenIdle(t *testing.T) {
	const idle = 100 * time.Millisecond
	config := DefaultPipelineConfig()
	config.IdleTimeout = idle
	config.ExecutionTimeout = 0

	p, err := NewPipelineBuilderWithConfig(config).
		Chain(&blockingStage{name: "block"}).
		Build()
	require.NoError(t, err)
	clock := &fakeIdleTimer{}
	p.idleAfterFunc = clock.afterFunc

	// Input that never sends: the stage blocks until the idle timeout fires.
	input := make(chan StreamElement)
	defer close(input)

	output, err := p.Execute(context.Background(), input)
	require.NoError(t, err)

	clock.advance(idle)
	errs := outputErrors(t, output)
	require.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], ErrIdleTimeout)
}

func TestExecute_IdleTimeoutDisabledWhenZero(t *testing.T) {
	config := DefaultPipelineConfig()
	config.IdleTimeout = 0
	config.ExecutionTimeout = 0

	p, err := NewPipelineBuilderWithConfig(config).
		Chain(&testPassthroughStage{name: "pass"}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	input := make(chan StreamElement, 1)
	text := "hello"
	input <- StreamElement{Text: &text}
	close(input)

	result, err := p.ExecuteSync(context.Background(), StreamElement{Text: &text})
	if err != nil {
		t.Fatalf("ExecuteSync: %v", err)
	}
	_ = result // completes normally — no spurious timeout
}

// TestExecute_BothTimeoutsCoexist: with ExecutionTimeout and IdleTimeout both
// set, whichever ends first ends the execution, and the error names that one.
func TestExecute_BothTimeoutsCoexist(t *testing.T) {
	for _, idleFirst := range []bool{true, false} {
		config := DefaultPipelineConfig()
		config.ExecutionTimeout = 500 * time.Millisecond
		config.IdleTimeout = 100 * time.Millisecond

		p, err := NewPipelineBuilderWithConfig(config).
			Chain(&blockingStage{name: "block"}).
			Build()
		require.NoError(t, err)
		clock, deadline := &fakeIdleTimer{}, &fakeExecDeadline{}
		p.idleAfterFunc = clock.afterFunc
		p.execTimeoutFunc = deadline.withTimeoutCause

		input := make(chan StreamElement)
		defer close(input)
		output, err := p.Execute(context.Background(), input)
		require.NoError(t, err)
		assert.Equal(t, config.ExecutionTimeout, deadline.appliedTimeout(),
			"the execution's deadline is ExecutionTimeout")

		if idleFirst {
			clock.advance(config.IdleTimeout)
		} else {
			require.True(t, deadline.expire(), "the pipeline never applied its ExecutionTimeout")
		}
		errs := outputErrors(t, output)

		require.Len(t, errs, 1, "idle first: %v", idleFirst)
		if idleFirst {
			assert.ErrorIs(t, errs[0], ErrIdleTimeout)
			assert.NotErrorIs(t, errs[0], ErrExecutionTimeout)
		} else {
			assert.ErrorIs(t, errs[0], ErrExecutionTimeout)
			assert.NotErrorIs(t, errs[0], ErrIdleTimeout)
		}
	}
}

// resetDetectorStage checks whether a reset func is present in the context.
type resetDetectorStage struct {
	name  string
	found bool
}

func (s *resetDetectorStage) Name() string    { return s.name }
func (s *resetDetectorStage) Type() StageType { return StageTypeTransform }
func (s *resetDetectorStage) Process(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
	defer close(out)
	// Check if idle reset func is in context
	if c, ok := ctx.Value(idleResetKey{}).(idleControl); ok && c.reset != nil {
		s.found = true
	}
	for elem := range in {
		out <- elem
	}
	return nil
}

func TestExecute_IdleResetFuncInContext(t *testing.T) {
	config := DefaultPipelineConfig()
	config.IdleTimeout = 5 * time.Second // long enough to not fire

	detector := &resetDetectorStage{name: "detect"}
	p, err := NewPipelineBuilderWithConfig(config).
		Chain(detector).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	text := "test"
	_, err = p.ExecuteSync(context.Background(), StreamElement{Text: &text})
	if err != nil {
		t.Fatalf("ExecuteSync: %v", err)
	}

	if !detector.found {
		t.Error("expected idle reset func to be present in context")
	}
}
