package stage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emitSourced is a root stage that emits n text elements tagged with the given
// Source, then closes. Used to feed a RouterStage with a deterministic mix.
func emitSourced(name, source string, n int) *StageFunc {
	return NewStageFunc(name, StageTypeGenerate,
		func(ctx context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for i := 0; i < n; i++ {
				e := StreamElement{Source: source}
				select {
				case out <- e:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
}

// collectStage is a sink stage that records every element it receives, guarded
// by a mutex since it may be read from the test goroutine after execution.
type collectStage struct {
	BaseStage
	mu  sync.Mutex
	got []StreamElement
}

func newCollectStage(name string) *collectStage {
	return &collectStage{BaseStage: NewBaseStage(name, StageTypeSink)}
}

func (c *collectStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)
	for elem := range input {
		c.mu.Lock()
		c.got = append(c.got, elem)
		c.mu.Unlock()
	}
	return ctx.Err()
}

func (c *collectStage) elements() []StreamElement {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]StreamElement, len(c.got))
	copy(out, c.got)
	return out
}

// routeToDest returns a RouterFunc that sends Source == "a" elements to destA
// and Source == "b" elements to destB, dropping anything else.
func routeToDest(destA, destB string) RouterFunc {
	return func(elem *StreamElement) []string {
		switch elem.Source {
		case "a":
			return []string{destA}
		case "b":
			return []string{destB}
		default:
			return nil
		}
	}
}

// TestRouterDeterministicSplit proves selective fan-out: every "a"-sourced
// element lands in sinkA and every "b"-sourced element lands in sinkB. A
// competitive Branch (shared channel, first-reader-wins) cannot guarantee this
// — both sinks would race for every element regardless of Source.
func TestRouterDeterministicSplit(t *testing.T) {
	router := NewRouterStage("router", routeToDest("sinkA", "sinkB"))
	sinkA := newCollectStage("sinkA")
	sinkB := newCollectStage("sinkB")

	p, err := NewPipelineBuilder().
		AddStage(router).AddStage(sinkA).AddStage(sinkB).
		Connect("router", "sinkA").
		Connect("router", "sinkB").
		Build()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const perSource = 25
	in := make(chan StreamElement, perSource*2)
	for i := 0; i < perSource; i++ {
		in <- StreamElement{Source: "a"}
		in <- StreamElement{Source: "b"}
	}
	close(in)

	out, err := p.Execute(ctx, in)
	require.NoError(t, err)

	// Router is not a leaf (has downstream edges), so nothing should reach the
	// pipeline output channel — it drains and closes once the sinks finish.
	var strayCount int
	for range out {
		strayCount++
	}
	assert.Equal(t, 0, strayCount, "router output must not leak into pipeline output")

	gotA := sinkA.elements()
	gotB := sinkB.elements()
	require.Len(t, gotA, perSource, "sinkA receives exactly the 'a' elements")
	require.Len(t, gotB, perSource, "sinkB receives exactly the 'b' elements")
	for _, e := range gotA {
		assert.Equal(t, "a", e.Source, "sinkA must never see a 'b' element")
	}
	for _, e := range gotB {
		assert.Equal(t, "b", e.Source, "sinkB must never see an 'a' element")
	}
}

// TestRouterFanOutThenFanIn composes selective fan-out with the existing
// fan-in (MergeStage): router splits by Source into two tracks, each track
// passes through a passthrough stage, and both tracks merge back into one
// collector. Asserts per-source counts and that MergeInputIndex is stamped,
// proving fan-out and fan-in compose.
func TestRouterFanOutThenFanIn(t *testing.T) {
	router := NewRouterStage("router", routeToDest("trackA", "trackB"))
	trackA := NewPassthroughStage("trackA")
	trackB := NewPassthroughStage("trackB")
	merge := NewMergeStage("merge", 2)
	sink := newCollectStage("sink")

	p, err := NewPipelineBuilder().
		AddStage(router).AddStage(trackA).AddStage(trackB).AddStage(merge).AddStage(sink).
		Connect("router", "trackA").
		Connect("router", "trackB").
		Merge("merge", "trackA", "trackB").
		Connect("merge", "sink").
		Build()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const perSource = 10
	in := make(chan StreamElement, perSource*2)
	for i := 0; i < perSource; i++ {
		in <- StreamElement{Source: "a"}
		in <- StreamElement{Source: "b"}
	}
	close(in)

	out, err := p.Execute(ctx, in)
	require.NoError(t, err)
	for range out {
		// sink is the only leaf; nothing should reach the pipeline output.
		t.Fatal("unexpected element on pipeline output; sink should have consumed it")
	}

	got := sink.elements()
	require.Len(t, got, perSource*2)

	var countA, countB, tagged int
	for _, e := range got {
		switch e.Source {
		case "a":
			countA++
		case "b":
			countB++
		}
		if e.Meta.MergeInputIndex != nil {
			tagged++
		}
	}
	assert.Equal(t, perSource, countA)
	assert.Equal(t, perSource, countB)
	assert.Equal(t, perSource*2, tagged, "merge stamps MergeInputIndex on every element")
}

// TestPipelineConcurrentExecute proves that a single built *StreamPipeline
// (router-less, so it exercises the general Execute() path used by the SDK's
// unary Send()) can be Execute()'d from multiple goroutines concurrently
// without racing. It regression-tests the fix that made the per-Execute
// MultiOutputStage edge-channel map (edgeChannels) a local threaded through
// createChannels/startStages/getStageInputs/upstreamChannel instead of a
// shared field written on every Execute() — the shared-field version raced
// under `go test -race` when two Execute() calls ran concurrently on the same
// pipeline (the SDK's Conversation.Send() reuses one built pipeline across
// concurrent calls, guarded only by an RLock).
func TestPipelineConcurrentExecute(t *testing.T) {
	p, err := NewPipelineBuilder().
		Chain(NewPassthroughStage("stageA"), NewPassthroughStage("stageB")).
		Build()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const goroutines = 8
	const perGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()

			in := make(chan StreamElement, perGoroutine)
			for i := 0; i < perGoroutine; i++ {
				in <- StreamElement{Source: "concurrent"}
			}
			close(in)

			out, execErr := p.Execute(ctx, in)
			assert.NoError(t, execErr)

			var got int
			for range out {
				got++
			}
			assert.Equal(t, perGoroutine, got, "each concurrent Execute() must see exactly its own elements")
		}()
	}
	wg.Wait()
}
