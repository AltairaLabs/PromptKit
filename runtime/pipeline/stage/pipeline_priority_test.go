package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queuedTurn is three text deltas followed by a barge-in Interrupt, already
// queued (buffered and closed) when the stage starts reading.
func queuedTurn() <-chan StreamElement {
	in := make(chan StreamElement, 4)
	in <- NewTextElement("a")
	in <- NewTextElement("b")
	in <- NewTextElement("c")
	in <- NewInterruptElement()
	close(in)
	return in
}

// describe names an element for order assertions.
func describe(e *StreamElement) string {
	switch {
	case e.Interrupt:
		return "interrupt"
	case e.Text != nil:
		return *e.Text
	default:
		return "?"
	}
}

// runRecorder runs a one-stage pipeline over input and returns the order in
// which the stage read its input.
func runRecorder(t *testing.T, cfg *PipelineConfig, input <-chan StreamElement) []string {
	t.Helper()
	var seen []string
	rec := NewStageFunc("rec", StageTypeSink, func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
		defer close(out)
		for e := range in {
			seen = append(seen, describe(&e))
		}
		return nil
	})
	p, err := NewPipelineBuilderWithConfig(cfg).Chain(rec).Build()
	require.NoError(t, err)
	out, err := p.Execute(context.Background(), input)
	require.NoError(t, err)
	for range out { //nolint:revive // drain
	}
	return seen
}

func TestPipeline_PrioritizeElements_DeliversHigherPriorityFirst(t *testing.T) {
	cfg := DefaultPipelineConfig().WithPrioritizeElements(true)

	seen := runRecorder(t, cfg, queuedTurn())

	// The Interrupt (PriorityCritical) overtakes the queued text; the text
	// (all PriorityNormal) keeps the order it was sent in.
	assert.Equal(t, []string{"interrupt", "a", "b", "c"}, seen)
}

func TestPipeline_DefaultConfig_KeepsSendOrder(t *testing.T) {
	seen := runRecorder(t, DefaultPipelineConfig(), queuedTurn())

	assert.Equal(t, []string{"a", "b", "c", "interrupt"}, seen)
}

func TestPipeline_PrioritizeElements_MultiStageKeepsTextOrder(t *testing.T) {
	cfg := DefaultPipelineConfig().WithPrioritizeElements(true)
	const n = 200
	var got []string
	sink := NewStageFunc("sink", StageTypeSink, func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
		defer close(out)
		for e := range in {
			got = append(got, *e.Text)
		}
		return nil
	})
	p, err := NewPipelineBuilderWithConfig(cfg).
		Chain(NewPassthroughStage("a"), NewPassthroughStage("b"), sink).Build()
	require.NoError(t, err)

	in := make(chan StreamElement)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)
	want := make([]string, n)
	for i := range n {
		want[i] = string(rune('A' + i%26))
		in <- NewTextElement(want[i])
	}
	close(in)
	for range out { //nolint:revive // drain
	}

	assert.Equal(t, want, got)
}

func TestPriorityRelay_StopsWhenConsumerReturns(t *testing.T) {
	in := make(chan StreamElement, 1)
	in <- NewTextElement("a")
	stop := make(chan struct{})
	out := prioritizeInput(in, 4, stop)

	close(stop)

	select {
	case _, ok := <-out:
		if ok {
			// The relay may have offered the element before it saw stop;
			// what matters is that it then closes.
			_, ok = <-out
		}
		assert.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("relay did not close its output after stop")
	}
}

func TestPriorityChannel_ReceiveReturnsOnCancelWhileWaiting(t *testing.T) {
	pc := NewPriorityChannel(1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := pc.Receive(ctx)
		done <- err
	}()
	time.Sleep(10 * time.Millisecond) // let Receive start waiting
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Receive ignored cancellation while waiting")
	}
}

func TestPriorityChannel_SendReturnsOnCancelWhileFull(t *testing.T) {
	pc := NewPriorityChannel(1)
	require.NoError(t, pc.Send(context.Background(), NewTextElement("a")))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pc.Send(ctx, NewTextElement("b")) }()
	time.Sleep(10 * time.Millisecond) // let Send start waiting
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Send ignored cancellation while waiting")
	}
}

func TestPriorityChannel_OutOfRangePriorityIsClamped(t *testing.T) {
	pc := NewPriorityChannel(4)
	ctx := context.Background()
	require.NoError(t, pc.Send(ctx, StreamElement{Priority: Priority(-3), Source: "low"}))
	require.NoError(t, pc.Send(ctx, StreamElement{Priority: Priority(99), Source: "top"}))

	first, ok, err := pc.Receive(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	second, _, err := pc.Receive(ctx)
	require.NoError(t, err)

	assert.Equal(t, "top", first.Source)
	assert.Equal(t, "low", second.Source)
}

func TestPriorityChannel_QueueThatNeverEmptiesStaysBounded(t *testing.T) {
	pc := NewPriorityChannel(4)
	ctx := context.Background()
	require.NoError(t, pc.Send(ctx, StreamElement{Priority: PriorityNormal, Sequence: 0}))
	for i := int64(1); i <= 1000; i++ {
		require.NoError(t, pc.Send(ctx, StreamElement{Priority: PriorityNormal, Sequence: i}))
		elem, ok, err := pc.Receive(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, i-1, elem.Sequence, "same-priority elements must stay FIFO")
	}

	assert.LessOrEqual(t, cap(pc.queues[PriorityNormal]), 8)
}

// BenchmarkPipelineChannels compares a four-stage text pipeline with the
// default FIFO channels and with PrioritizeElements.
func BenchmarkPipelineChannels(b *testing.B) {
	for _, tc := range []struct {
		name       string
		prioritize bool
	}{{"fifo", false}, {"priority", true}} {
		b.Run(tc.name, func(b *testing.B) {
			cfg := DefaultPipelineConfig().WithPrioritizeElements(tc.prioritize)
			p, err := NewPipelineBuilderWithConfig(cfg).Chain(
				NewPassthroughStage("a"), NewPassthroughStage("b"),
				NewPassthroughStage("c"), NewPassthroughStage("d"),
			).Build()
			require.NoError(b, err)
			const perRun = 1000
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				in := make(chan StreamElement)
				out, err := p.Execute(context.Background(), in)
				if err != nil {
					b.Fatal(err)
				}
				go func() {
					for range perRun {
						in <- NewTextElement("x")
					}
					close(in)
				}()
				for range out { //nolint:revive // drain
				}
			}
		})
	}
}
