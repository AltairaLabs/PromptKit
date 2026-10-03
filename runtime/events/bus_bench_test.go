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

// BenchmarkEventBusDispatch measures handing one event to every listener and
// the listeners running it, without the publish channel hop. allocs/op is the
// cost a single streamed token pays per event. Each listener's queue holds
// every event, so none is dropped and the timer covers every delivery.
func BenchmarkEventBusDispatch(b *testing.B) {
	for _, n := range benchListenerCounts {
		b.Run(fmt.Sprintf("listeners=%d", n), func(b *testing.B) {
			bus := NewEventBus(WithEventBufferSize(b.N + 1))
			defer bus.Close()

			var delivered atomic.Int64
			for range n {
				bus.SubscribeAll(func(*Event) { delivered.Add(1) })
			}
			event := newReasoningDelta()
			want := int64(b.N * n)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				bus.deliver(event)
			}
			for delivered.Load() < want {
				runtime.Gosched()
			}
			b.StopTimer()

			if got := delivered.Load(); got != want {
				b.Fatalf("delivered %d deliveries, want %d", got, want)
			}
		})
	}
}

// BenchmarkEventBusPublishStream measures the end-to-end path: concurrent
// streams publish token events and the bus delivers them to every listener.
// Each op is one token, timed until every listener has received it. Buffers
// hold every event, so nothing is dropped and every delivery is timed.
func BenchmarkEventBusPublishStream(b *testing.B) {
	for _, n := range benchListenerCounts {
		if n == 0 {
			continue // nothing would start delivery, so nothing to wait for
		}
		b.Run(fmt.Sprintf("listeners=%d", n), func(b *testing.B) {
			bus := NewEventBus(WithEventBufferSize(b.N + 1))
			defer bus.Close()

			var delivered atomic.Int64
			for range n {
				bus.SubscribeAll(func(*Event) { delivered.Add(1) })
			}

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if !bus.Publish(newReasoningDelta()) {
						b.Error("publish dropped an event the buffer had room for")
						return
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
