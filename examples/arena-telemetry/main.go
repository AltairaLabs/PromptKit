// Example: Running Arena programmatically with OTel tracing and Prometheus metrics.
//
// This shows how to wire up the same observability infrastructure that the SDK
// uses (WithTracerProvider, WithMetrics) to Arena's engine for programmatic use.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/metrics"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

func main() {
	ctx := context.Background()

	// --- OTel setup: stdout exporter for demo, replace with OTLP for production ---
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		log.Fatal(err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
	defer func() { _ = tp.Shutdown(ctx) }()
	otel.SetTracerProvider(tp)

	// --- Prometheus setup ---
	promRegistry := prometheus.NewRegistry()
	metricsCollector, err := metrics.NewCollector("arena", promRegistry, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Expose /metrics for scraping
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{}))
		log.Println("Metrics available at :9090/metrics")
		_ = http.ListenAndServe(":9090", mux)
	}()

	// --- Arena engine setup ---
	eng, err := engine.NewEngineFromConfigFile("arena.yaml")
	if err != nil {
		log.Fatal(err)
	}
	defer eng.Close()

	// Wire observability — order matters: bus first, then consumers
	bus := events.NewEventBus()
	defer bus.Close()

	eng.SetEventBus(bus)
	eng.SetTracerProvider(tp)
	eng.SetMetrics(metricsCollector, map[string]string{
		"environment": "ci",
		"suite":       "nightly",
	})

	// --- Execute ---
	plan, err := eng.GenerateRunPlan(nil, nil, nil, nil) // all scenarios
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Running %d test combinations...\n", len(plan.Combinations))
	runIDs, err := eng.ExecuteRuns(ctx, plan, 4) // concurrency=4
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Completed %d runs\n", len(runIDs))

	// At this point:
	// - OTel spans have been exported for every provider call (agent, selfplay, judge)
	// - Prometheus metrics are available at :9090/metrics with source labels
	// - All events flowed through the same bus the SDK uses
}
