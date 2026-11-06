package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// fakeStallTimers stands in for the stall timers of an abandoned execution's
// sends. Each timer started shows up on started; it fires when the test sends
// on it.
type fakeStallTimers struct{ started chan chan time.Time }

func newFakeStallTimers() *fakeStallTimers {
	return &fakeStallTimers{started: make(chan chan time.Time, 8)}
}

func (f *fakeStallTimers) start(time.Duration) (<-chan time.Time, func() bool) {
	c := make(chan time.Time, 1)
	f.started <- c
	return c, func() bool { return true }
}

// fire expires the next stall timer started.
func (f *fakeStallTimers) fire(t *testing.T) {
	t.Helper()
	select {
	case c := <-f.started:
		c <- time.Now()
	case <-time.After(2 * time.Second):
		t.Fatal("no stall timer was started")
	}
}

// dropFirst expires the stall of the execution's first send, while nothing
// reads, and waits until the reader counts as gone. The timer starts just
// before its send selects on it, so a reader arriving in between could still
// take the element; waiting on gone keeps the reader out until the drop.
func (f *fakeStallTimers) dropFirst(t *testing.T, out *callerOutput) {
	t.Helper()
	f.fire(t)
	require.Eventually(t, out.gone.Load, 2*time.Second, time.Millisecond, "the first send must give up")
}

// abandonedAfterRelease builds a pipeline whose one stage waits on release,
// then emits n messages and returns stageErr. It starts an execution and shuts
// the pipeline down while the stage waits, so the execution is abandoned. It
// returns once release is closed. emitter, when non-nil, observes the run.
func abandonedAfterRelease(
	t *testing.T, n int, stageErr error, emitter *events.Emitter,
) (p *StreamPipeline, output <-chan StreamElement, out *callerOutput, timers *fakeStallTimers) {
	t.Helper()
	release := make(chan struct{})
	slow := NewStageFunc("slow", StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, o chan<- StreamElement) error {
			defer close(o)
			for range in { //nolint:revive // drain
			}
			<-release
			for i := 0; i < n; i++ {
				o <- userElement("reply")
			}
			return stageErr
		})
	config := DefaultPipelineConfig()
	config.ChannelBufferSize = 0
	config.GracefulShutdownTimeout = 0
	p, err := NewPipelineBuilderWithConfig(config).Chain(slow).Build()
	require.NoError(t, err)
	timers = newFakeStallTimers()
	p.stallTimerFunc = timers.start
	p.eventEmitter = emitter

	in := make(chan StreamElement)
	close(in)
	output, out, err = p.execute(context.Background(), in)
	require.NoError(t, err)
	require.ErrorIs(t, p.Shutdown(context.Background()), ErrShutdownTimeout)
	close(release)
	return p, output, out, timers
}

// TestShutdown_TruncatedStreamEndsInError: after Shutdown abandons an
// execution, a reader slower than the stall loses output. The stream it reads
// must still end in an error, never close as if the turn completed: one
// wrapping ErrOutputTruncated, and the turn's own error when it failed.
func TestShutdown_TruncatedStreamEndsInError(t *testing.T) {
	for _, stageErr := range []error{nil, errStageBoom} {
		p, output, out, timers := abandonedAfterRelease(t, 3, stageErr, nil)
		timers.dropFirst(t, out) // the reader takes nothing within the stall: it is gone

		// The reader comes back and reads what is left.
		res, err := p.accumulateResult(output)
		assert.Less(t, len(res.Messages), 3, "stage error %v: output was dropped", stageErr)
		require.ErrorIs(t, err, ErrOutputTruncated,
			"stage error %v: a truncated stream must end in an error", stageErr)
		assert.NotErrorIs(t, err, ErrPipelineShuttingDown, "the turn ran; it was not refused")
		if stageErr != nil {
			assert.ErrorIs(t, err, stageErr, "the turn's own error must still reach the caller")
		}
	}
}

// TestShutdown_TruncatedExecuteSyncReturnsError: a reader that never takes the
// last error either does not hold the execution, and ExecuteSync still returns
// the error that ended the stream. Observers see the run fail.
func TestShutdown_TruncatedExecuteSyncReturnsError(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	failed := make(chan error, 1)
	bus.Subscribe(events.EventPipelineFailed, func(e *events.Event) {
		if d, ok := e.Data.(*events.PipelineFailedData); ok {
			select {
			case failed <- d.Error:
			default:
			}
		}
	})

	p, output, out, timers := abandonedAfterRelease(t, 3, nil, events.NewEmitter(bus, "run", "sess", "conv"))
	timers.fire(t) // the first dropped element
	timers.fire(t) // the last attempt to deliver the stream's error
	require.True(t, waitExecutionsDone(p, 2*time.Second), "a hung reader must not hold the execution")

	_, err := p.accumulateSync(output, out)
	require.ErrorIs(t, err, ErrOutputTruncated,
		"ExecuteSync must report a truncated turn even when the reader missed the error element")

	select {
	case got := <-failed:
		assert.ErrorIs(t, got, ErrOutputTruncated, "the pipeline.failed event must carry the truncation")
	case <-time.After(2 * time.Second):
		t.Fatal("a truncated run must emit pipeline.failed, not pipeline.completed")
	}
}
