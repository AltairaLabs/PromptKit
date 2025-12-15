package interview

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/examples/voice-interview/audio"
	"github.com/AltairaLabs/PromptKit/sdk/examples/voice-interview/video"
)

// Controller orchestrates the interview flow
type Controller struct {
	mu sync.RWMutex

	// Core components
	conv        *sdk.Conversation
	audioSystem *audio.AudioSystem
	webcam      *video.WebcamCapture
	state       *InterviewState

	// Event channels
	events      chan Event
	audioOut    chan []byte
	transcripts chan string

	// Configuration
	config ControllerConfig

	// Runtime state
	ctx    context.Context
	cancel context.CancelFunc
}

// ControllerConfig holds controller configuration
type ControllerConfig struct {
	Mode           InterviewMode
	EnableWebcam   bool
	WebcamInterval time.Duration // How often to send webcam frames
	VerboseLogging bool
}

// DefaultControllerConfig returns sensible defaults
func DefaultControllerConfig() ControllerConfig {
	return ControllerConfig{
		Mode:           ModeASM,
		EnableWebcam:   false,
		WebcamInterval: 2 * time.Second, // Fast enough for gesture detection
		VerboseLogging: false,
	}
}

// Event represents an interview event
type Event struct {
	Type      EventType
	Timestamp time.Time
	Data      interface{}
}

// EventType identifies the type of event
type EventType int

const (
	EventInterviewStarted EventType = iota
	EventQuestionAsked
	EventUserSpeaking
	EventUserSilent
	EventResponseReceived
	EventScoreUpdated
	EventAudioReceived
	EventTranscriptReceived
	EventInterviewCompleted
	EventError
)

// NewController creates a new interview controller
func NewController(
	conv *sdk.Conversation,
	audioSystem *audio.AudioSystem,
	questionBank *QuestionBank,
	config ControllerConfig,
) *Controller {
	sessionID := fmt.Sprintf("interview-%d", time.Now().UnixNano())
	state := NewInterviewState(sessionID, questionBank, config.Mode)

	return &Controller{
		conv:        conv,
		audioSystem: audioSystem,
		state:       state,
		events:      make(chan Event, 100),
		audioOut:    make(chan []byte, 500),
		transcripts: make(chan string, 10),
		config:      config,
	}
}

// SetWebcam sets the webcam capture (optional)
func (c *Controller) SetWebcam(webcam *video.WebcamCapture) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.webcam = webcam
}

// Events returns the event channel
func (c *Controller) Events() <-chan Event {
	return c.events
}

// AudioOutput returns the audio output channel (for TTS playback)
func (c *Controller) AudioOutput() <-chan []byte {
	return c.audioOut
}

// Transcripts returns the transcript channel
func (c *Controller) Transcripts() <-chan string {
	return c.transcripts
}

// State returns the current interview state
func (c *Controller) State() *InterviewState {
	return c.state
}

// Start begins the interview
func (c *Controller) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start audio capture
	if err := c.audioSystem.Capture().Start(c.ctx); err != nil {
		return fmt.Errorf("failed to start audio capture: %w", err)
	}

	// Start audio playback
	if err := c.audioSystem.Playback().Start(c.ctx); err != nil {
		return fmt.Errorf("failed to start audio playback: %w", err)
	}

	// Start the interview
	c.state.Start()
	c.emitEvent(EventInterviewStarted, nil)

	// Start processing goroutines
	// Both ASM and VAD modes now use the same duplex streaming approach
	// In ASM mode: audio → Gemini Live (native) → audio
	// In VAD mode: audio → [AudioTurn→STT→LLM→TTS pipeline] → audio
	go c.runAudioStreaming()
	go c.processResponses()

	// Start webcam processor if enabled
	if c.config.EnableWebcam && c.webcam != nil {
		go c.processWebcamFrames()
	}

	return nil
}

// Stop stops the interview
func (c *Controller) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.audioSystem != nil {
		c.audioSystem.Close()
	}
	if c.webcam != nil {
		c.webcam.Stop()
	}
}

