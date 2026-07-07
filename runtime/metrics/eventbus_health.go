package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// eventBusHealthCollector is a pull-based Prometheus collector that exports
// eventbus_events_dropped_total by reading EventBus.DroppedCount() at scrape
// time. It runs NO goroutine and does NO per-event work — the event bus stays
// Prometheus-free, and this metric is the early-warning signal for bus
// saturation under burst load (see AltairaLabs/PromptKit#853).
type eventBusHealthCollector struct {
	bus     *events.EventBus
	dropped *prometheus.Desc
}

// NewEventBusHealthCollector builds the collector for a bus. A nil bus yields a
// collector whose Describe/Collect are safe no-ops (nothing to export).
func NewEventBusHealthCollector(
	bus *events.EventBus,
	namespace string,
	constLabels prometheus.Labels,
) prometheus.Collector {
	if namespace == "" {
		namespace = defaultNamespace
	}
	return &eventBusHealthCollector{
		bus: bus,
		dropped: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "eventbus_events_dropped_total"),
			"Total events dropped by the event bus because its buffer was full. "+
				"Read at scrape time from EventBus.DroppedCount(). A rising value is "+
				"the early-warning signal that the bus is saturated under burst load; "+
				"tune PROMPTKIT_EVENT_BUS_* before it starves autoscaling signals.",
			nil, constLabels,
		),
	}
}

// Describe implements prometheus.Collector.
func (c *eventBusHealthCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.dropped
}

// Collect implements prometheus.Collector. It reads the live dropped count from
// the bus at scrape time (no goroutine, no per-event work).
func (c *eventBusHealthCollector) Collect(ch chan<- prometheus.Metric) {
	if c.bus == nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(
		c.dropped, prometheus.CounterValue, float64(c.bus.DroppedCount()),
	)
}
