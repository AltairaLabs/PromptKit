package stage

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
)

const (
	// DefaultChannelBufferSize is the default buffer size for channels between stages.
	// For text pipelines, 32 provides adequate buffering.
	// For audio pipelines at ~50 chunks/sec, consider using DefaultAudioChannelBufferSize.
	DefaultChannelBufferSize = 32
	// DefaultAudioChannelBufferSize is the recommended buffer size for audio pipelines.
	// At 50 chunks/sec, 64 buffers provide ~1.3 seconds of buffering to absorb jitter.
	DefaultAudioChannelBufferSize = 64
	// DefaultMaxConcurrentPipelines is the default maximum number of concurrent pipeline executions.
	DefaultMaxConcurrentPipelines = 100
	// DefaultExecutionTimeoutSeconds is the default execution timeout in seconds.
	// Set to 0 (disabled). IdleTimeout is the primary liveness check.
	DefaultExecutionTimeoutSeconds = 0
	// DefaultIdleTimeoutSeconds is the default idle timeout in seconds.
	// The timer resets on each activity signal (stream chunk, round, tool
	// completion) and is held open while tool calls are in flight.
	DefaultIdleTimeoutSeconds = 30
	// DefaultGracefulShutdownTimeoutSeconds is the default graceful shutdown timeout in seconds.
	DefaultGracefulShutdownTimeoutSeconds = 10
)

// PipelineConfig defines configuration options for pipeline execution.
type PipelineConfig struct {
	// ChannelBufferSize controls buffering between stages.
	// Smaller values = lower latency but more backpressure.
	// Larger values = higher throughput but more memory usage.
	// Default: 32
	ChannelBufferSize int

	// MaxConcurrentPipelines limits how many executions of one pipeline run at
	// once. When the limit is reached, Execute waits for a running execution to
	// finish. It returns the context's error if ctx ends first, and
	// ErrPipelineShuttingDown if Shutdown is called first. Zero means no limit.
	// Validate rejects negative values. Build reads it, so changing it later
	// has no effect on a built pipeline.
	// Default: 100
	MaxConcurrentPipelines int

	// PrioritizeElements makes each stage read its input in priority order: a
	// higher-priority element queued behind lower-priority ones is delivered
	// first, and elements of equal priority keep the order they were sent in.
	// Up to ChannelBufferSize elements wait in each stage's priority queue.
	//
	// With it a control element can overtake content sent before it: an
	// EndOfTurn or EndOfStream (PriorityCritical) can reach a stage ahead of
	// the turn's text or audio. Enable it only when every stage tolerates that.
	// The streaming provider stage does not: it fires a turn on EndOfTurn, so
	// an EndOfTurn ahead of the turn's messages fires nothing and holds the
	// messages for the next turn. False (the default) delivers every element
	// in the order it was sent, whatever its priority.
	PrioritizeElements bool

	// ExecutionTimeout sets a hard maximum duration for a single pipeline execution.
	// This is a safety ceiling. Prefer IdleTimeout for liveness detection.
	// Set to 0 to disable (default).
	ExecutionTimeout time.Duration

	// IdleTimeout sets the maximum duration of inactivity before the pipeline is
	// canceled. The timer resets on each streaming chunk, round completion, and
	// tool completion, and is held open for as long as tool calls are running.
	// A tool in flight is not an idle pipeline, and slow tools are bounded by
	// their own TimeoutMs instead. Set to 0 to disable.
	// Default: 30 seconds.
	IdleTimeout time.Duration

	// GracefulShutdownTimeout sets the maximum time to wait for in-flight executions during shutdown.
	// Default: 10 seconds
	GracefulShutdownTimeout time.Duration

	// ProviderBinding, when non-nil, is attached to the execution context so
	// checks can resolve the LOGICAL provider names their pack declared
	// (evals.BindingFromContext). The host owns the mapping; the pipeline only
	// carries it.
	ProviderBinding evals.ProviderBinding

	// InferenceRegistry, when non-nil, is attached to the execution
	// context (via inference.WithRegistry) so stages and downstream
	// consumers resolve inference providers with inference.FromContext.
	InferenceRegistry *inference.Registry

	// ClassifyRegistry is ignored.
	//
	// Deprecated: use InferenceRegistry. Kept only for v2 compatibility;
	// removed in v3.
	ClassifyRegistry *classify.Registry
}

// DefaultPipelineConfig returns a PipelineConfig with sensible defaults.
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		ChannelBufferSize:       DefaultChannelBufferSize,
		MaxConcurrentPipelines:  DefaultMaxConcurrentPipelines,
		ExecutionTimeout:        DefaultExecutionTimeoutSeconds * time.Second,
		IdleTimeout:             DefaultIdleTimeoutSeconds * time.Second,
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
	if c.IdleTimeout < 0 {
		return ErrInvalidIdleTimeout
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

// WithPrioritizeElements sets whether stages read their input in priority order.
func (c *PipelineConfig) WithPrioritizeElements(prioritize bool) *PipelineConfig {
	c.PrioritizeElements = prioritize
	return c
}

// WithExecutionTimeout sets the execution timeout.
func (c *PipelineConfig) WithExecutionTimeout(timeout time.Duration) *PipelineConfig {
	c.ExecutionTimeout = timeout
	return c
}

// WithIdleTimeout sets the idle timeout.
func (c *PipelineConfig) WithIdleTimeout(timeout time.Duration) *PipelineConfig {
	c.IdleTimeout = timeout
	return c
}

// WithGracefulShutdownTimeout sets the graceful shutdown timeout.
func (c *PipelineConfig) WithGracefulShutdownTimeout(timeout time.Duration) *PipelineConfig {
	c.GracefulShutdownTimeout = timeout
	return c
}
