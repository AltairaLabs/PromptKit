// Streaming Audio Example - Gemini Live API with SDK Integration
//
// This example demonstrates TRUE bidirectional audio streaming with Gemini,
// using the SDK's conversation management for state tracking.
//
// Gemini handles VAD/turn detection internally - we use OpenRawStreamSession()
// to get direct access without local VAD processing.
//
// Prerequisites:
//   - GEMINI_API_KEY or GOOGLE_API_KEY environment variable
//   - sox installed (brew install sox)
//   - HEADPHONES to prevent audio feedback!
//
// Usage:
//
//	go run .
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ConversationTurn represents a single turn in the conversation
type ConversationTurn struct {
	Role      string // "user" or "assistant"
	Text      string // Transcribed text
	Timestamp time.Time
}

// ConversationHistory tracks the full conversation
type ConversationHistory struct {
	mu    sync.Mutex
	turns []ConversationTurn
}

func (h *ConversationHistory) AddTurn(role, text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.turns = append(h.turns, ConversationTurn{
		Role:      role,
		Text:      text,
		Timestamp: time.Now(),
	})
}

func (h *ConversationHistory) Display() {
	h.mu.Lock()
	defer h.mu.Unlock()

	fmt.Println("\n\n📜 ════════════════════════════════════════════════════════════")
	fmt.Println("                    CONVERSATION HISTORY")
	fmt.Println("   ════════════════════════════════════════════════════════════")

	if len(h.turns) == 0 {
		fmt.Println("   (No conversation recorded)")
	} else {
		for i, turn := range h.turns {
			icon := "👤"
			if turn.Role == "assistant" {
				icon = "🤖"
			}
			fmt.Printf("\n   %s Turn %d (%s):\n", icon, i+1, turn.Role)
			// Word wrap the text
			words := strings.Fields(turn.Text)
			line := "      "
			for _, word := range words {
				if len(line)+len(word) > 70 {
					fmt.Println(line)
					line = "      "
				}
				line += word + " "
			}
			if line != "      " {
				fmt.Println(line)
			}
		}
	}
	fmt.Println("\n════════════════════════════════════════════════════════════════")
}

