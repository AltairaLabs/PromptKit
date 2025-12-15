// Package video provides webcam capture for multimodal input.
package video

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// WebcamCapture handles webcam video capture
type WebcamCapture struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	frameOut    chan *Frame
	running     bool
	deviceIndex int
	width       int
	height      int
	fps         int
}

// Frame represents a captured video frame
type Frame struct {
	Data      []byte    // JPEG encoded image data
	Base64    string    // Base64 encoded for API
	Width     int       // Frame width
	Height    int       // Frame height
	Timestamp time.Time // Capture timestamp
}

// WebcamConfig holds webcam configuration
type WebcamConfig struct {
	DeviceIndex int // Camera device index (0 = default)
	Width       int // Capture width (default 640)
	Height      int // Capture height (default 480)
	FPS         int // Frames per second (default 30, widely supported)
}

// DefaultWebcamConfig returns sensible defaults for interview use
func DefaultWebcamConfig() WebcamConfig {
	return WebcamConfig{
		DeviceIndex: 0,
		Width:       640,
		Height:      480,
		FPS:         30, // 30 FPS is widely supported; we only send frames periodically
	}
}

// NewWebcamCapture creates a new webcam capture instance
func NewWebcamCapture(config WebcamConfig) *WebcamCapture {
	if config.Width == 0 {
		config.Width = 640
	}
	if config.Height == 0 {
		config.Height = 480
	}
	if config.FPS == 0 {
		config.FPS = 30
	}

	return &WebcamCapture{
		frameOut:    make(chan *Frame, 10),
		deviceIndex: config.DeviceIndex,
		width:       config.Width,
		height:      config.Height,
		fps:         config.FPS,
	}
}

// Start begins webcam capture
func (wc *WebcamCapture) Start(ctx context.Context) error {
	wc.mu.Lock()
	if wc.running {
		wc.mu.Unlock()
		return nil
	}
	wc.running = true
	wc.mu.Unlock()

	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		wc.running = false
		return fmt.Errorf("ffmpeg not found: %w (install with: brew install ffmpeg)", err)
	}

	// Start capture goroutine
	go wc.captureLoop(ctx)

	return nil
}

// Stop stops webcam capture
func (wc *WebcamCapture) Stop() {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	wc.running = false
	if wc.cmd != nil && wc.cmd.Process != nil {
		wc.cmd.Process.Kill()
		wc.cmd = nil
	}
}

// Frames returns the channel for receiving video frames
func (wc *WebcamCapture) Frames() <-chan *Frame {
	return wc.frameOut
}

// CaptureFrame captures a single frame from the webcam
func (wc *WebcamCapture) CaptureFrame(ctx context.Context) (*Frame, error) {
	// Build ffmpeg command based on platform
	args := wc.buildFFmpegArgs(true) // single frame mode

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to capture frame: %w", err)
	}

	// Encode to base64
	base64Data := base64.StdEncoding.EncodeToString(output)

	return &Frame{
		Data:      output,
		Base64:    base64Data,
		Width:     wc.width,
		Height:    wc.height,
		Timestamp: time.Now(),
	}, nil
}

func (wc *WebcamCapture) captureLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second / time.Duration(wc.fps))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			wc.Stop()
			return
		case <-ticker.C:
			wc.mu.Lock()
			if !wc.running {
				wc.mu.Unlock()
				return
			}
			wc.mu.Unlock()

			frame, err := wc.CaptureFrame(ctx)
			if err != nil {
				continue // Skip this frame
			}

			select {
			case wc.frameOut <- frame:
			default:
				// Drop if channel is full
			}
		}
	}
}

func (wc *WebcamCapture) buildFFmpegArgs(singleFrame bool) []string {
	var args []string

	switch runtime.GOOS {
	case "darwin":
		// macOS uses AVFoundation
		args = []string{
			"-f", "avfoundation",
			"-framerate", fmt.Sprintf("%d", wc.fps),
			"-video_size", fmt.Sprintf("%dx%d", wc.width, wc.height),
			"-i", fmt.Sprintf("%d", wc.deviceIndex),
		}
	case "linux":
		// Linux uses v4l2
		args = []string{
			"-f", "v4l2",
			"-framerate", fmt.Sprintf("%d", wc.fps),
			"-video_size", fmt.Sprintf("%dx%d", wc.width, wc.height),
			"-i", fmt.Sprintf("/dev/video%d", wc.deviceIndex),
		}
	case "windows":
		// Windows uses dshow
		args = []string{
			"-f", "dshow",
			"-framerate", fmt.Sprintf("%d", wc.fps),
			"-video_size", fmt.Sprintf("%dx%d", wc.width, wc.height),
			"-i", fmt.Sprintf("video=%d", wc.deviceIndex),
		}
	}

	if singleFrame {
		args = append(args,
			"-frames:v", "1",
			"-f", "image2pipe",
			"-vcodec", "mjpeg",
			"-q:v", "5", // Quality (2-31, lower is better)
			"-",
		)
	}

	return args
}

