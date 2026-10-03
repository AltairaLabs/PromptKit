package main

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// countingStore is a minimal events.EventStore for exercising the event bus
// under load (#2146). It does what a cheap production store does per event
// (look at it, collate it) and nothing more, so a benchmark run with it
// measures the bus's delivery cost rather than storage I/O.
//
// Attaching any EventStore via sdk.WithEventStore subscribes it to the
// conversation's bus; without a subscriber the bus never dispatches.
type countingStore struct {
	total     atomic.Int64
	reasoning atomic.Int64
	bytes     atomic.Int64
	byType    sync.Map // events.EventType -> *atomic.Int64
}

var _ events.EventStore = (*countingStore)(nil)

func (s *countingStore) Append(_ context.Context, e *events.Event) error {
	s.OnEvent(e)
	return nil
}

func (s *countingStore) OnEvent(e *events.Event) {
	s.total.Add(1)
	n, _ := s.byType.LoadOrStore(e.Type, new(atomic.Int64))
	n.(*atomic.Int64).Add(1)
	if d, ok := e.Data.(*events.ReasoningDeltaData); ok {
		s.reasoning.Add(1)
		s.bytes.Add(int64(len(d.Text)))
	}
}

func (s *countingStore) Query(context.Context, *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}

func (s *countingStore) QueryRaw(context.Context, *events.EventFilter) ([]*events.StoredEvent, error) {
	return nil, nil
}

func (s *countingStore) Stream(context.Context, string) (<-chan *events.Event, error) {
	ch := make(chan *events.Event)
	close(ch)
	return ch, nil
}

func (s *countingStore) Close() error { return nil }

// ServeHTTP reports how many events reached the store, so a run can confirm
// the bus actually delivered (a zero reasoning count means the upstream
// streamed no reasoning, or nothing was subscribed). It also reports the
// process's goroutine count: once a run has finished it should fall back to
// the server's baseline, and a count that tracks the number of conversations
// served means something per-conversation leaked (#2146 leaked ten each).
func (s *countingStore) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	byType := map[string]int64{}
	s.byType.Range(func(k, v any) bool {
		byType[string(k.(events.EventType))] = v.(*atomic.Int64).Load()
		return true
	})
	_ = json.NewEncoder(w).Encode(map[string]any{
		"events":          s.total.Load(),
		"reasoning_delta": s.reasoning.Load(),
		"reasoning_bytes": s.bytes.Load(),
		"by_type":         byType,
		"goroutines":      runtime.NumGoroutine(),
	})
}
