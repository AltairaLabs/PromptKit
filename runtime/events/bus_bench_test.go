package events

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
)

// Benchmarks for the per-delivery cost of the EventBus (#2146).
//
// reasoning.delta is published once per streamed reasoning token, so these
// measure the path a streaming turn takes once anything subscribes to the
// bus: an event store, the OTel listener or a metrics collector (the SDK
// wires each with SubscribeAll). With no subscriber the workers never start
// and dispatch never runs.
//
// Run with:
//
//	go -C runtime test ./events/ -run '^$' -bench EventBus -benchmem

// benchListenerCounts covers no listener, one (an event store alone) and
// three (store, OTel and metrics together).
var benchListenerCounts = []int{0, 1, 3}

func newReasoningDelta() *Event {
	return &Event{
		Type: EventReasoningDelta,
		Data: &ReasoningDeltaData{Text: "tok", Round: 1, ProviderCallID: "call-1"},
	}
}

// BenchmarkEventBusDispatch measures one dispatch of one event to every
// listener, without the worker channel hop. allocs/op is the cost a single
// streamed token pays inside a worker.
func BenchmarkEventBusDispatch(b *testing.B) {
	for _, n := range benchListenerCounts {
		b.Run(fmt.Sprintf("listeners=%d", n), func(b *testing.B) {
			bus := NewEventBus(WithWorkerPoolSize(1))
			defer bus.Close()

			var delivered atomic.Int64
			for range n {
				bus.SubscribeAll(func(*Event) { delivered.Add(1) })
			}
			event := newReasoningDelta()

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				bus.dispatch(event)
			}
			b.StopTimer()

			if got, want := delivered.Load(), int64(b.N*n); got != want {
				b.Fatalf("delivered %d deliveries, want %d", got, want)
			}
		})
	}
}

// BenchmarkEventBusPublishStream measures the end-to-end path: concurrent
// streams publish token events and the default worker pool delivers them.
// Each op is one token, timed until every listener has received it.
func BenchmarkEventBusPublishStream(b *testing.B) {
	for _, n := range benchListenerCounts {
		if n == 0 {
			continue // nothing would start the workers, so nothing to wait for
		}
		b.Run(fmt.Sprintf("listeners=%d", n), func(b *testing.B) {
			bus := NewEventBus(
				WithWorkerPoolSize(DefaultWorkerPoolSize),
				WithEventBufferSize(DefaultEventBufferSize),
			)
			defer bus.Close()

			var delivered atomic.Int64
			for range n {
				bus.SubscribeAll(func(*Event) { delivered.Add(1) })
			}

			// RunParallel starts GOMAXPROCS publishers.
			limit := cap(bus.eventCh) - runtime.GOMAXPROCS(0)

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					// Publish drops (and logs) when the buffer is full. Leave
					// room for one in-flight send per publisher so every op
					// is a delivered token and the wait below terminates.
					for len(bus.eventCh) > limit {
						runtime.Gosched()
					}
					for !bus.Publish(newReasoningDelta()) {
						runtime.Gosched()
					}
				}
			})
			want := int64(b.N * n)
			for delivered.Load() < want {
				runtime.Gosched()
			}
			b.StopTimer()
		})
	}
}