func main() {
	// Check for API key
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		fmt.Println("⚠️  Set GEMINI_API_KEY or GOOGLE_API_KEY environment variable")
		os.Exit(1)
	}

	// Check for sox
	if _, err := exec.LookPath("rec"); err != nil {
		fmt.Println("⚠️  'rec' command not found. Install sox:")
		fmt.Println("   brew install sox")
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("🎧 ════════════════════════════════════════════════════════════")
	fmt.Println("         STREAMING AUDIO - Gemini Live API + SDK")
	fmt.Println("   ════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("   ⚠️  USE HEADPHONES to prevent audio feedback!")
	fmt.Println()
	fmt.Println("   Features:")
	fmt.Println("   • Bidirectional audio streaming via Gemini Live API")
	fmt.Println("   • Gemini handles VAD and turn detection")
	fmt.Println("   • Conversation history tracking")
	fmt.Println("   • Natural interruption support")
	fmt.Println()
	fmt.Println("   Say 'goodbye' or 'bye' to end the conversation naturally")
	fmt.Println("   Press Ctrl+C to exit immediately")
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════")
	fmt.Println()

	// Conversation history
	history := &ConversationHistory{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\n👋 Session ended by user.")
		history.Display()
		cancel()
	}()

	if err := runStreamingSession(ctx, apiKey, history); err != nil {
		if err != context.Canceled {
			fmt.Printf("Error: %v\n", err)
		}
		history.Display()
		os.Exit(0)
	}

	history.Display()
}

func runStreamingSession(ctx context.Context, apiKey string, history *ConversationHistory) error {
	// Enable verbose logging to see Gemini messages
	logger.SetVerbose(true)

	fmt.Println("🎤 Initializing...")

	// Create Gemini provider
	provider := gemini.NewProvider(
		"gemini",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com",
		providers.ProviderDefaults{Temperature: 0.7},
		false,
	)
	defer provider.Close()
	fmt.Println("   ✓ Provider ready")

	// Open conversation using SDK - this gives us state tracking
	conv, err := sdk.Open("./assistant.pack.json", "voice",
		sdk.WithProvider(provider),
	)
	if err != nil {
		return fmt.Errorf("failed to open conversation: %w", err)
	}
	defer conv.Close()
	fmt.Println("   ✓ Conversation loaded")

	// Open RAW stream session - no VAD wrapper, Gemini handles turn detection
	// This uses the system prompt from assistant.pack.json
	session, err := conv.OpenRawStreamSession(ctx,
		sdk.WithResponseModalities("AUDIO"), // Get audio responses
	)
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}
	defer session.Close()
	fmt.Println("   ✓ Stream connected")

	fmt.Println()
	fmt.Println("🎙️  Ready! Speak into your microphone...")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Greet - ask assistant to introduce itself
	fmt.Print("🤖 ")
	if err := session.SendText(ctx, "Hello! Please introduce yourself briefly."); err != nil {
		fmt.Printf("(Greeting failed: %v)\n", err)
	}

	// Start mic capture
	micChunks := make(chan []byte, 50)
	go captureAudio(ctx, micChunks)

	// Audio player with turn-aware buffer management
	player := newAudioPlayer(ctx)
	defer player.Close()

	// Accumulate text for conversation tracking
	var responseText strings.Builder // Model's response transcription
	var userText strings.Builder     // User's transcribed speech

	// Silence counter for natural conversation end detection
	silentTurns := 0
	lastUserInput := time.Now()

	// Main streaming loop
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case chunk := <-micChunks:
			// Send mic audio directly to Gemini - no local processing
			mediaChunk := &types.MediaChunk{
				Data: chunk,
				Metadata: map[string]string{
					"mime_type": "audio/pcm;rate=16000",
				},
			}
			if err := session.SendChunk(ctx, mediaChunk); err != nil {
				fmt.Printf("\n(send err: %v)", err)
			}

		case resp, ok := <-session.Response():
			if !ok {
				// Channel closed - session ended
				fmt.Println("\n\n🔌 Session closed by server")
				return nil
			}

			if resp.Error != nil {
				fmt.Printf("\n(error: %v)", resp.Error)
				continue
			}

			// Handle interruption - user started speaking
			if resp.Interrupted {
				fmt.Print("\n⚡ [interrupted] ")
				player.Flush() // Clear any buffered audio
				responseText.Reset()
				continue
			}

			// Handle transcription events
			if resp.Metadata != nil {
				msgType, _ := resp.Metadata["type"].(string)

				// User's speech transcription
				if msgType == "input_transcription" {
					if transcript, ok := resp.Metadata["transcription"].(string); ok && transcript != "" {
						// Gemini sends complete transcription, not incremental
						// So replace rather than append
						userText.Reset()
						userText.WriteString(transcript)
						fmt.Printf("\n👤 %s", transcript)
						lastUserInput = time.Now()
						silentTurns = 0

						// Check for conversation end phrases
						lower := strings.ToLower(transcript)
						if strings.Contains(lower, "goodbye") || strings.Contains(lower, "bye") ||
							strings.Contains(lower, "see you") || strings.Contains(lower, "talk later") {
							fmt.Print(" [ending conversation...]")
							// Let the assistant respond, then exit
							go func() {
								time.Sleep(5 * time.Second)
								fmt.Println("\n\n👋 Conversation ended naturally.")
							}()
						}

						// Save user turn immediately - transcription IS the complete turn
						// Gemini sends transcription when the user turn is detected
						history.AddTurn("user", transcript)
						fmt.Print(" ✓")
					}
					continue
				}

				// Model's speech transcription
				if msgType == "output_transcription" {
					// Text is in Delta - handled below
				}
			}

			// Accumulate text for conversation tracking (model's response)
			if resp.Delta != "" {
				responseText.WriteString(resp.Delta)
				fmt.Print(resp.Delta)
			}

			// Play audio immediately
			if resp.MediaDelta != nil && resp.MediaDelta.Data != nil {
				if audio, err := base64.StdEncoding.DecodeString(*resp.MediaDelta.Data); err == nil {
					player.Play(audio)
				}
			}

			// Response complete - save to history and flush audio
			if resp.FinishReason != nil {
				// Wait for audio to finish playing
				player.WaitForDrain()

				if responseText.Len() > 0 {
					history.AddTurn("assistant", responseText.String())
					fmt.Print(" ✓")
				}
				fmt.Println()
				responseText.Reset()

				// Check for extended silence (no user input for a while)
				if time.Since(lastUserInput) > 30*time.Second {
					silentTurns++
					if silentTurns >= 2 {
						fmt.Println("\n\n😴 No activity detected. Ending session.")
						return nil
					}
				}
			}
		}
	}
}

