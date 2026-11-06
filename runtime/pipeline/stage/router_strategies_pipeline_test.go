package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routerUnderTest builds one of the strategy routers wired to sinkA and sinkB.
type routerUnderTest struct {
	name  string
	build func() Stage
	// wantA / wantB are the exact counts each sink must receive, or -1 when the
	// split is not deterministic (random, weighted) and only the total matters.
	wantA, wantB int
	// drainsOnCancel marks RouterStage, which logs a cancelled send and keeps
	// reading its input, so it returns nil once the input closes rather than
	// the context error the strategy routers return.
	drainsOnCancel bool
}

const strategyRouterElems = 10

func strategyRouters() []routerUnderTest {
	sinks := []string{"sinkA", "sinkB"}
	return []routerUnderTest{
		{name: "router_stage", build: func() Stage { return NewRouterStage("router", routeToDest("sinkA", "sinkB")) },
			wantA: strategyRouterElems / 2, wantB: strategyRouterElems / 2, drainsOnCancel: true},
		{name: "broadcast", build: func() Stage { return NewBroadcastRouter("router") },
			wantA: strategyRouterElems, wantB: strategyRouterElems},
		{name: "content", build: func() Stage {
			return NewContentRouter("router",
				RouteWhen("sinkA", func(e StreamElement) bool { return e.Source == "a" }),
				RouteWhen("sinkB", func(StreamElement) bool { return true }))
		}, wantA: strategyRouterElems / 2, wantB: strategyRouterElems / 2},
		{name: "round_robin", build: func() Stage { return NewRoundRobinRouter("router", sinks) },
			wantA: strategyRouterElems / 2, wantB: strategyRouterElems / 2},
		{name: "hash", build: func() Stage {
			return NewHashRouter("router", sinks, func(e StreamElement) string { return e.Source })
		}, wantA: -1, wantB: -1},
		{name: "weighted", build: func() Stage {
			return NewWeightedRouter("router", map[string]float64{"sinkA": 1, "sinkB": 1})
		}, wantA: -1, wantB: -1},
		{name: "random", build: func() Stage { return NewRandomRouter("router", sinks) },
			wantA: -1, wantB: -1},
	}
}

// TestStrategyRouters_BranchPipelineCompletes builds a real pipeline in which
// each strategy router fans out to two branches through Branch, and runs it
// with ExecuteSync. The branches read the per-edge channels the pipeline
// registers through RegisterOutput, so they only finish when the router closes
// those channels. A router that closes only its main output leaves both
// branches waiting forever and the pipeline never completes.
func TestStrategyRouters_BranchPipelineCompletes(t *testing.T) {
	for _, tc := range strategyRouters() {
		t.Run(tc.name, func(t *testing.T) {
			sinkA := newCollectStage("sinkA")
			sinkB := newCollectStage("sinkB")
			p, err := NewPipelineBuilder().
				AddStage(tc.build()).AddStage(sinkA).AddStage(sinkB).
				Branch("router", "sinkA", "sinkB").
				Build()
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			elems := make([]StreamElement, 0, strategyRouterElems)
			for i := 0; i < strategyRouterElems; i++ {
				src := "a"
				if i%2 == 1 {
					src = "b"
				}
				elems = append(elems, StreamElement{Source: src})
			}

			done := make(chan error, 1)
			go func() {
				_, execErr := p.ExecuteSync(ctx, elems...)
				done <- execErr
			}()

			select {
			case execErr := <-done:
				require.NoError(t, execErr, "pipeline must finish cleanly, not by timeout")
			case <-time.After(2 * time.Second):
				t.Fatal("pipeline hung: the router left its branch channels open")
			}

			gotA, gotB := len(sinkA.elements()), len(sinkB.elements())
			if tc.wantA >= 0 {
				assert.Equal(t, tc.wantA, gotA, "sinkA count")
				assert.Equal(t, tc.wantB, gotB, "sinkB count")
			} else {
				assert.Equal(t, strategyRouterElems, gotA+gotB, "every element reaches one branch")
			}
		})
	}
}

// TestStrategyRouters_SharedChannelClosedOnce registers one channel under both
// names; closing the registered outputs must not close it twice.
func TestStrategyRouters_SharedChannelClosedOnce(t *testing.T) {
	for _, tc := range strategyRouters() {
		t.Run(tc.name, func(t *testing.T) {
			router, ok := tc.build().(MultiOutputStage)
			require.True(t, ok, "router must implement MultiOutputStage")

			shared := make(chan StreamElement, 4)
			router.RegisterOutput("sinkA", shared)
			router.RegisterOutput("sinkB", shared)

			in := make(chan StreamElement, 1)
			in <- StreamElement{Source: "a"}
			close(in)

			require.NotPanics(t, func() {
				_ = router.Process(context.Background(), in, make(chan StreamElement, 1))
			})
			var got int
			for range shared {
				got++
			}
			assert.Positive(t, got, "the element reaches the shared channel, which is then closed")
		})
	}
}

// TestStrategyRouters_CloseOutputsOnCancel checks that a router still closes
// every registered output when it returns early because the context ended.
func TestStrategyRouters_CloseOutputsOnCancel(t *testing.T) {
	for _, tc := range strategyRouters() {
		t.Run(tc.name, func(t *testing.T) {
			router, ok := tc.build().(MultiOutputStage)
			require.True(t, ok, "router must implement MultiOutputStage")

			// Unbuffered and never read, so the first send blocks until ctx ends.
			outA := make(chan StreamElement)
			outB := make(chan StreamElement)
			router.RegisterOutput("sinkA", outA)
			router.RegisterOutput("sinkB", outB)

			in := make(chan StreamElement, 1)
			in <- StreamElement{Source: "a"}
			close(in)
			main := make(chan StreamElement, 1)

			ctx, cancel := context.WithCancel(context.Background())
			errCh := make(chan error, 1)
			go func() { errCh <- router.Process(ctx, in, main) }()
			cancel()

			select {
			case err := <-errCh:
				if tc.drainsOnCancel {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("router did not return after cancel")
			}

			for name, ch := range map[string]chan StreamElement{"sinkA": outA, "sinkB": outB, "main": main} {
				select {
				case _, open := <-ch:
					assert.False(t, open, "%s must be closed", name)
				default:
					t.Errorf("%s left open", name)
				}
			}
		})
	}
}
