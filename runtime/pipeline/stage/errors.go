package stage

import (
	"errors"
	"fmt"
)

// Common errors
var (
	// ErrPipelineShuttingDown is returned when attempting to execute a pipeline that is shutting down.
	ErrPipelineShuttingDown = errors.New("pipeline is shutting down")

	// ErrShutdownTimeout is returned when pipeline shutdown times out.
	ErrShutdownTimeout = errors.New("shutdown timeout exceeded")

	// ErrInvalidPipeline is returned when building an invalid pipeline.
	ErrInvalidPipeline = errors.New("invalid pipeline configuration")

	// ErrCyclicDependency is returned when the pipeline DAG contains cycles.
	ErrCyclicDependency = errors.New("cyclic dependency detected in pipeline")

	// ErrStageNotFound is returned when a referenced stage doesn't exist.
	ErrStageNotFound = errors.New("stage not found")

	// ErrDuplicateStageName is returned when multiple stages have the same name.
	ErrDuplicateStageName = errors.New("duplicate stage name")

	// ErrNoStages is returned when trying to build a pipeline with no stages.
	ErrNoStages = errors.New("pipeline must have at least one stage")

	// ErrInvalidChannelBufferSize is returned for invalid buffer size.
	ErrInvalidChannelBufferSize = errors.New("channel buffer size must be non-negative")

	// ErrInvalidMaxConcurrentPipelines is returned for invalid max concurrent pipelines.
	ErrInvalidMaxConcurrentPipelines = errors.New("max concurrent pipelines must be non-negative")

	// ErrInvalidExecutionTimeout is returned for invalid execution timeout.
	ErrInvalidExecutionTimeout = errors.New("execution timeout must be non-negative")

	// ErrInvalidGracefulShutdownTimeout is returned for invalid graceful shutdown timeout.
	ErrInvalidGracefulShutdownTimeout = errors.New("graceful shutdown timeout must be non-negative")

	// ErrInvalidIdleTimeout is returned for invalid idle timeout.
	ErrInvalidIdleTimeout = errors.New("idle timeout must be non-negative")

	// ErrFFmpegNotFound is returned when FFmpeg binary cannot be found.
	ErrFFmpegNotFound = errors.New("ffmpeg not found")

	// ErrFFmpegFailed is returned when FFmpeg execution fails.
	ErrFFmpegFailed = errors.New("ffmpeg execution failed")

	// ErrFFmpegTimeout is returned when FFmpeg execution times out.
	ErrFFmpegTimeout = errors.New("ffmpeg execution timeout")

	// ErrInvalidVideoFormat is returned when video cannot be processed.
	ErrInvalidVideoFormat = errors.New("invalid or unsupported video format")

	// ErrNoFramesExtracted is returned when FFmpeg produces no output frames.
	ErrNoFramesExtracted = errors.New("no frames extracted from video")

	// ErrVideoDataRequired is returned when video data is required but missing.
	ErrVideoDataRequired = errors.New("video data required but not available")

	// ErrFanInNotSupported is returned when a stage has multiple upstream edges but
	// does not implement MultiInputStage.
	ErrFanInNotSupported = errors.New("stage has multiple upstreams but does not implement MultiInputStage")

	// ErrDuplicateFanInEdge is returned when a stage receives multiple edges from
	// the same upstream stage, creating a duplicate input source.
	ErrDuplicateFanInEdge = errors.New("duplicate upstream edge detected")
)

// StageError wraps an error with stage information.
//
//nolint:revive // Intentionally named StageError for clarity; stage.Error would be too generic
type StageError struct {
	StageName string
	StageType StageType
	Err       error
}

// Error returns the error message.
func (e *StageError) Error() string {
	return fmt.Sprintf("stage '%s' (%s) failed: %v", e.StageName, e.StageType, e.Err)
}

// Unwrap returns the underlying error.
func (e *StageError) Unwrap() error {
	return e.Err
}

// NewStageError creates a new StageError.
func NewStageError(stageName string, stageType StageType, err error) *StageError {
	return &StageError{
		StageName: stageName,
		StageType: stageType,
		Err:       err,
	}
}

// ErrTemplateUnresolved is returned by TemplateStage when the system prompt
// references a variable nothing supplied.
//
// The turn fails rather than proceeding. The alternative (sending the
// unrendered template) puts literal {{...}} in front of the model, and since
// rendering aborts at the first missing name, every other variable in that
// prompt goes unrendered too. That reads in production as a model ignoring its
// instructions, which is a far longer trail back to a missing variable than an
// error naming it.
var ErrTemplateUnresolved = errors.New("system prompt has unresolved variables")

// ErrMaxRoundsExceeded matches, with errors.Is, a turn the provider stage
// ended because the model was still calling tools when the tool loop reached
// its round limit. Unlike a provider's own error it carries nothing secret,
// and it is recoverable: the model was working, and another message lets it
// continue. errors.As with *MaxRoundsError gives the limit.
var ErrMaxRoundsExceeded = errors.New("max rounds exceeded")

// MaxRoundsError is the error the provider stage returns when the tool loop
// reaches its round limit. It wraps ErrMaxRoundsExceeded.
type MaxRoundsError struct {
	// Rounds is the limit that was reached.
	Rounds int
}

// Error returns the error message.
func (e *MaxRoundsError) Error() string {
	return fmt.Sprintf("provider stage: max rounds (%d) exceeded", e.Rounds)
}

// Unwrap returns ErrMaxRoundsExceeded.
func (e *MaxRoundsError) Unwrap() error {
	return ErrMaxRoundsExceeded
}
