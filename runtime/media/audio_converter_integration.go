package media

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// FFmpeg error types.
var (
	ErrFFmpegNotFound = fmt.Errorf("ffmpeg not found in PATH")
	ErrFFmpegTimeout  = fmt.Errorf("ffmpeg execution timed out")
)

// convertWithFFmpeg performs audio conversion using ffmpeg.
func (c *AudioConverter) convertWithFFmpeg(
	ctx context.Context,
	data []byte,
	fromMIME, toMIME string,
) ([]byte, error) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "audio-convert-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(tempDir); removeErr != nil {
			logger.Warn("Failed to remove temp directory", "path", tempDir, "error", removeErr)
		}
	}()

	// Determine file extensions
	fromFormat := MIMETypeToAudioFormat(fromMIME)
	toFormat := MIMETypeToAudioFormat(toMIME)

	inputPath := filepath.Join(tempDir, "input."+fromFormat)
	outputPath := filepath.Join(tempDir, "output."+toFormat)

	// Write input file
	if writeErr := os.WriteFile(inputPath, data, DefaultTempFilePermissions); writeErr != nil {
		return nil, fmt.Errorf("failed to write input file: %w", writeErr)
	}

	// Build ffmpeg arguments
	args := c.buildFFmpegArgs(inputPath, outputPath, toFormat)

	// Run ffmpeg
	if runErr := c.runFFmpeg(ctx, args); runErr != nil {
		return nil, runErr
	}

	// Read output file
	//nolint:gosec // G304: outputPath is constructed from temp directory, not user input
	output, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read output file: %w", readErr)
	}

	return output, nil
}

// buildFFmpegArgs constructs ffmpeg command arguments.
func (c *AudioConverter) buildFFmpegArgs(inputPath, outputPath, toFormat string) []string {
	args := []string{
		"-y",
		"-i", inputPath,
		"-vn",
	}

	// Add sample rate if specified
	if c.config.SampleRate > 0 {
		args = append(args, "-ar", fmt.Sprintf("%d", c.config.SampleRate))
	}

	// Add channels if specified
	if c.config.Channels > 0 {
		args = append(args, "-ac", fmt.Sprintf("%d", c.config.Channels))
	}

	// Format-specific options
	switch toFormat {
	case AudioFormatWAV:
		// PCM 16-bit signed little-endian for maximum compatibility
		args = append(args, "-acodec", "pcm_s16le")

	case AudioFormatMP3:
		args = append(args, "-acodec", "libmp3lame")
		if c.config.BitRate != "" {
			args = append(args, "-b:a", c.config.BitRate)
		} else {
			args = append(args, "-b:a", "192k") // Default to good quality
		}

	case AudioFormatFLAC:
		args = append(args, "-acodec", "flac")

	case AudioFormatOGG:
		args = append(args, "-acodec", "libvorbis")
		if c.config.BitRate != "" {
			args = append(args, "-b:a", c.config.BitRate)
		}

	case AudioFormatAAC, AudioFormatM4A:
		args = append(args, "-acodec", "aac")
		if c.config.BitRate != "" {
			args = append(args, "-b:a", c.config.BitRate)
		}
	}

	// Output file
	args = append(args, outputPath)

	return args
}

// runFFmpeg executes ffmpeg with timeout.
func (c *AudioConverter) runFFmpeg(ctx context.Context, args []string) error {
	// Create context with timeout
	timeout := time.Duration(c.config.FFmpegTimeout) * time.Second
	ffmpegCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	//nolint:gosec // G204: FFmpegPath is configurable but expected to be ffmpeg binary
	cmd := exec.CommandContext(ffmpegCtx, c.config.FFmpegPath, args...)

	// Capture stderr for error messages
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	logger.Debug("Running ffmpeg", "args", args)

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

		return fmt.Errorf("ffmpeg failed: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// CheckFFmpegAvailable checks if ffmpeg is available in PATH.
func CheckFFmpegAvailable(ffmpegPath string) error {
	if ffmpegPath == "" {
		ffmpegPath = DefaultFFmpegPath
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultFFmpegCheckTimeout*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegPath, "-version")
	if err := cmd.Run(); err != nil {
		if execErr, ok := err.(*exec.Error); ok && execErr.Err == exec.ErrNotFound {
			return ErrFFmpegNotFound
		}
		return fmt.Errorf("ffmpeg check failed: %w", err)
	}
	return nil
}
