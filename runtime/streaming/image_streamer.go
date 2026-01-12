package streaming

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// Default image streaming configuration constants.
const (
	// DefaultTargetFPS is the default target frame rate for image streaming.
	// 1 FPS is suitable for most LLM vision scenarios.
	DefaultTargetFPS = 1.0

	// DefaultImageQuality is the default JPEG quality (1-100).
	DefaultImageQuality = 85
)

// ImageStreamer provides utilities for streaming image frames through a pipeline.
// Use this for realtime video scenarios like webcam feeds or screen sharing.
type ImageStreamer struct {
	// TargetFPS is the target frame rate for realtime streaming.
	// Default: 1.0 (1 frame per second).
	TargetFPS float64
}

// NewImageStreamer creates a new image streamer with the specified target FPS.
// Use targetFPS of 0 or less for default (1.0 FPS).
func NewImageStreamer(targetFPS float64) *ImageStreamer {
	if targetFPS <= 0 {
		targetFPS = DefaultTargetFPS
	}
	return &ImageStreamer{
		TargetFPS: targetFPS,
	}
}

// SendFrame sends a single image frame through the pipeline without pacing.
// This is the burst mode equivalent - sends immediately without delay.
//
// Parameters:
//   - data: Raw image data (JPEG, PNG, etc.)
//   - mimeType: MIME type of the image (e.g., "image/jpeg")
//   - frameNum: Sequence number for ordering
//   - timestamp: When the frame was captured
//   - output: Pipeline input channel
func (s *ImageStreamer) SendFrame(
	ctx context.Context,
	data []byte,
	mimeType string,
	frameNum int64,
	timestamp time.Time,
	output chan<- stage.StreamElement,
) error {
	elem := stage.StreamElement{
		Image: &stage.ImageData{
			Data:      data,
			MIMEType:  mimeType,
			FrameNum:  frameNum,
			Timestamp: timestamp,
		},
		Priority: stage.PriorityHigh,
		Metadata: map[string]any{
			"passthrough": true,
		},
	}

	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendFrameWithDimensions sends a frame with explicit width and height.
// Use this when dimensions are known to avoid decoding overhead downstream.
func (s *ImageStreamer) SendFrameWithDimensions(
	ctx context.Context,
	data []byte,
	mimeType string,
	width, height int,
	frameNum int64,
	timestamp time.Time,
	output chan<- stage.StreamElement,
) error {
	elem := stage.StreamElement{
		Image: &stage.ImageData{
			Data:      data,
			MIMEType:  mimeType,
			Width:     width,
			Height:    height,
			FrameNum:  frameNum,
			Timestamp: timestamp,
		},
		Priority: stage.PriorityHigh,
		Metadata: map[string]any{
			"passthrough": true,
		},
	}

	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StreamFramesBurst sends all frames as fast as possible without pacing.
// Use this for pre-recorded frame sequences where real-time pacing isn't needed.
func (s *ImageStreamer) StreamFramesBurst(
	ctx context.Context,
	frames [][]byte,
	mimeType string,
	output chan<- stage.StreamElement,
) error {
	totalFrames := len(frames)
	if totalFrames == 0 {
		return nil
	}

	logger.Debug("Streaming frames in BURST MODE",
		"total_frames", totalFrames,
		"mime_type", mimeType,
	)

	streamStart := time.Now()
	for i, frameData := range frames {
		if err := s.SendFrame(ctx, frameData, mimeType, int64(i), time.Now(), output); err != nil {
			return err
		}

		s.logProgress(i, totalFrames, streamStart, len(frameData))
	}

	return nil
}

// StreamFramesRealtime sends frames paced to match the target FPS.
// Use this for simulating real-time playback of pre-recorded frames.
func (s *ImageStreamer) StreamFramesRealtime(
	ctx context.Context,
	frames [][]byte,
	mimeType string,
	output chan<- stage.StreamElement,
) error {
	totalFrames := len(frames)
	if totalFrames == 0 {
		return nil
	}

	frameInterval := s.getFrameInterval()

	logger.Debug("Streaming frames in REALTIME MODE",
		"total_frames", totalFrames,
		"mime_type", mimeType,
		"target_fps", s.getTargetFPS(),
		"frame_interval_ms", frameInterval.Milliseconds(),
	)

	streamStart := time.Now()
	for i, frameData := range frames {
		if err := s.SendFrame(ctx, frameData, mimeType, int64(i), time.Now(), output); err != nil {
			return err
		}

		s.logProgress(i, totalFrames, streamStart, len(frameData))

		// Pace frames to match target FPS
		if i < totalFrames-1 {
			select {
			case <-time.After(frameInterval):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// SendImageEndOfStream signals that image/frame input is complete for the current turn.
// This triggers the provider to generate a response.
func SendImageEndOfStream(
	ctx context.Context,
	output chan<- stage.StreamElement,
) error {
	logger.Debug("Sending image EndOfStream signal to trigger response")
	elem := stage.StreamElement{
		EndOfStream: true,
		Metadata: map[string]any{
			"media_type": "image",
		},
	}
	select {
	case output <- elem:
		logger.Debug("Image EndOfStream signal sent")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// getTargetFPS returns the configured target FPS or default.
func (s *ImageStreamer) getTargetFPS() float64 {
	if s.TargetFPS <= 0 {
		return DefaultTargetFPS
	}
	return s.TargetFPS
}

// getFrameInterval returns the duration between frames based on target FPS.
func (s *ImageStreamer) getFrameInterval() time.Duration {
	fps := s.getTargetFPS()
	return time.Duration(float64(time.Second) / fps)
}

// logProgress logs progress for first, middle, and last frames.
func (s *ImageStreamer) logProgress(frameIdx, totalFrames int, streamStart time.Time, frameBytes int) {
	if frameIdx == 0 || frameIdx == totalFrames/2 || frameIdx == totalFrames-1 {
		logger.Debug("Image frame sent",
			"frame_idx", frameIdx,
			"total_frames", totalFrames,
			"elapsed_ms", time.Since(streamStart).Milliseconds(),
			"frame_bytes", frameBytes,
		)
	}
}
