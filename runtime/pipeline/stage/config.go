package stage

import (
	"time"
)

const (
	// DefaultChannelBufferSize is the default buffer size for channels between stages.
	// For text pipelines, 16 provides adequate buffering.
	// For audio pipelines at ~50 chunks/sec, consider using DefaultAudioChannelBufferSize.
	DefaultChannelBufferSize = 32
	// DefaultAudioChannelBufferSize is the recommended buffer size for audio pipelines.
	// At 50 chunks/sec, 64 buffers provide ~1.3 seconds of buffering to absorb jitter.
	DefaultAudioChannelBufferSize = 64
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
}

// DefaultPipelineConfig returns a PipelineConfig with sensible defaults.
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		ChannelBufferSize:       DefaultChannelBufferSize,
		MaxConcurrentPipelines:  DefaultMaxConcurrentPipelines,
		ExecutionTimeout:        DefaultExecutionTimeoutSeconds * time.Second,
		GracefulShutdownTimeout: DefaultGracefulShutdownTimeoutSeconds * time.Second,
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
