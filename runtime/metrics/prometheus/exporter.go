// Package prometheus provides Prometheus metrics exporters for PromptKit pipelines.
package prometheus

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// defaultReadHeaderTimeout is the timeout for reading request headers.
	defaultReadHeaderTimeout = 10 * time.Second
)

// Exporter serves Prometheus metrics over HTTP.
type Exporter struct {
	addr     string
	server   *http.Server
	registry *prometheus.Registry
	mu       sync.Mutex
	started  bool
}

// NewExporter creates a new Prometheus exporter that serves metrics at the given address.
func NewExporter(addr string) *Exporter {
	reg := prometheus.NewRegistry()

	// Register all PromptKit metrics
	for _, collector := range allMetrics {
		reg.MustRegister(collector)
	}

	// Register Go runtime metrics
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return &Exporter{
		addr:     addr,
		registry: reg,
	}
}

// NewExporterWithRegistry creates a new Prometheus exporter with a custom registry.
// This is useful for testing or when you want more control over metric registration.
func NewExporterWithRegistry(addr string, registry *prometheus.Registry) *Exporter {
	return &Exporter{
		addr:     addr,
		registry: registry,
	}
}

// Registry returns the underlying Prometheus registry.
func (e *Exporter) Registry() *prometheus.Registry {
	return e.registry
}

// Start begins serving metrics at /metrics endpoint.
// This method blocks until the server is stopped or encounters an error.
// Returns http.ErrServerClosed when shut down gracefully.
func (e *Exporter) Start() error {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return nil
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write error is intentionally ignored - client may have disconnected
		// and there's nothing actionable to do with the error in a health check
		_, _ = w.Write([]byte("ok"))
	})

	e.server = &http.Server{
		Addr:              e.addr,
		Handler:           mux,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
	}
	e.started = true
	e.mu.Unlock()

	return e.server.ListenAndServe()
}

// Shutdown gracefully stops the exporter with the given context.
func (e *Exporter) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.server != nil && e.started {
		e.started = false
		return e.server.Shutdown(ctx)
	}
	return nil
}

// Handler returns an http.Handler for the metrics endpoint.
// This is useful when you want to integrate metrics into an existing HTTP server.
func (e *Exporter) Handler() http.Handler {
	return promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// MustRegister registers additional collectors with the exporter's registry.
// Panics if registration fails.
func (e *Exporter) MustRegister(cs ...prometheus.Collector) {
	e.registry.MustRegister(cs...)
}

// Register registers additional collectors with the exporter's registry.
// Returns an error if registration fails.
func (e *Exporter) Register(c prometheus.Collector) error {
	return e.registry.Register(c)
}
