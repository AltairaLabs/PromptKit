// Package stage provides pipeline stages for media processing.
package stage

import (
	"context"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// Default frame rate limit configuration constants.
const (
	// DefaultFrameRateLimitFPS is the default target frame rate.
	// 1 FPS is suitable for most LLM vision scenarios.
	DefaultFrameRateLimitFPS = 1.0

	// String constants for drop strategies.
	dropStrategyUniformStr = "uniform"
	dropStrategyUnknownStr = "unknown"
	dropStrategyLatestStr  = "keep_latest"
)

// DropStrategy defines how frames are dropped when rate limiting.
type DropStrategy int

const (
	// DropStrategyKeepLatest keeps the most recent frame and drops older ones.
	// This ensures the model sees the most current state.
	DropStrategyKeepLatest DropStrategy = iota

	// DropStrategyUniform attempts to keep frames uniformly distributed.
	// This provides a more representative sampling across time.
	DropStrategyUniform
)

// String returns the string representation of the drop strategy.
func (s DropStrategy) String() string {
	switch s {
	case DropStrategyKeepLatest:
		return dropStrategyLatestStr
	case DropStrategyUniform:
		return dropStrategyUniformStr
	default:
		return dropStrategyUnknownStr
	}
}

// FrameRateLimitConfig configures the FrameRateLimitStage behavior.
type FrameRateLimitConfig struct {
	// TargetFPS is the target output frame rate.
	// Frames exceeding this rate will be dropped.
	// Default: 1.0 FPS.
	TargetFPS float64

	// DropStrategy determines which frames to drop when rate limiting.
	// Default: DropStrategyKeepLatest.
	DropStrategy DropStrategy

	// PassthroughAudio allows audio elements to bypass rate limiting.
	// This is important for maintaining audio quality in mixed streams.
	// Default: true.
	PassthroughAudio bool

	// PassthroughNonMedia allows non-media elements (text, messages, etc.)
	// to bypass rate limiting.
	// Default: true.
	PassthroughNonMedia bool
}

// DefaultFrameRateLimitConfig returns sensible defaults for frame rate limiting.
func DefaultFrameRateLimitConfig() FrameRateLimitConfig {
	return FrameRateLimitConfig{
		TargetFPS:           DefaultFrameRateLimitFPS,
		DropStrategy:        DropStrategyKeepLatest,
		PassthroughAudio:    true,
		PassthroughNonMedia: true,
	}
}

// FrameRateLimitStage drops frames to maintain a target frame rate.
// This is useful for high-FPS video feeds (e.g., 30fps webcam) that need
// to be reduced to a rate suitable for LLM processing (e.g., 1fps).
//
// This is a Transform stage that may drop elements (N:M where M <= N).
type FrameRateLimitStage struct {
	BaseStage
	config FrameRateLimitConfig

	// Timing state
	mu             sync.Mutex // Protects all mutable state below
	lastEmitTime   time.Time
	frameInterval  time.Duration
	droppedFrames  int64
	emittedFrames  int64
	loggedDropOnce bool
}

// NewFrameRateLimitStage creates a new frame rate limiting stage.
func NewFrameRateLimitStage(config FrameRateLimitConfig) *FrameRateLimitStage {
	fps := config.TargetFPS
	if fps <= 0 {
		fps = DefaultFrameRateLimitFPS
	}

	return &FrameRateLimitStage{
		BaseStage:     NewBaseStage("frame-rate-limit", StageTypeTransform),
		config:        config,
		frameInterval: time.Duration(float64(time.Second) / fps),
	}
}

// Process implements the Stage interface.
// Drops video/image frames to maintain the target frame rate.
func (s *FrameRateLimitStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		shouldEmit := s.shouldEmitElement(&elem)

		if shouldEmit {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	// Log final statistics
	if s.droppedFrames > 0 || s.emittedFrames > 0 {
		logger.Debug("FrameRateLimitStage: stream completed",
			"emitted_frames", s.emittedFrames,
			"dropped_frames", s.droppedFrames,
			"target_fps", s.config.TargetFPS,
		)
	}

	return nil
}

// shouldEmitElement determines if an element should be emitted or dropped.
func (s *FrameRateLimitStage) shouldEmitElement(elem *StreamElement) bool {
	// Always passthrough control signals
	if elem.EndOfStream || elem.Error != nil {
		return true
	}

	// Check passthrough for audio
	if s.config.PassthroughAudio && elem.Audio != nil {
		return true
	}

	// Check passthrough for non-media elements
	if s.config.PassthroughNonMedia {
		if elem.Text != nil || elem.Message != nil || elem.ToolCall != nil || elem.Part != nil {
			return true
		}
	}

	// Only rate-limit video and image elements
	isVideoOrImage := (elem.Video != nil && len(elem.Video.Data) > 0) ||
		(elem.Image != nil && len(elem.Image.Data) > 0)

	if !isVideoOrImage {
		// Not a video/image element, passthrough
		return true
	}

	// Apply rate limiting based on strategy
	return s.applyRateLimit()
}

// applyRateLimit applies the rate limiting logic to a video/image element.
func (s *FrameRateLimitStage) applyRateLimit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// First frame always passes
	if s.lastEmitTime.IsZero() {
		s.lastEmitTime = now
		s.emittedFrames++
		return true
	}

	// Check if enough time has passed since last emit
	elapsed := now.Sub(s.lastEmitTime)

	if elapsed >= s.frameInterval {
		// Enough time has passed, emit this frame
		s.lastEmitTime = now
		s.emittedFrames++
		return true
	}

	// Drop this frame
	s.droppedFrames++

	// Log drop once to avoid spam
	if !s.loggedDropOnce {
		logger.Debug("FrameRateLimitStage: dropping frames to maintain target FPS",
			"target_fps", s.config.TargetFPS,
			"frame_interval_ms", s.frameInterval.Milliseconds(),
		)
		s.loggedDropOnce = true
	}

	return false
}

// GetConfig returns the stage configuration.
func (s *FrameRateLimitStage) GetConfig() FrameRateLimitConfig {
	return s.config
}

// GetStats returns the current frame statistics.
func (s *FrameRateLimitStage) GetStats() (emitted, dropped int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emittedFrames, s.droppedFrames
}