// runAudioStreaming handles continuous audio streaming for both ASM and VAD modes
// In ASM mode: audio streams directly to Gemini Live API
// In VAD mode: audio flows through the pipeline (AudioTurn → STT → LLM → TTS)
func (c *Controller) runAudioStreaming() {
	modeStr := "ASM"
	if c.config.Mode == ModeVAD {
		modeStr = "VAD"
	}
	fmt.Printf("[%s] Audio streaming started\n", modeStr)

	audioChunks := c.audioSystem.Capture().AudioChunks()
	energyLevels := c.audioSystem.Capture().EnergyLevels()

	chunkCount := 0
	errorCount := 0
	fmt.Printf("[%s] Waiting for audio from microphone...\n", modeStr)

	for {
		select {
		case <-c.ctx.Done():
			fmt.Printf("[%s] Stopped - sent %d chunks (errors=%d)\n", modeStr, chunkCount, errorCount)
			return
		case audioData := <-audioChunks:
			chunkCount++
			if chunkCount == 1 {
				fmt.Printf("[%s] First audio chunk from mic: %d bytes\n", modeStr, len(audioData))
			}
			// Stream audio to the pipeline (works for both ASM and VAD modes)
			audioDataStr := string(audioData)
			chunk := &providers.StreamChunk{
				MediaDelta: &types.MediaContent{
					MIMEType: types.MIMETypeAudioWAV,
					Data:     &audioDataStr,
				},
			}

			if err := c.conv.SendChunk(c.ctx, chunk); err != nil {
				errorCount++
				if errorCount == 1 {
					fmt.Printf("[%s ERROR] First SendChunk error: %v\n", modeStr, err)
				} else if errorCount%10 == 0 {
					fmt.Printf("[%s ERROR] SendChunk errors: %d (latest: %v)\n", modeStr, errorCount, err)
				}
				c.emitEvent(EventError, err)
			} else if chunkCount%100 == 0 {
				fmt.Printf("[%s] Sent %d audio chunks\n", modeStr, chunkCount)
			}
		case energy := <-energyLevels:
			// Emit speaking/silent events for UI feedback
			if energy > 0.1 {
				c.emitEvent(EventUserSpeaking, energy)
			} else {
				c.emitEvent(EventUserSilent, energy)
			}
		}
	}
}

// processResponses handles streaming responses from the model (works for both ASM and VAD modes)
func (c *Controller) processResponses() {
	fmt.Println("[RESPONSE] processResponses goroutine started")

	// Get the response channel once - it's the same channel for the entire duplex session
	respCh, err := c.conv.Response()
	if err != nil {
		fmt.Printf("[RESPONSE ERROR] Failed to get response channel: %v\n", err)
		c.emitEvent(EventError, err)
		return
	}

	if respCh == nil {
		fmt.Println("[RESPONSE ERROR] Response channel is nil!")
		c.emitEvent(EventError, fmt.Errorf("response channel is nil"))
		return
	}

	fmt.Println("[RESPONSE] Response channel obtained, waiting for model responses...")
	fmt.Println("[RESPONSE] (If you see no further RESPONSE logs, Gemini is not responding)")

	// Process all chunks from the single response channel
	c.handleResponseStream(respCh)
}

// handleResponseStream processes a single response stream
func (c *Controller) handleResponseStream(respCh <-chan providers.StreamChunk) {
	fmt.Println("[RESPONSE] Waiting for chunks from Gemini...")
	var transcript string
	chunkCount := 0
	audioChunkCount := 0
	textChunkCount := 0
	lastLogTime := time.Now()

	for {
		select {
		case <-c.ctx.Done():
			fmt.Println("[RESPONSE] Stream canceled by context")
			return
		case chunk, ok := <-respCh:
			if !ok {
				// Stream ended
				fmt.Printf("[RESPONSE] Stream ended - Total: %d chunks (audio=%d, text=%d)\n",
					chunkCount, audioChunkCount, textChunkCount)
				if transcript != "" {
					c.emitEvent(EventTranscriptReceived, transcript)
					select {
					case c.transcripts <- transcript:
					default:
					}
				}
				return
			}

			chunkCount++
			if chunkCount == 1 {
				fmt.Println("[RESPONSE] *** FIRST RESPONSE CHUNK FROM GEMINI! ***")
			}

			// Periodic progress log
			if time.Since(lastLogTime) > 5*time.Second {
				fmt.Printf("[RESPONSE] Progress: %d chunks received (audio=%d, text=%d)\n",
					chunkCount, audioChunkCount, textChunkCount)
				lastLogTime = time.Now()
			}

			if chunk.Error != nil {
				fmt.Printf("[RESPONSE ERROR] Chunk error: %v\n", chunk.Error)
				c.emitEvent(EventError, chunk.Error)
				return
			}

			// Handle audio response
			if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
				audioChunkCount++
				// Decode base64 audio data from Gemini
				audioData, err := base64.StdEncoding.DecodeString(*chunk.MediaDelta.Data)
				if err != nil {
					fmt.Printf("[RESPONSE ERROR] Failed to decode audio: %v\n", err)
					continue
				}
				c.emitEvent(EventAudioReceived, len(audioData))

				if audioChunkCount == 1 {
					fmt.Printf("[RESPONSE] First AUDIO from Gemini: %d bytes (decoded from base64)\n", len(audioData))
				} else if audioChunkCount%50 == 0 {
					fmt.Printf("[RESPONSE] Received %d audio chunks\n", audioChunkCount)
				}

				// Queue for playback
				select {
				case c.audioOut <- audioData:
					// Also send to playback
					if err := c.audioSystem.Playback().Write(audioData); err != nil {
						fmt.Printf("[RESPONSE WARNING] Audio playback write error: %v\n", err)
					}
				default:
					fmt.Println("[RESPONSE WARNING] Audio output channel full, dropping chunk")
				}
			}

			// Handle text response (Delta contains incremental text)
			if chunk.Delta != "" {
				textChunkCount++
				if textChunkCount == 1 {
					fmt.Printf("[RESPONSE] First TEXT from Gemini: %q\n", truncate(chunk.Delta, 50))
				}
				transcript += chunk.Delta
			}

			// Check for finish
			if chunk.FinishReason != nil {
				fmt.Printf("[RESPONSE] Finished (reason=%s) - Total: %d chunks\n",
					*chunk.FinishReason, chunkCount)
				if transcript != "" {
					fmt.Printf("[RESPONSE] Final transcript: %s\n", truncate(transcript, 100))
					c.emitEvent(EventTranscriptReceived, transcript)
					select {
					case c.transcripts <- transcript:
					default:
					}
				}
				return
			}
		}
	}
}