// captureAudio captures from mic and sends raw PCM
func captureAudio(ctx context.Context, out chan<- []byte) {
	cmd := exec.CommandContext(ctx, "rec",
		"-q", "-r", "16000", "-c", "1", "-b", "16", "-t", "raw", "-",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Printf("mic error: %v\n", err)
		return
	}
	if err := cmd.Start(); err != nil {
		fmt.Printf("mic start error: %v\n", err)
		return
	}

	// Small chunks for low latency: 20ms = 640 bytes at 16kHz mono 16-bit
	buf := make([]byte, 640)
	for {
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
			return
		default:
		}

		n, err := stdout.Read(buf)
		if err != nil {
			if err != io.EOF {
				fmt.Printf("mic read error: %v\n", err)
			}
			return
		}
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case out <- data:
			default:
				// Drop if buffer full
			}
		}
	}
}

// AudioPlayer manages audio playback with proper turn boundaries
// It restarts the sox process between turns to ensure clean audio separation
type AudioPlayer struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	isPlaying bool

	// For tracking when audio finishes
	bytesSent   int64
	bytesPerSec int64 // 24000 * 2 * 1 = 48000
}

func newAudioPlayer(parentCtx context.Context) *AudioPlayer {
	ctx, cancel := context.WithCancel(parentCtx)
	return &AudioPlayer{
		ctx:         ctx,
		cancel:      cancel,
		bytesPerSec: 48000, // 24kHz * 2 bytes * 1 channel
	}
}

// ensureStarted starts sox if not already running
func (p *AudioPlayer) ensureStarted() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isPlaying && p.stdin != nil {
		return nil
	}

	p.cmd = exec.CommandContext(p.ctx, "play",
		"-q", "-r", "24000", "-c", "1", "-b", "16", "-e", "signed-integer", "-t", "raw", "-",
	)

	var err error
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start sox: %w", err)
	}

	p.isPlaying = true
	p.bytesSent = 0
	return nil
}

func (p *AudioPlayer) Play(data []byte) {
	if err := p.ensureStarted(); err != nil {
		fmt.Printf("\n(audio start err: %v)", err)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stdin != nil {
		n, err := p.stdin.Write(data)
		if err != nil {
			fmt.Printf("\n(audio write err: %v)", err)
			return
		}
		p.bytesSent += int64(n)
	}
}

// Flush stops playback and clears the buffer by killing sox
func (p *AudioPlayer) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stopLocked()
}

// stopLocked stops the current sox process (must hold mu)
func (p *AudioPlayer) stopLocked() {
	if p.stdin != nil {
		p.stdin.Close()
		p.stdin = nil
	}
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
		p.cmd.Wait()
		p.cmd = nil
	}
	p.isPlaying = false
	p.bytesSent = 0
}

// WaitForDrain waits for buffered audio to finish playing, then stops sox
func (p *AudioPlayer) WaitForDrain() {
	p.mu.Lock()
	bytesSent := p.bytesSent
	p.mu.Unlock()

	if bytesSent > 0 {
		// Estimate playback time and wait
		duration := time.Duration(bytesSent) * time.Second / time.Duration(p.bytesPerSec)
		// Add buffer for sox internal buffering
		duration += 200 * time.Millisecond

		select {
		case <-time.After(duration):
		case <-p.ctx.Done():
		}
	}

	// Stop sox to get a clean slate for the next turn
	p.mu.Lock()
	p.stopLocked()
	p.mu.Unlock()
}

func (p *AudioPlayer) Close() {
	p.cancel()
	p.mu.Lock()
	p.stopLocked()
	p.mu.Unlock()
}
