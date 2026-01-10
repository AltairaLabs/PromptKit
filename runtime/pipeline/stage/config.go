package stage

import (
	"time"
)

const (
	// DefaultChannelBufferSize is the default buffer size for channels between stages.
	DefaultChannelBufferSize = 16
	// DefaultMaxConcurrentPipelines is the default maximum number of concurrent pipeline executions.
	DefaultMaxConcurrentPipelines = 100
	// DefaultExecutionTimeoutSeconds is the default execution timeout in seconds.
	DefaultExecutionTimeoutSeconds = 30
	// DefaultGracefulShutdownTimeoutSeconds is the default graceful shutdown timeout in seconds.
	DefaultGracefulShutdownTimeoutSeconds = 10
)

// PipelineConfig defines configuration options for pipeline execution.
type PipelineConfig struct {
	// ChannelBufferSize controls buffering between stages.
	// Smaller values = lower latency but more backpressure.
	// Larger values = higher throughput but more memory usage.
	// Default: 16
	ChannelBufferSize int

	// PriorityQueueEnabled enables priority-based scheduling.
	// When enabled, high-priority elements (audio) are processed before low-priority (logs).
	// Default: false
	PriorityQueueEnabled bool

	// MaxConcurrentPipelines limits the number of concurrent pipeline executions.
	// This is used by PipelinePool to control concurrency.
	// Default: 100
	MaxConcurrentPipelines int

	// ExecutionTimeout sets the maximum duration for a single pipeline execution.
	// Set to 0 to disable timeout.
	// Default: 30 seconds
	ExecutionTimeout time.Duration

	// GracefulShutdownTimeout sets the maximum time to wait for in-flight executions during shutdown.
	// Default: 10 seconds
	GracefulShutdownTimeout time.Duration

	// EnableMetrics enables collection of per-stage metrics (latency, throughput, etc.).
	// Default: false
	EnableMetrics bool

	// EnableTracing enables detailed tracing of element flow through stages.
	// Default: false (can be expensive for high-throughput pipelines)
	EnableTracing bool

	// PrometheusEnabled enables Prometheus metrics export via HTTP.
	// Default: false
	PrometheusEnabled bool

	// PrometheusAddr is the address to serve Prometheus metrics on (e.g., ":9090").
	// Only used when PrometheusEnabled is true.
	// Default: ":9090"
	PrometheusAddr string
}

// DefaultPipelineConfig returns a PipelineConfig with sensible defaults.
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		ChannelBufferSize:       DefaultChannelBufferSize,
		PriorityQueueEnabled:    false,
		MaxConcurrentPipelines:  DefaultMaxConcurrentPipelines,
		ExecutionTimeout:        DefaultExecutionTimeoutSeconds * time.Second,
		GracefulShutdownTimeout: DefaultGracefulShutdownTimeoutSeconds * time.Second,
		EnableMetrics:           false,
		EnableTracing:           false,
		PrometheusEnabled:       false,
		PrometheusAddr:          ":9090",
	}
}

// Validate checks if the configuration is valid.
func (c *PipelineConfig) Validate() error {
	if c.ChannelBufferSize < 0 {
		return ErrInvalidChannelBufferSize
	}
	if c.MaxConcurrentPipelines < 0 {
		return ErrInvalidMaxConcurrentPipelines
	}
	if c.ExecutionTimeout < 0 {
		return ErrInvalidExecutionTimeout
	}
	if c.GracefulShutdownTimeout < 0 {
		return ErrInvalidGracefulShutdownTimeout
	}
	return nil
}

// WithChannelBufferSize sets the channel buffer size.
func (c *PipelineConfig) WithChannelBufferSize(size int) *PipelineConfig {
	c.ChannelBufferSize = size
	return c
}

// WithPriorityQueue enables or disables priority-based scheduling.
func (c *PipelineConfig) WithPriorityQueue(enabled bool) *PipelineConfig {
	c.PriorityQueueEnabled = enabled
	return c
}

// WithMaxConcurrentPipelines sets the maximum number of concurrent pipeline executions.
func (c *PipelineConfig) WithMaxConcurrentPipelines(maxPipelines int) *PipelineConfig {
	c.MaxConcurrentPipelines = maxPipelines
	return c
}

// WithExecutionTimeout sets the execution timeout.
func (c *PipelineConfig) WithExecutionTimeout(timeout time.Duration) *PipelineConfig {
	c.ExecutionTimeout = timeout
	return c
}

// WithGracefulShutdownTimeout sets the graceful shutdown timeout.
func (c *PipelineConfig) WithGracefulShutdownTimeout(timeout time.Duration) *PipelineConfig {
	c.GracefulShutdownTimeout = timeout
	return c
}

// WithMetrics enables or disables metrics collection.
func (c *PipelineConfig) WithMetrics(enabled bool) *PipelineConfig {
	c.EnableMetrics = enabled
	return c
}

// WithTracing enables or disables detailed tracing.
func (c *PipelineConfig) WithTracing(enabled bool) *PipelineConfig {
	c.EnableTracing = enabled
	return c
}

// WithPrometheusExporter enables Prometheus metrics export at the given address.
// The address should be in the format ":port" or "host:port".
// Example: ":9090" or "localhost:9090"
func (c *PipelineConfig) WithPrometheusExporter(addr string) *PipelineConfig {
	c.PrometheusEnabled = true
	c.PrometheusAddr = addr
	return c
}