// truncate shortens a string for logging
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// processWebcamFrames streams video frames to Gemini
func (c *Controller) processWebcamFrames() {
	if c.webcam == nil {
		return
	}

	// Start continuous streaming at 5 FPS (good balance for gesture detection)
	const streamFPS = 5
	fmt.Printf("[WEBCAM] Starting video stream at %d FPS\n", streamFPS)

	if err := c.webcam.StartStreaming(c.ctx, streamFPS); err != nil {
		fmt.Printf("[WEBCAM] Failed to start streaming: %v\n", err)
		// Fall back to periodic capture
		c.processWebcamFramesFallback()
		return
	}

	// Read frames from the stream and send to Gemini
	frames := c.webcam.Frames()
	frameCount := 0

	for {
		select {
		case <-c.ctx.Done():
			fmt.Printf("[WEBCAM] Stream stopped after %d frames\n", frameCount)
			return
		case frame, ok := <-frames:
			if !ok {
				fmt.Println("[WEBCAM] Frame channel closed")
				return
			}

			frameCount++
			if frameCount%10 == 1 { // Log every 10th frame
				fmt.Printf("[WEBCAM] Streaming frame #%d (%d bytes)\n", frameCount, len(frame.Data))
			}

			// Send frame to Gemini
			chunk := &providers.StreamChunk{
				MediaDelta: &types.MediaContent{
					MIMEType: types.MIMETypeImageJPEG,
					Data:     &frame.Base64,
				},
			}

			if err := c.conv.SendChunk(c.ctx, chunk); err != nil {
				// Don't log every error, just count them
				if frameCount%50 == 0 {
					fmt.Printf("[WEBCAM] Send errors occurring (frame %d)\n", frameCount)
				}
			}
		}
	}
}

// processWebcamFramesFallback uses the old ticker-based approach
func (c *Controller) processWebcamFramesFallback() {
	ticker := time.NewTicker(c.config.WebcamInterval)
	defer ticker.Stop()

	fmt.Printf("[WEBCAM] Using fallback mode (interval: %v)\n", c.config.WebcamInterval)

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.sendWebcamFrame()
		}
	}
}

// sendWebcamFrame captures and sends a single webcam frame
func (c *Controller) sendWebcamFrame() {
	if c.webcam == nil {
		return
	}

	frame, err := c.webcam.CaptureFrame(c.ctx)
	if err != nil {
		return // Skip this frame silently
	}

	chunk := &providers.StreamChunk{
		MediaDelta: &types.MediaContent{
			MIMEType: types.MIMETypeImageJPEG,
			Data:     &frame.Base64,
		},
	}

	if err := c.conv.SendChunk(c.ctx, chunk); err != nil {
		c.emitEvent(EventError, err)
	}
}

// emitEvent sends an event to the event channel
func (c *Controller) emitEvent(eventType EventType, data interface{}) {
	select {
	case c.events <- Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}:
	default:
		// Drop if channel is full
	}
}

// UpdateVariables updates the conversation variables
func (c *Controller) UpdateVariables() {
	vars := c.state.GetVariables()
	for k, v := range vars {
		c.conv.SetVar(k, v)
	}
}
