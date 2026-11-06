package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitExecutionsDone reports whether every execution of p returned within d.
func waitExecutionsDone(p *StreamPipeline, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// TestShutdown_ReleasesExecutionWhoseReaderIsGone: a consumer that stops
// reading and never cancels holds the execution only until Shutdown's grace
// period ends, both while output is being forwarded and while the turn's
// error waits to be delivered.
func TestShutdown_ReleasesExecutionWhoseReaderIsGone(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ChannelBufferSize = 0
	config.GracefulShutdownTimeout = 10 * time.Millisecond
	p, err := NewPipelineBuilderWithConfig(config).
		Chain(emitAndReturnStage("fails", "partial", errStageBoom)).Build()
	require.NoError(t, err)

	in := make(chan StreamElement)
	close(in)
	_, err = p.Execute(context.Background(), in) // nobody ever reads the output
	require.NoError(t, err)

	require.ErrorIs(t, p.Shutdown(context.Background()), ErrShutdownTimeout)
	assert.True(t, waitExecutionsDone(p, 2*time.Second), "the abandoned execution must return once the grace period ends")
}

// TestShutdown_LiveReaderKeepsOutputAfterGracePeriod: Shutdown's grace period
// ends while a turn is still running (an LLM call longer than the grace), but
// the caller is still reading. Every element the turn then produces, and its
// error, still reach that reader: only a reader that has stopped reading loses
// output. A zero grace period abandons the execution at once, and the same
// holds.
func TestShutdown_LiveReaderKeepsOutputAfterGracePeriod(t *testing.T) {
	const n = 64
	type result struct {
		messages int
		errs     []error
	}
	for _, grace := range []time.Duration{0, 10 * time.Millisecond} {
		release := make(chan struct{})
		slow := NewStageFunc("slow", StageTypeTransform,
			func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				for range in { //nolint:revive // drain
				}
				<-release
				for i := 0; i < n; i++ {
					out <- userElement("reply")
				}
				return errStageBoom
			})
		config := DefaultPipelineConfig()
		config.GracefulShutdownTimeout = grace
		p, err := NewPipelineBuilderWithConfig(config).Chain(slow).Build()
		require.NoError(t, err)

		in := make(chan StreamElement)
		close(in)
		out, err := p.Execute(context.Background(), in)
		require.NoError(t, err)

		got := make(chan result, 1)
		go func() { // the caller reads to the end, as Execute requires
			var r result
			for e := range out {
				if e.Message != nil {
					r.messages++
				}
				if e.Error != nil {
					r.errs = append(r.errs, e.Error)
				}
			}
			got <- r
		}()

		require.ErrorIs(t, p.Shutdown(context.Background()), ErrShutdownTimeout)
		close(release)

		r := <-got
		assert.Equal(t, n, r.messages, "grace %v: a live reader must get every element after the grace period", grace)
		require.Len(t, r.errs, 1, "grace %v: a live reader must get the turn's error", grace)
		assert.ErrorIs(t, r.errs[0], errStageBoom)
	}
}

// TestShutdown_GoneReaderIsWaitedOnOnce: once one send to a reader that has
// stopped reading times out, the rest of the output is dropped without each
// element waiting out the grace period again.
func TestShutdown_GoneReaderIsWaitedOnOnce(t *testing.T) {
	const n = 64
	many := NewStageFunc("many", StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for range in { //nolint:revive // drain
			}
			for i := 0; i < n; i++ {
				out <- userElement("reply")
			}
			return errStageBoom
		})
	config := DefaultPipelineConfig()
	config.ChannelBufferSize = 0
	config.GracefulShutdownTimeout = 20 * time.Millisecond
	p, err := NewPipelineBuilderWithConfig(config).Chain(many).Build()
	require.NoError(t, err)

	in := make(chan StreamElement)
	close(in)
	_, err = p.Execute(context.Background(), in) // nobody ever reads the output
	require.NoError(t, err)

	require.ErrorIs(t, p.Shutdown(context.Background()), ErrShutdownTimeout)
	// Waiting out the grace period once per element would take n*20ms.
	assert.True(t, waitExecutionsDone(p, 500*time.Millisecond),
		"after one send to a gone reader times out, the rest must be dropped without waiting")
}

// TestShutdown_SlowReaderStillGetsTheError: a reader that is slow, but reads
// within the grace period, still gets every element and the turn's error.
func TestShutdown_SlowReaderStillGetsTheError(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ChannelBufferSize = 0
	config.GracefulShutdownTimeout = 2 * time.Second
	p, err := NewPipelineBuilderWithConfig(config).
		Chain(failingStage("fails", errStageBoom, false)).Build()
	require.NoError(t, err)

	in := make(chan StreamElement)
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)

	shutdownErr := make(chan error, 1)
	go func() { shutdownErr <- p.Shutdown(context.Background()) }()
	time.Sleep(20 * time.Millisecond) // shutting down, not yet read
	errs := errorElements(out)

	require.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], errStageBoom)
	require.NoError(t, <-shutdownErr)
}
