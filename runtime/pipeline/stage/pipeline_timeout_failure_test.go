package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitForCtxStage blocks until its context ends and returns the context's
// error: a non-provider stage cut off by the pipeline's own timeout.
func waitForCtxStage(name string) *StageFunc {
	return NewStageFunc(name, StageTypeTransform,
		func(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for range in { //nolint:revive // drain the turn's input first
			}
			<-ctx.Done()
			return ctx.Err()
		})
}

// TestExecuteSync_ExecutionTimeoutFailsTheTurn: a stage cut off by the
// pipeline's own ExecutionTimeout fails the turn. Only the caller's own
// cancellation stays silent. Repeated: the error must arrive every time, not
// when the delivery happens to beat the done execution context.
func TestExecuteSync_ExecutionTimeoutFailsTheTurn(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ExecutionTimeout = 5 * time.Millisecond
	config.IdleTimeout = 0

	for i := 0; i < 50; i++ {
		probe := &endOfInputProbe{}
		p, err := NewPipelineBuilderWithConfig(config).
			Chain(waitForCtxStage("slow"), probe.stage()).Build()
		require.NoError(t, err)

		_, execErr := p.ExecuteSync(context.Background(), userElement("hi"))

		require.ErrorIs(t, execErr, context.DeadlineExceeded, "run %d", i)
		var stageErr *StageError
		require.ErrorAs(t, execErr, &stageErr, "run %d", i)
		assert.Equal(t, "slow", stageErr.StageName)

		_, inputErrs, _ := probe.observed()
		require.Len(t, inputErrs, 1, "run %d: the downstream stage must be told the turn failed", i)
	}
}

// TestExecute_TimeoutFailureWaitsForAReader: the turn's error is delivered
// even when the caller reads only after the pipeline's own timeout has ended
// the execution's context. Unbuffered output: the delivery has to wait for the
// reader, and must not give up because the execution's context is done.
func TestExecute_TimeoutFailureWaitsForAReader(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ChannelBufferSize = 0
	config.ExecutionTimeout = 5 * time.Millisecond
	config.IdleTimeout = 0

	p, err := NewPipelineBuilderWithConfig(config).Chain(waitForCtxStage("slow")).Build()
	require.NoError(t, err)

	in := make(chan StreamElement)
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)

	time.Sleep(30 * time.Millisecond) // well past the timeout, before reading
	errs := errorElements(out)
	require.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], context.DeadlineExceeded)
}

// TestExecuteSync_IdleTimeoutFailsTheTurn: the same for IdleTimeout, whose
// context ends as Canceled. The error names the idle timeout as the cause.
func TestExecuteSync_IdleTimeoutFailsTheTurn(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ExecutionTimeout = 0
	config.IdleTimeout = 5 * time.Millisecond

	for i := 0; i < 50; i++ {
		p, err := NewPipelineBuilderWithConfig(config).Chain(waitForCtxStage("slow")).Build()
		require.NoError(t, err)

		_, execErr := p.ExecuteSync(context.Background(), userElement("hi"))

		require.ErrorIs(t, execErr, ErrIdleTimeout, "run %d", i)
		assert.ErrorIs(t, execErr, context.Canceled, "run %d", i)
	}
}

// TestExecuteSync_IdleTimeoutNamedOnAnEmittedError: a stage that emits its
// context error and returns it (the provider stage does) still gets the turn's
// error to name ErrIdleTimeout, delivered once.
func TestExecuteSync_IdleTimeoutNamedOnAnEmittedError(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ExecutionTimeout = 0
	config.IdleTimeout = 5 * time.Millisecond
	emitter := NewStageFunc("emitter", StageTypeTransform,
		func(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for range in { //nolint:revive // drain
			}
			<-ctx.Done()
			out <- NewErrorElement(ctx.Err())
			return ctx.Err()
		})
	p, err := NewPipelineBuilderWithConfig(config).Chain(emitter).Build()
	require.NoError(t, err)

	in := make(chan StreamElement, 1)
	in <- userElement("hi")
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)

	errs := errorElements(out)
	require.Len(t, errs, 1, "the error must arrive once")
	assert.ErrorIs(t, errs[0], ErrIdleTimeout)
}

// TestExecuteSync_IdleTimeoutFailsStreamingProviderTurn: the idle timer
// cutting off a streaming provider's generation is not a barge-in. The turn
// fails with ErrIdleTimeout instead of being dropped silently.
func TestExecuteSync_IdleTimeoutFailsStreamingProviderTurn(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ExecutionTimeout = 0
	config.IdleTimeout = 5 * time.Millisecond
	prov := &blockingProvider{started: make(chan struct{})}
	p, err := NewPipelineBuilderWithConfig(config).Chain(
		NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState()),
	).Build()
	require.NoError(t, err)

	_, execErr := p.ExecuteSync(context.Background(), userElement("hi"), NewEndOfTurnElement())

	require.ErrorIs(t, execErr, ErrIdleTimeout)
}

// TestExecuteSync_CallerCancelWithTimeoutsConfiguredAddsNoError: with both
// timeouts configured, the caller's own cancellation still adds no error.
func TestExecuteSync_CallerCancelWithTimeoutsConfiguredAddsNoError(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ExecutionTimeout = time.Minute
	config.IdleTimeout = time.Minute

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
	probe := &endOfInputProbe{}
	p, err := NewPipelineBuilderWithConfig(config).Chain(canceler, probe.stage()).Build()
	require.NoError(t, err)

	in := make(chan StreamElement, 1)
	in <- userElement("hi")
	close(in)
	out, err := p.Execute(ctx, in)
	require.NoError(t, err)

	assert.Empty(t, errorElements(out), "a caller cancel must not become a pipeline error")
	sawEOF, inputErrs, _ := probe.observed()
	require.True(t, sawEOF)
	assert.Empty(t, inputErrs, "a caller cancel must not be signaled as a failure")
}
