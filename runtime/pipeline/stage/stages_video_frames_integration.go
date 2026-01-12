// Package stage provides pipeline stages for media processing.
//
// This file contains FFmpeg-dependent integration code for video frame extraction.
// These functions require FFmpeg to be installed and cannot be unit tested without it.
package stage

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // Register JPEG decoder
	_ "image/png"  // Register PNG decoder
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// extractFrames extracts frames from a video element.
// This function requires FFmpeg to be installed.
func (s *VideoToFramesStage) extractFrames(
	ctx context.Context,
	elem *StreamElement,
	output chan<- StreamElement,
) error {
	video := elem.Video

	// Create temp directory for processing
	tempDir, err := os.MkdirTemp("", "video-frames-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(tempDir); removeErr != nil {
			logger.Warn("Failed to remove temp directory", "path", tempDir, "error", removeErr)
		}
	}()

	// Write video to temp file
	inputPath, err := s.writeVideoToTempFile(ctx, video, tempDir)
	if err != nil {
		return fmt.Errorf("failed to write video to temp file: %w", err)
	}

	// Build output pattern
	outputExt := "jpg"
	if s.config.OutputFormat == OutputFormatPNG {
		outputExt = OutputFormatPNG
	}
	outputPattern := filepath.Join(tempDir, fmt.Sprintf("frame_%%04d.%s", outputExt))

	// Build FFmpeg arguments
	args := s.buildFFmpegArgs(inputPath, outputPattern)

	// Run FFmpeg
	if runErr := s.runFFmpeg(ctx, args); runErr != nil {
		return runErr
	}

	// Generate unique video ID
	videoID := s.generateVideoID()

	// Read extracted frames and emit as StreamElements
	frameCount, err := s.readExtractedFrames(ctx, tempDir, videoID, elem, output)
	if err != nil {
		return err
	}

	if frameCount == 0 {
		return ErrNoFramesExtracted
	}

	return nil
}

// writeVideoToTempFile writes video data to a temporary file for FFmpeg.
func (s *VideoToFramesStage) writeVideoToTempFile(
	ctx context.Context,
	video *VideoData,
	tempDir string,
) (string, error) {
	// Determine file extension from MIME type
	ext := ".mp4"
	switch video.MIMEType {
	case "video/webm":
		ext = ".webm"
	case "video/quicktime":
		ext = ".mov"
	case "video/x-msvideo":
		ext = ".avi"
	case "video/x-matroska":
		ext = ".mkv"
	}

	inputPath := filepath.Join(tempDir, "input"+ext)

	// Get video data
	var data []byte
	if video.IsExternalized() {
		if s.config.StorageService == nil {
			return "", ErrVideoDataRequired
		}
		loadedData, err := video.EnsureLoaded(ctx, s.config.StorageService)
		if err != nil {
			return "", fmt.Errorf("failed to load externalized video: %w", err)
		}
		data = loadedData
	} else {
		data = video.Data
	}

	if len(data) == 0 {
		return "", ErrVideoDataRequired
	}

	// Write to file
	if err := os.WriteFile(inputPath, data, filePermission); err != nil {
		return "", fmt.Errorf("failed to write video file: %w", err)
	}

	return inputPath, nil
}

// runFFmpeg executes FFmpeg with timeout.
func (s *VideoToFramesStage) runFFmpeg(ctx context.Context, args []string) error {
	// Create context with timeout
	ffmpegCtx, cancel := context.WithTimeout(ctx, s.config.FFmpegTimeout)
	defer cancel()

	//nolint:gosec // G204: FFmpegPath is configurable but expected to be ffmpeg binary
	cmd := exec.CommandContext(ffmpegCtx, s.config.FFmpegPath, args...)

	// Capture stderr for error messages
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Run FFmpeg
	if err := cmd.Run(); err != nil {
		// Check if it was a timeout
		if ffmpegCtx.Err() == context.DeadlineExceeded {
			return ErrFFmpegTimeout
		}

		// Check if FFmpeg was not found
		if execErr, ok := err.(*exec.Error); ok && execErr.Err == exec.ErrNotFound {
			return ErrFFmpegNotFound
		}

		return fmt.Errorf("%w: %s", ErrFFmpegFailed, stderr.String())
	}

	return nil
}

// readExtractedFrames reads output frames and creates StreamElements.
//
//nolint:gocognit // Complex but well-structured frame processing logic
func (s *VideoToFramesStage) readExtractedFrames(
	ctx context.Context,
	tempDir string,
	videoID string,
	elem *StreamElement,
	output chan<- StreamElement,
) (int, error) {
	// List output files
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read temp directory: %w", err)
	}

	// Filter and sort frame files
	var frameFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "frame_") && (strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".png")) {
			frameFiles = append(frameFiles, name)
		}
	}

	// Sort by name to ensure correct order
	sort.Strings(frameFiles)

	totalFrames := len(frameFiles)
	if totalFrames == 0 {
		return 0, nil
	}

	// Emit each frame as an ImageData element
	for i, fileName := range frameFiles {
		framePath := filepath.Join(tempDir, fileName)

		// Read frame data
		//nolint:gosec // G304: framePath is constructed from temp directory and known pattern
		data, err := os.ReadFile(framePath)
		if err != nil {
			logger.Warn("Failed to read frame file", "path", framePath, "error", err)
			continue
		}

		// Decode to get dimensions
		var width, height int
		reader := bytes.NewReader(data)
		img, _, err := image.Decode(reader)
		if err == nil {
			bounds := img.Bounds()
			width = bounds.Dx()
			height = bounds.Dy()
		}

		// Determine MIME type
		mimeType := "image/jpeg"
		format := "jpeg"
		if strings.HasSuffix(fileName, ".png") {
			mimeType = "image/png"
			format = "png"
		}

		// Create ImageData
		imageData := &ImageData{
			Data:     data,
			MIMEType: mimeType,
			Width:    width,
			Height:   height,
			Format:   format,
		}

		// Create StreamElement
		frameElem := NewImageElement(imageData)
		frameElem.Sequence = elem.Sequence
		frameElem.Source = elem.Source

		// Add correlation metadata
		frameElem.WithMetadata(VideoFramesVideoIDKey, videoID)
		frameElem.WithMetadata(VideoFramesFrameIndexKey, i)
		frameElem.WithMetadata(VideoFramesTotalFramesKey, totalFrames)
		frameElem.WithMetadata(VideoFramesOriginalVideoKey, elem.Video)

		// Copy MediaExtract metadata if present
		if msgID := elem.GetMetadata(MediaExtractMessageIDKey); msgID != nil {
			frameElem.WithMetadata(MediaExtractMessageIDKey, msgID)
		}
		if partIdx := elem.GetMetadata(MediaExtractPartIndexKey); partIdx != nil {
			frameElem.WithMetadata(MediaExtractPartIndexKey, partIdx)
		}

		// Emit frame
		select {
		case output <- frameElem:
		case <-ctx.Done():
			return i, ctx.Err()
		}
	}

	return totalFrames, nil
}
