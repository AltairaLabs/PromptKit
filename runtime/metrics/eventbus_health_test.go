package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

func TestEventBusHealthCollector_ReadsDroppedAtScrape(t *testing.T) {
	// Bus with a 1-slot buffer and no subscribers, so publishes past capacity drop.
	bus := events.NewEventBus(events.WithEventBufferSize(1))
	t.Cleanup(bus.Close)

	c := NewEventBusHealthCollector(bus, "test", nil)
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)

	// Saturate: publish many events with no consumer draining.
	for i := 0; i < 100; i++ {
		bus.Publish(&events.Event{Type: events.EventPipelineStarted})
	}
	if bus.DroppedCount() == 0 {
		t.Fatalf("expected some dropped events to exercise the exporter")
	}

	got := testutil.ToFloat64(c)
	if got < 1 {
		t.Errorf("eventbus_events_dropped_total = %v, want >= 1", got)
	}

	// Value is read at scrape time: more drops -> higher gauge on next gather.
	for i := 0; i < 100; i++ {
		bus.Publish(&events.Event{Type: events.EventPipelineStarted})
	}
	if got2 := testutil.ToFloat64(c); got2 < got {
		t.Errorf("expected scrape value to rise with more drops: %v -> %v", got, got2)
	}

	// Confirm the metric family name is exposed in a scrape.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var names []string
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	if !strings.Contains(strings.Join(names, ","), "test_eventbus_events_dropped_total") {
		t.Errorf("scrape missing test_eventbus_events_dropped_total; got %v", names)
	}
}

func TestEventBusHealthCollector_NilBusIsSafe(t *testing.T) {
	c := NewEventBusHealthCollector(nil, "test", nil)
	reg := prometheus.NewRegistry()
	reg.MustRegister(c) // Describe/Collect must not panic on nil bus
	if _, err := reg.Gather(); err != nil {
		t.Fatalf("Gather with nil bus: %v", err)
	}
}