// ImageToJPEG converts an image.Image to JPEG bytes
func ImageToJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// IsWebcamAvailable checks if a webcam is available
func IsWebcamAvailable() bool {
	// Try to capture a single frame
	wc := NewWebcamCapture(DefaultWebcamConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := wc.CaptureFrame(ctx)
	return err == nil
}

// StartStreaming starts continuous video streaming at the configured FPS.
// Frames are sent to the Frames() channel. This is more efficient than
// calling CaptureFrame repeatedly as it uses a single ffmpeg process.
func (wc *WebcamCapture) StartStreaming(ctx context.Context, targetFPS int) error {
	wc.mu.Lock()
	if wc.running {
		wc.mu.Unlock()
		return nil
	}
	wc.running = true
	wc.mu.Unlock()

	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		wc.running = false
		return fmt.Errorf("ffmpeg not found: %w (install with: brew install ffmpeg)", err)
	}

	// Build ffmpeg args for continuous MJPEG streaming
	args := wc.buildStreamingArgs(targetFPS)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		wc.running = false
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		wc.running = false
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	wc.cmd = cmd

	// Start goroutine to read MJPEG stream
	go wc.readMJPEGStream(ctx, stdout)

	// Wait for process in background
	go func() {
		cmd.Wait()
		wc.mu.Lock()
		wc.running = false
		wc.mu.Unlock()
	}()

	return nil
}

// readMJPEGStream reads MJPEG frames from the ffmpeg output and sends them to frameOut
func (wc *WebcamCapture) readMJPEGStream(ctx context.Context, r io.Reader) {
	reader := bufio.NewReaderSize(r, 256*1024) // 256KB buffer

	// JPEG markers
	jpegStart := []byte{0xFF, 0xD8} // SOI (Start of Image)
	jpegEnd := []byte{0xFF, 0xD9}   // EOI (End of Image)

	var frameBuffer bytes.Buffer
	inFrame := false

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		b, err := reader.ReadByte()
		if err != nil {
			if err != io.EOF {
				fmt.Printf("[WEBCAM] Stream read error: %v\n", err)
			}
			return
		}

		frameBuffer.WriteByte(b)

		// Check for JPEG start marker
		if !inFrame && frameBuffer.Len() >= 2 {
			data := frameBuffer.Bytes()
			if bytes.HasSuffix(data, jpegStart) {
				// Start new frame, keep only the marker
				frameBuffer.Reset()
				frameBuffer.Write(jpegStart)
				inFrame = true
			} else if frameBuffer.Len() > 2 {
				// Not in a frame, discard old bytes but keep last byte
				last := data[len(data)-1]
				frameBuffer.Reset()
				frameBuffer.WriteByte(last)
			}
		}

		// Check for JPEG end marker
		if inFrame && frameBuffer.Len() >= 2 {
			data := frameBuffer.Bytes()
			if bytes.HasSuffix(data, jpegEnd) {
				// Complete frame received
				frameData := make([]byte, frameBuffer.Len())
				copy(frameData, data)

				frame := &Frame{
					Data:      frameData,
					Base64:    base64.StdEncoding.EncodeToString(frameData),
					Width:     wc.width,
					Height:    wc.height,
					Timestamp: time.Now(),
				}

				// Send to channel (non-blocking)
				select {
				case wc.frameOut <- frame:
				default:
					// Drop frame if channel full
				}

				frameBuffer.Reset()
				inFrame = false
			}
		}

		// Safety: limit buffer size
		if frameBuffer.Len() > 1024*1024 { // 1MB max
			frameBuffer.Reset()
			inFrame = false
		}
	}
}

// buildStreamingArgs builds ffmpeg arguments for continuous MJPEG streaming
func (wc *WebcamCapture) buildStreamingArgs(targetFPS int) []string {
	var args []string

	// Input settings based on platform
	switch runtime.GOOS {
	case "darwin":
		args = []string{
			"-f", "avfoundation",
			"-framerate", "30", // Capture at 30fps (camera requirement)
			"-video_size", fmt.Sprintf("%dx%d", wc.width, wc.height),
			"-i", fmt.Sprintf("%d", wc.deviceIndex),
		}
	case "linux":
		args = []string{
			"-f", "v4l2",
			"-framerate", "30",
			"-video_size", fmt.Sprintf("%dx%d", wc.width, wc.height),
			"-i", fmt.Sprintf("/dev/video%d", wc.deviceIndex),
		}
	case "windows":
		args = []string{
			"-f", "dshow",
			"-framerate", "30",
			"-video_size", fmt.Sprintf("%dx%d", wc.width, wc.height),
			"-i", fmt.Sprintf("video=%d", wc.deviceIndex),
		}
	}

	// Output settings: MJPEG stream at target FPS
	args = append(args,
		"-an",                                    // No audio
		"-vf", fmt.Sprintf("fps=%d", targetFPS), // Reduce to target FPS
		"-f", "mjpeg",                            // MJPEG format
		"-q:v", "10",                             // Quality (lower = better, 10 is decent for streaming)
		"-",                                      // Output to stdout
	)

	return args
}
