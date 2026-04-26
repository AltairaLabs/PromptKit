package probes

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// stubReporter implements the local reporter interface (Helper + Errorf) so
// unit tests can drive the violation path of AssertHolds without going
// through *testing.T.
type stubReporter struct {
	errors []string
}

func (s *stubReporter) Helper() {}

func (s *stubReporter) Errorf(format string, args ...any) {
	s.errors = append(s.errors, format)
}

// ---------------------------------------------------------------------------
// Bound
// ---------------------------------------------------------------------------

func TestBound_Exactly(t *testing.T) {
	b := Exactly(3)
	assert.False(t, b.Contains(2))
	assert.True(t, b.Contains(3))
	assert.False(t, b.Contains(4))
	assert.Equal(t, "exactly 3", b.String())
}

func TestBound_AtMost(t *testing.T) {
	b := AtMost(2)
	assert.True(t, b.Contains(0))
	assert.True(t, b.Contains(2))
	assert.False(t, b.Contains(3))
	assert.Equal(t, "at most 2", b.String())
}

func TestBound_AtLeast(t *testing.T) {
	b := AtLeast(1)
	assert.False(t, b.Contains(0))
	assert.True(t, b.Contains(1))
	assert.True(t, b.Contains(1_000_000))
	assert.Equal(t, "at least 1", b.String())
}

func TestBound_Range(t *testing.T) {
	b := Range(2, 5)
	assert.False(t, b.Contains(1))
	assert.True(t, b.Contains(2))
	assert.True(t, b.Contains(4))
	assert.True(t, b.Contains(5))
	assert.False(t, b.Contains(6))
	assert.Equal(t, "between 2 and 5", b.String())
}

// ---------------------------------------------------------------------------
// Contract assertions (via the unexported assert helper that takes reporter)
// ---------------------------------------------------------------------------

func TestStageContract_Pass(t *testing.T) {
	contract := StageContract{
		Stage: "demo",
		PerSend: Ops{
			"events.demo.x": Exactly(1),
			"events.demo.y": AtMost(2),
		},
	}
	snap := Snapshot{counts: map[Op]int{
		"events.demo.x": 1,
		"events.demo.y": 0,
	}}
	r := &stubReporter{}
	contract.assert(r, snap)
	assert.Empty(t, r.errors)
}

func TestStageContract_AllViolationsReported(t *testing.T) {
	contract := StageContract{
		Stage: "demo",
		PerSend: Ops{
			"events.demo.x": Exactly(1),
			"events.demo.y": AtMost(0),
		},
	}
	snap := Snapshot{counts: map[Op]int{
		"events.demo.x": 5,
		"events.demo.y": 7,
	}}
	r := &stubReporter{}
	contract.assert(r, snap)
	assert.Len(t, r.errors, 2, "both clauses should report independently")
}

func TestPipelineInvariants_Pass(t *testing.T) {
	inv := PipelineInvariants{
		Label: "store-budget",
		PerSend: Ops{
			"store.Load": Exactly(1),
			"store.Save": Exactly(1),
		},
	}
	snap := Snapshot{counts: map[Op]int{"store.Load": 1, "store.Save": 1}}
	r := &stubReporter{}
	inv.assert(r, snap)
	assert.Empty(t, r.errors)
}

func TestPipelineInvariants_Fail(t *testing.T) {
	inv := PipelineInvariants{
		Label: "store-budget",
		PerSend: Ops{
			"store.Load": Exactly(1),
		},
	}
	snap := Snapshot{counts: map[Op]int{"store.Load": 2}}
	r := &stubReporter{}
	inv.assert(r, snap)
	assert.Len(t, r.errors, 1)
}

func TestPipelineInvariants_DefaultLabel(t *testing.T) {
	inv := PipelineInvariants{
		PerSend: Ops{"x": Exactly(0)},
	}
	snap := Snapshot{counts: map[Op]int{"x": 1}}
	r := &stubReporter{}
	inv.assert(r, snap)
	assert.Len(t, r.errors, 1)
}

// AssertHolds itself is the public *testing.T variant — exercise it once
// against a passing case so the t.Helper / delegation path is covered.
func TestStageContract_AssertHolds_Public(t *testing.T) {
	contract := StageContract{
		Stage:   "demo",
		PerSend: Ops{"x": AtMost(0)},
	}
	contract.AssertHolds(t, Snapshot{counts: map[Op]int{"x": 0}})
}

func TestPipelineInvariants_AssertHolds_Public(t *testing.T) {
	inv := PipelineInvariants{PerSend: Ops{"x": AtMost(0)}}
	inv.AssertHolds(t, Snapshot{counts: map[Op]int{"x": 0}})
}

// ---------------------------------------------------------------------------
// probedStore
// ---------------------------------------------------------------------------

