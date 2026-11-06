package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newTestDistributor builds a distributor over outs, with one done channel
// per reader.
func newTestDistributor(outs ...chan StreamElement) (*distributor, []chan struct{}) {
	d := &distributor{outs: outs, readerDone: make([]<-chan struct{}, len(outs)), live: make([]bool, len(outs))}
	dones := make([]chan struct{}, len(outs))
	for i := range outs {
		dones[i] = make(chan struct{})
		d.readerDone[i] = dones[i]
		d.live[i] = true
	}
	return d, dones
}

// TestDistributor_ReadyReaderCostsNoAllocation: handing an element to a
// reader that is ready for it allocates nothing. Building reflect.Select cases
// for every element cost several allocations each.
func TestDistributor_ReadyReaderCostsNoAllocation(t *testing.T) {
	d, _ := newTestDistributor(make(chan StreamElement, 1024), make(chan StreamElement, 1024))
	elem := NewTextElement("x")
	allocs := testing.AllocsPerRun(200, func() {
		if !d.send(elem) {
			t.Fatal("send reported no reader left")
		}
	})
	require.Zero(t, allocs)
	require.Equal(t, 201, len(d.outs[0])+len(d.outs[1]), "every element reaches a reader")
	require.InDelta(t, len(d.outs[0]), len(d.outs[1]), 1, "ready readers share the elements")
}

// TestDistributor_BusyReadersBlockUntilOneTakes: with no reader ready, send
// waits for one, skipping a reader that returns meanwhile, and reports false
// once every reader has returned.
func TestDistributor_BusyReadersBlockUntilOneTakes(t *testing.T) {
	d, dones := newTestDistributor(make(chan StreamElement), make(chan StreamElement))

	got := make(chan StreamElement, 1)
	go func() {
		// Both readers are busy when send starts. Reader 0 returns without
		// reading, then reader 1 takes the element.
		time.Sleep(5 * time.Millisecond)
		close(dones[0])
		time.Sleep(5 * time.Millisecond)
		got <- <-d.outs[1]
	}()
	require.True(t, d.send(NewTextElement("x")))
	require.Equal(t, "x", *(<-got).Text)
	require.False(t, d.live[0], "the returned reader is no longer live")

	close(dones[1])
	require.False(t, d.send(NewTextElement("y")), "no reader is left")
}

// BenchmarkPipelineBranchFanOut measures a competitive (Branch) fan-out: one
// stage's shared output read by two downstream stages, every element handed
// to whichever reader takes it.
func BenchmarkPipelineBranchFanOut(b *testing.B) {
	p, err := NewPipelineBuilder().
		AddStage(NewPassthroughStage("a")).
		AddStage(NewPassthroughStage("p")).
		AddStage(NewPassthroughStage("q")).
		Branch("a", "p", "q").Build()
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
}
