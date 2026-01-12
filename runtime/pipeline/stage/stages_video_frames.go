// Package stage provides pipeline stages for media processing.
package stage

import (
	"context"
	"encoding/base64"
	"fmt"
	_ "image/jpeg" // Register JPEG decoder
	_ "image/png"  // Register PNG decoder
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Metadata keys for video-to-frames correlation.
const (
	// VideoFramesVideoIDKey uniquely identifies the source video.
	VideoFramesVideoIDKey = "video_frames_video_id"

	// VideoFramesFrameIndexKey tracks the frame index within the video.
	VideoFramesFrameIndexKey = "video_frames_frame_index"

	// VideoFramesTotalFramesKey tracks expected total frames.
	VideoFramesTotalFramesKey = "video_frames_total_frames"

	// VideoFramesTimestampKey tracks the frame's timestamp in the original video.
	VideoFramesTimestampKey = "video_frames_timestamp"

	// VideoFramesOriginalVideoKey stores reference to original VideoData.
	VideoFramesOriginalVideoKey = "video_frames_original_video"
)

// Default configuration values.
const (
	// DefaultFrameInterval is the default time between extracted frames.
	DefaultFrameInterval = time.Second

	// DefaultTargetFPS is the default target frame rate.
	DefaultTargetFPS = 1.0

	// DefaultMaxFrames is the default maximum frames to extract.
	DefaultMaxFrames = 30

	// DefaultOutputFormat is the default output image format.
	DefaultOutputFormat = "jpeg"

	// OutputFormatPNG is the PNG output format.
	OutputFormatPNG = "png"

	// DefaultOutputQuality is the default JPEG quality.
	DefaultOutputQuality = 85

	// DefaultFFmpegPath is the default path to FFmpeg.
	DefaultFFmpegPath = "ffmpeg"

	// DefaultFFmpegTimeout is the default FFmpeg execution timeout.
	DefaultFFmpegTimeout = 5 * time.Minute

	// DefaultFramesCompletionTimeout is the default timeout for frame accumulation.
	DefaultFramesCompletionTimeout = 30 * time.Second

	// DefaultMaxFramesPerMessage is the default max frames in composed message.
	DefaultMaxFramesPerMessage = 30

	// filePermission is the permission mode for temp files.
	filePermission = 0600

	// ffmpegQScaleMin is the minimum quality scale for FFmpeg (best quality).
	ffmpegQScaleMin = 2

	// ffmpegQScaleMax is the maximum quality scale for FFmpeg (worst quality).
	ffmpegQScaleMax = 31

	// qualityScaleRange is the range for quality mapping (1-100 to qscale).
	qualityScaleRange = 99
)

// FrameExtractionMode defines how frames are selected from video.
type FrameExtractionMode int

const (
	// FrameExtractionInterval extracts frames at fixed time intervals.
	FrameExtractionInterval FrameExtractionMode = iota

	// FrameExtractionKeyframes extracts only keyframes (I-frames).
	FrameExtractionKeyframes

	// FrameExtractionFPS extracts at a specific frame rate.
	FrameExtractionFPS
)

// String returns the string representation of the extraction mode.
func (m FrameExtractionMode) String() string {
	switch m {
	case FrameExtractionInterval:
		return "interval"
	case FrameExtractionKeyframes:
		return "keyframes"
	case FrameExtractionFPS:
		return "fps"
	default:
		return unknownType
	}
}

// VideoToFramesConfig configures the VideoToFramesStage behavior.
type VideoToFramesConfig struct {
	// Mode determines how frames are extracted.
	// Default: FrameExtractionInterval.
	Mode FrameExtractionMode

	// Interval is the time between extracted frames (for FrameExtractionInterval mode).
	// Default: 1 second.
	Interval time.Duration

	// TargetFPS is the target frame rate (for FrameExtractionFPS mode).
	// Default: 1.0 (1 frame per second).
	TargetFPS float64

	// MaxFrames limits the maximum number of frames to extract.
	// 0 means unlimited.
	// Default: 30.
	MaxFrames int

	// OutputFormat specifies the output image format.
	// Default: "jpeg".
	OutputFormat string // "jpeg" or "png"

	// OutputQuality specifies JPEG quality (1-100).
	// Default: 85.
	OutputQuality int

	// OutputWidth resizes frames to this width (0 = original).
	// Height is calculated to maintain aspect ratio.
	// Default: 0 (original).
	OutputWidth int

	// FFmpegPath is the path to the ffmpeg binary.
	// Default: "ffmpeg".
	FFmpegPath string

	// FFmpegTimeout is the maximum time for FFmpeg execution per video.
	// Default: 5 minutes.
	FFmpegTimeout time.Duration

	// StorageService for loading externalized video data (optional).
	StorageService storage.MediaStorageService
}

// DefaultVideoToFramesConfig returns sensible defaults for frame extraction.
func DefaultVideoToFramesConfig() VideoToFramesConfig {
	return VideoToFramesConfig{
		Mode:          FrameExtractionInterval,
		Interval:      DefaultFrameInterval,
		TargetFPS:     DefaultTargetFPS,
		MaxFrames:     DefaultMaxFrames,
		OutputFormat:  DefaultOutputFormat,
		OutputQuality: DefaultOutputQuality,
		OutputWidth:   0,
		FFmpegPath:    DefaultFFmpegPath,
		FFmpegTimeout: DefaultFFmpegTimeout,
	}
}

// VideoToFramesStage extracts frames from video StreamElements into individual image StreamElements.
// This is a Transform stage with fan-out behavior (1 video → N images).
//
// Input: StreamElement with Video
// Output: Multiple StreamElements with Image, preserving correlation metadata
//
// Non-video elements are passed through unchanged.
type VideoToFramesStage struct {
	BaseStage
	config  VideoToFramesConfig
	videoID int64 // atomic counter for unique video IDs
}

// NewVideoToFramesStage creates a new video-to-frames extraction stage.
//
//nolint:gocritic // hugeParam: config is intentionally passed by value for API consistency
func NewVideoToFramesStage(config VideoToFramesConfig) *VideoToFramesStage {
	return &VideoToFramesStage{
		BaseStage: NewBaseStage("video-to-frames", StageTypeTransform),
		config:    config,
	}
}

// Process implements the Stage interface.
// Extracts frames from videos and emits individual image elements for each frame.
func (s *VideoToFramesStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Only process video elements
		if elem.Video == nil {
			// Pass through non-video elements
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Extract frames from video
		if err := s.extractFrames(ctx, &elem, output); err != nil {
			logger.Error("Failed to extract frames from video", "error", err)
			elem.Error = err
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// generateVideoID creates a unique identifier for video correlation.
// buildFFmpegArgs constructs FFmpeg command arguments.
func (s *VideoToFramesStage) buildFFmpegArgs(inputPath, outputPattern string) []string {
	args := []string{
		"-y",            // Overwrite output
		"-i", inputPath, // Input file
	}

	// Build video filter based on mode
	var filters []string

	switch s.config.Mode {
	case FrameExtractionInterval:
		fps := 1.0 / s.config.Interval.Seconds()
		filters = append(filters, fmt.Sprintf("fps=%.4f", fps))

	case FrameExtractionKeyframes:
		filters = append(filters, "select='eq(pict_type,I)'")
		args = append(args, "-vsync", "vfr")

	case FrameExtractionFPS:
		filters = append(filters, fmt.Sprintf("fps=%.4f", s.config.TargetFPS))
	}

	// Add scaling if requested
	if s.config.OutputWidth > 0 {
		filters = append(filters, fmt.Sprintf("scale=%d:-1", s.config.OutputWidth))
	}

	// Add video filter
	if len(filters) > 0 {
		args = append(args, "-vf", ffmpegJoinFilters(filters))
	}

	// Add frame limit
	if s.config.MaxFrames > 0 {
		args = append(args, "-vframes", fmt.Sprintf("%d", s.config.MaxFrames))
	}

	// Add quality setting
	if s.config.OutputFormat == DefaultOutputFormat {
		// FFmpeg quality scale: 2-31 (2=best, 31=worst)
		// Map quality 1-100 to 31-2
		qScaleRange := ffmpegQScaleMax - ffmpegQScaleMin
		qScale := ffmpegQScaleMax - int(float64(s.config.OutputQuality-1)*float64(qScaleRange)/qualityScaleRange)
		qScale = max(qScale, ffmpegQScaleMin)
		qScale = min(qScale, ffmpegQScaleMax)
		args = append(args, "-q:v", fmt.Sprintf("%d", qScale))
	}

	// Output pattern
	args = append(args, outputPattern)

	return args
}

// ffmpegJoinFilters joins FFmpeg video filters with comma separator.
func ffmpegJoinFilters(filters []string) string {
	if len(filters) == 0 {
		return ""
	}
	result := filters[0]
	for i := 1; i < len(filters); i++ {
		result += "," + filters[i]
	}
	return result
}

// generateVideoID creates a unique identifier for video correlation.
func (s *VideoToFramesStage) generateVideoID() string {
	id := atomic.AddInt64(&s.videoID, 1)
	return fmt.Sprintf("video-%d-%d", time.Now().UnixNano(), id)
}

// GetConfig returns the stage configuration.
func (s *VideoToFramesStage) GetConfig() VideoToFramesConfig {
	return s.config
}

// FrameSelectionStrategy defines how frames are selected when limiting.
type FrameSelectionStrategy int

const (
	// FrameSelectionUniform selects frames uniformly distributed across the video.
	FrameSelectionUniform FrameSelectionStrategy = iota

	// FrameSelectionFirst selects the first N frames.
	FrameSelectionFirst

	// FrameSelectionLast selects the last N frames.
	FrameSelectionLast
)

// String returns the string representation of the selection strategy.
func (s FrameSelectionStrategy) String() string {
	switch s {
	case FrameSelectionUniform:
		return "uniform"
	case FrameSelectionFirst:
		return "first"
	case FrameSelectionLast:
		return "last"
	default:
		return unknownType
	}
}

// FramesToMessageConfig configures the FramesToMessageStage behavior.
type FramesToMessageConfig struct {
	// CompletionTimeout is how long to wait for all frames of a video.
	// If timeout is reached, compose with available frames.
	// Default: 30s.
	CompletionTimeout time.Duration

	// MaxFramesPerMessage limits frames included in the composed message.
	// 0 means unlimited.
	// Default: 30.
	MaxFramesPerMessage int

	// FrameSelectionStrategy determines which frames to include when limiting.
	// Default: FrameSelectionUniform.
	FrameSelectionStrategy FrameSelectionStrategy

	// StorageService for externalizing composed images (optional).
	StorageService storage.MediaStorageService
}

// DefaultFramesToMessageConfig returns sensible defaults for frame composition.
func DefaultFramesToMessageConfig() FramesToMessageConfig {
	return FramesToMessageConfig{
		CompletionTimeout:      DefaultFramesCompletionTimeout,
		MaxFramesPerMessage:    DefaultMaxFramesPerMessage,
		FrameSelectionStrategy: FrameSelectionUniform,
	}
}

// pendingFrames tracks accumulated frames for a single video.
type pendingFrames struct {
	frames        map[int]*ImageData // frame_index -> image data
	totalFrames   int                // expected total
	originalVideo *VideoData         // reference to source video
	receivedAt    time.Time
	sequence      int64
	source        string
}

// FramesToMessageStage collects extracted frames and composes them into Messages.
// Elements are correlated by video ID from VideoToFramesStage metadata.
//
// Input: StreamElements with Image and video_frames metadata
// Output: StreamElement with Message containing composed image Parts[]
//
// Non-frame elements (those without video_frames metadata) are passed through unchanged.
//
// This is an Accumulate stage (N:1 fan-in pattern).
type FramesToMessageStage struct {
	BaseStage
	config  FramesToMessageConfig
	pending map[string]*pendingFrames // video_id -> pending
	mu      sync.Mutex
}

// NewFramesToMessageStage creates a new frame composition stage.
func NewFramesToMessageStage(config FramesToMessageConfig) *FramesToMessageStage {
	return &FramesToMessageStage{
		BaseStage: NewBaseStage("frames-to-message", StageTypeAccumulate),
		config:    config,
		pending:   make(map[string]*pendingFrames),
	}
}

// Process implements the Stage interface.
// Collects frames and composes them into messages.
//
//nolint:dupl,gocognit // Similar pattern to MediaComposeStage; complex but well-structured
func (s *FramesToMessageStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	// Start timeout checker
	timeoutDone := make(chan struct{})
	timeoutExited := make(chan struct{})
	go s.checkTimeouts(ctx, output, timeoutDone, timeoutExited)

	// Ensure we wait for checkTimeouts to exit before closing output
	defer func() {
		close(timeoutDone)
		<-timeoutExited
		close(output)
	}()

	for elem := range input {
		// Check if this is a frame element with video_frames metadata
		videoID := elem.GetMetadata(VideoFramesVideoIDKey)
		if videoID == nil {
			// Pass through non-frame elements
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Accumulate the frame
		complete, err := s.accumulateFrame(&elem)
		if err != nil {
			logger.Error("Failed to accumulate frame", "error", err)
			elem.Error = err
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// If all frames received, compose and emit
		if complete != nil {
			composed, err := s.composeMessage(complete)
			if err != nil {
				logger.Error("Failed to compose frames message", "error", err)
				errElem := NewErrorElement(err)
				errElem.Sequence = complete.sequence
				errElem.Source = complete.source
				select {
				case output <- errElem:
				case <-ctx.Done():
					return ctx.Err()
				}
				continue
			}

			outElem := NewMessageElement(composed)
			outElem.Sequence = complete.sequence
			outElem.Source = complete.source

			select {
			case output <- outElem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	// Process any remaining pending frames
	s.mu.Lock()
	remaining := make([]*pendingFrames, 0, len(s.pending))
	for _, pf := range s.pending {
		remaining = append(remaining, pf)
	}
	s.pending = make(map[string]*pendingFrames)
	s.mu.Unlock()

	for _, pf := range remaining {
		composed, err := s.composeMessage(pf)
		if err != nil {
			logger.Warn("Failed to compose incomplete frames", "error", err)
			continue
		}

		outElem := NewMessageElement(composed)
		outElem.Sequence = pf.sequence
		outElem.Source = pf.source

		select {
		case output <- outElem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// accumulateFrame adds a frame to its pending video.
// Returns the complete pendingFrames if all frames have been received.
func (s *FramesToMessageStage) accumulateFrame(elem *StreamElement) (*pendingFrames, error) {
	videoID, ok := elem.GetMetadata(VideoFramesVideoIDKey).(string)
	if !ok {
		return nil, fmt.Errorf("invalid video ID type")
	}

	frameIdx, ok := elem.GetMetadata(VideoFramesFrameIndexKey).(int)
	if !ok {
		return nil, fmt.Errorf("invalid frame index type")
	}

	totalFrames, ok := elem.GetMetadata(VideoFramesTotalFramesKey).(int)
	if !ok {
		return nil, fmt.Errorf("invalid total frames type")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Get or create pending frames
	pf, exists := s.pending[videoID]
	if !exists {
		origVideo, _ := elem.GetMetadata(VideoFramesOriginalVideoKey).(*VideoData)
		pf = &pendingFrames{
			frames:        make(map[int]*ImageData),
			totalFrames:   totalFrames,
			originalVideo: origVideo,
			receivedAt:    time.Now(),
			sequence:      elem.Sequence,
			source:        elem.Source,
		}
		s.pending[videoID] = pf
	}

	// Add the frame
	if elem.Image != nil {
		pf.frames[frameIdx] = elem.Image
	}

	// Check if complete
	if len(pf.frames) >= pf.totalFrames {
		delete(s.pending, videoID)
		return pf, nil
	}

	return nil, nil
}

// composeMessage builds a Message from accumulated frames.
func (s *FramesToMessageStage) composeMessage(pf *pendingFrames) (*types.Message, error) {
	// Select frames based on strategy
	frames := s.selectFrames(pf.frames)

	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames to compose")
	}

	// Create message
	msg := &types.Message{
		Role:      "user",
		Timestamp: time.Now(),
	}

	// Add each frame as an image part
	for _, img := range frames {
		part, err := s.createImagePart(img)
		if err != nil {
			logger.Warn("Failed to create image part", "error", err)
			continue
		}
		msg.Parts = append(msg.Parts, part)
	}

	return msg, nil
}

// selectFrames applies the selection strategy when limiting frames.
func (s *FramesToMessageStage) selectFrames(frames map[int]*ImageData) []*ImageData {
	// Get sorted frame indices
	indices := make([]int, 0, len(frames))
	for idx := range frames {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	// If no limit or under limit, return all frames in order
	maxFrames := s.config.MaxFramesPerMessage
	if maxFrames <= 0 || len(indices) <= maxFrames {
		result := make([]*ImageData, 0, len(indices))
		for _, idx := range indices {
			result = append(result, frames[idx])
		}
		return result
	}

	// Apply selection strategy
	var selectedIndices []int

	switch s.config.FrameSelectionStrategy {
	case FrameSelectionFirst:
		selectedIndices = indices[:maxFrames]

	case FrameSelectionLast:
		selectedIndices = indices[len(indices)-maxFrames:]

	case FrameSelectionUniform:
		// Uniform distribution
		step := float64(len(indices)-1) / float64(maxFrames-1)
		for i := 0; i < maxFrames; i++ {
			idx := int(float64(i) * step)
			if idx >= len(indices) {
				idx = len(indices) - 1
			}
			selectedIndices = append(selectedIndices, indices[idx])
		}
	}

	// Build result
	result := make([]*ImageData, 0, len(selectedIndices))
	for _, idx := range selectedIndices {
		result = append(result, frames[idx])
	}
	return result
}

// createImagePart creates a ContentPart from ImageData.
func (s *FramesToMessageStage) createImagePart(img *ImageData) (types.ContentPart, error) {
	if img == nil {
		return types.ContentPart{}, fmt.Errorf("image data is nil")
	}

	media := &types.MediaContent{
		MIMEType: img.MIMEType,
	}

	// Handle data
	if img.IsExternalized() {
		ref := string(img.StorageRef)
		media.StorageReference = &ref
	} else if len(img.Data) > 0 {
		// Encode as base64
		encoded := encodeImageData(img.Data)
		media.Data = &encoded
	}

	// Set dimensions
	if img.Width > 0 {
		media.Width = &img.Width
	}
	if img.Height > 0 {
		media.Height = &img.Height
	}

	return types.ContentPart{
		Type:  types.ContentTypeImage,
		Media: media,
	}, nil
}

// encodeImageData encodes image data as base64.
func encodeImageData(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// checkTimeouts periodically checks for timed-out pending videos.
func (s *FramesToMessageStage) checkTimeouts(
	ctx context.Context,
	output chan<- StreamElement,
	done <-chan struct{},
	exited chan<- struct{},
) {
	defer close(exited)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processTimeouts(ctx, output)
		}
	}
}

// processTimeouts handles videos that have exceeded the completion timeout.
//
//nolint:dupl // Similar pattern to MediaComposeStage but different data structures
func (s *FramesToMessageStage) processTimeouts(ctx context.Context, output chan<- StreamElement) {
	s.mu.Lock()
	now := time.Now()
	var timedOut []*pendingFrames
	var timedOutIDs []string

	for id, pf := range s.pending {
		if now.Sub(pf.receivedAt) > s.config.CompletionTimeout {
			timedOut = append(timedOut, pf)
			timedOutIDs = append(timedOutIDs, id)
		}
	}

	for _, id := range timedOutIDs {
		delete(s.pending, id)
	}
	s.mu.Unlock()

	// Emit partial messages
	for _, pf := range timedOut {
		logger.Warn("Composing incomplete frames due to timeout",
			"received_frames", len(pf.frames),
			"expected_frames", pf.totalFrames,
		)

		composed, err := s.composeMessage(pf)
		if err != nil {
			logger.Error("Failed to compose timed-out frames", "error", err)
			continue
		}

		outElem := NewMessageElement(composed)
		outElem.Sequence = pf.sequence
		outElem.Source = pf.source

		select {
		case output <- outElem:
		case <-ctx.Done():
			return
		}
	}
}

// GetConfig returns the stage configuration.
func (s *FramesToMessageStage) GetConfig() FramesToMessageConfig {
	return s.config
}