func TestProbedStore_CountsCallsAndForwards(t *testing.T) {
	inner := statestore.NewMemoryStore()
	s := newProbedStore(inner)

	state := &statestore.ConversationState{ID: "c1"}
	require.NoError(t, s.Save(context.Background(), state))
	got, err := s.Load(context.Background(), "c1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, s.Save(context.Background(), state))

	loads, saves, forks := s.snapshot()
	assert.Equal(t, 1, loads)
	assert.Equal(t, 2, saves)
	assert.Equal(t, 0, forks)

	s.reset()
	loads, saves, forks = s.snapshot()
	assert.Equal(t, 0, loads)
	assert.Equal(t, 0, saves)
	assert.Equal(t, 0, forks)
}

func TestProbedStore_ForkCounts(t *testing.T) {
	inner := statestore.NewMemoryStore()
	require.NoError(t, inner.Save(context.Background(), &statestore.ConversationState{ID: "src"}))
	s := newProbedStore(inner)
	require.NoError(t, s.Fork(context.Background(), "src", "dst"))
	_, _, forks := s.snapshot()
	assert.Equal(t, 1, forks)
}

func TestProbedStore_OptionalInterfacesDelegate(t *testing.T) {
	inner := statestore.NewMemoryStore()
	require.NoError(t, inner.Save(context.Background(), &statestore.ConversationState{
		ID:       "c1",
		Messages: []types.Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "yo"}},
	}))
	s := newProbedStore(inner)

	// MessageReader: LoadRecentMessages + MessageCount
	recent, err := s.LoadRecentMessages(context.Background(), "c1", 10)
	require.NoError(t, err)
	assert.Len(t, recent, 2)
	count, err := s.MessageCount(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// MessageAppender: MemoryStore implements it
	require.NoError(t, s.AppendMessages(context.Background(), "c1",
		[]types.Message{{Role: "user", Content: "more"}}))
	count, err = s.MessageCount(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// MetadataAccessor — return value may be nil for state without metadata;
	// what matters is that the call delegates without error.
	_, err = s.LoadMetadata(context.Background(), "c1")
	require.NoError(t, err)

	// SummaryAccessor: LoadSummaries returns nil for fresh state
	summaries, err := s.LoadSummaries(context.Background(), "c1")
	require.NoError(t, err)
	assert.Empty(t, summaries)
	require.NoError(t, s.SaveSummary(context.Background(), "c1",
		statestore.Summary{StartTurn: 0, EndTurn: 1, Content: "summary"}))
	summaries, err = s.LoadSummaries(context.Background(), "c1")
	require.NoError(t, err)
	assert.Len(t, summaries, 1)
}

// stubBareStore implements only statestore.Store (no optional interfaces).
type stubBareStore struct{}

func (stubBareStore) Load(_ context.Context, _ string) (*statestore.ConversationState, error) {
	return &statestore.ConversationState{}, nil
}

func (stubBareStore) Save(_ context.Context, _ *statestore.ConversationState) error { return nil }

func (stubBareStore) Fork(_ context.Context, _, _ string) error { return nil }

func TestProbedStore_OptionalInterfacesFallbackOnBareStore(t *testing.T) {
	s := newProbedStore(stubBareStore{})

	_, err := s.LoadRecentMessages(context.Background(), "c", 10)
	assert.ErrorIs(t, err, statestore.ErrNotFound)

	_, err = s.MessageCount(context.Background(), "c")
	assert.ErrorIs(t, err, statestore.ErrNotFound)

	err = s.AppendMessages(context.Background(), "c", nil)
	assert.ErrorIs(t, err, statestore.ErrNotFound)

	_, err = s.LoadMetadata(context.Background(), "c")
	assert.ErrorIs(t, err, statestore.ErrNotFound)

	summaries, err := s.LoadSummaries(context.Background(), "c")
	assert.NoError(t, err)
	assert.Nil(t, summaries)

	err = s.SaveSummary(context.Background(), "c", statestore.Summary{})
	assert.ErrorIs(t, err, statestore.ErrNotFound)
}

func TestProbedProvider_PredictAndStreamCount(t *testing.T) {
	inner := mock.NewProvider("test-mock", "test-model", false)
	p := newProbedProvider(inner)

	// ID/Model/SupportsStreaming/ShouldIncludeRawOutput/Close/CalculateCost
	assert.NotEmpty(t, p.ID())
	assert.NotEmpty(t, p.Model())
	_ = p.SupportsStreaming()
	_ = p.ShouldIncludeRawOutput()
	cost := p.CalculateCost(100, 50, 0)
	assert.NotNil(t, cost)

	_, err := p.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, p.predicts())

	if p.SupportsStreaming() {
		ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "hi"}},
		})
		require.NoError(t, err)
		// Drain so the goroutine exits cleanly.
		for range ch {
		}
		// Stream count is internal state; nothing to assert beyond no error.
	}

	require.NoError(t, p.Close())
	p.reset()
	assert.Equal(t, 0, p.predicts())
}

// ---------------------------------------------------------------------------
// Probes (Snapshot / ResetCounters / Bus)
// ---------------------------------------------------------------------------

const (
	eventDeliveryTimeout = 2 * time.Second
	eventDeliveryPoll    = 5 * time.Millisecond
)

func newTestProbes(t *testing.T) *Probes {
	t.Helper()
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	p := &Probes{
		store:              newProbedStore(statestore.NewMemoryStore()),
		bus:                bus,
		summarizerProvider: newProbedProvider(mock.NewProvider("unit-summarizer", "unit-summarizer-model", false)),
		events:             map[events.EventType]int{},
		eventRecords:       map[events.EventType][]*events.Event{},
	}
	bus.SubscribeAll(func(e *events.Event) {
		p.mu.Lock()
		p.events[e.Type]++
		p.eventRecords[e.Type] = append(p.eventRecords[e.Type], e)
		p.mu.Unlock()
	})
	return p
}

func TestProbes_SnapshotIncludesEventsAndStore(t *testing.T) {
	p := newTestProbes(t)

	require.NoError(t, p.store.Save(context.Background(), &statestore.ConversationState{ID: "x"}))
	p.bus.Publish(&events.Event{Type: events.EventTemplateRendered})
	p.bus.Publish(&events.Event{Type: events.EventTemplateRendered})
	p.bus.Publish(&events.Event{Type: events.EventTemplateStarted})

	require.Eventually(t, func() bool {
		s := p.Snapshot()
		return s.Count("events.prompt.template.rendered") == 2 &&
			s.Count("events.prompt.template.started") == 1
	}, eventDeliveryTimeout, eventDeliveryPoll)

	snap := p.Snapshot()
	assert.Equal(t, 0, snap.Count("events.prompt.template.failed"))
	assert.Equal(t, 1, snap.Count("store.Save"))

	all := snap.All()
	assert.Equal(t, 2, all["events.prompt.template.rendered"])
	// All() returns a copy — mutating it must not affect future snapshots.
	all["events.prompt.template.rendered"] = 999
	assert.Equal(t, 2, p.Snapshot().Count("events.prompt.template.rendered"))
}

func TestProbes_ResetCounters(t *testing.T) {
	p := newTestProbes(t)

	require.NoError(t, p.store.Save(context.Background(), &statestore.ConversationState{ID: "x"}))
	p.bus.Publish(&events.Event{Type: events.EventTemplateRendered})
	require.Eventually(t, func() bool {
		return p.Snapshot().Count("events.prompt.template.rendered") == 1
	}, eventDeliveryTimeout, eventDeliveryPoll)

	p.ResetCounters()
	snap := p.Snapshot()
	assert.Equal(t, 0, snap.Count("store.Save"))
	assert.Equal(t, 0, snap.Count("events.prompt.template.rendered"))
}

func TestProbes_BusReturnsSameInstance(t *testing.T) {
	p := newTestProbes(t)
	assert.Same(t, p.bus, p.Bus())
}

func TestProbes_WaitForCount_Reaches(t *testing.T) {
	p := newTestProbes(t)
	go func() {
		time.Sleep(10 * time.Millisecond)
		p.bus.Publish(&events.Event{Type: events.EventTemplateRendered})
	}()
	assert.True(t, p.WaitForCount("events.prompt.template.rendered", 1, 2*time.Second))
}

func TestProbes_WaitForCount_Timeout(t *testing.T) {
	p := newTestProbes(t)
	assert.False(t, p.WaitForCount("events.prompt.template.rendered", 1, 20*time.Millisecond))
}

// ---------------------------------------------------------------------------
// Run (smoke + seed)
// ---------------------------------------------------------------------------

func TestRun_DefaultsAreSane(t *testing.T) {
	p, conv := Run(t, RunOptions{})
	require.NotNil(t, p)
	require.NotNil(t, conv)
	// Counters reset post-Open: a fresh Snapshot reports zero.
	snap := p.Snapshot()
	assert.Equal(t, 0, snap.Count("store.Load"))
	assert.Equal(t, 0, snap.Count("store.Save"))
}

func TestRun_SeedHistoryFlowsThroughSend(t *testing.T) {
	const seedN = 3
	p, conv := Run(t, RunOptions{SeedHistory: seedN})
	require.NotNil(t, conv)

	_, err := conv.Send(context.Background(), "ping")
	require.NoError(t, err)

	// The pipeline must Save at least once; Load count is left for the
	// pipeline contract test in the parent package to assert.
	assert.GreaterOrEqual(t, p.Snapshot().Count("store.Save"), 1)
}
