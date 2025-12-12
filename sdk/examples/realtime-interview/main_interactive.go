// Package main demonstrates a voice interview using batch STT → LLM → TTS.
//
// This example showcases:
//   - Real Microphone Input - captures from system microphone via sox
//   - Speech-to-Text (Whisper API) - batch transcription
//   - LLM responses (GPT-4o)
//   - TTS (Text-to-Speech) - spoken responses
//
// Architecture (Batch Mode):
//
//	┌──────────────┐     ┌─────────────┐     ┌───────────────────┐
//	│  Microphone  │────►│   Whisper   │────►│       LLM         │
//	│  (sox rec)   │     │   (batch)   │     │    (GPT-4o)       │
//	└──────────────┘     └─────────────┘     └───────────────────┘
//	                                                   │
//	┌──────────────┐     ┌─────────────┐               │
//	│   Speaker    │◄────│     TTS     │◄──────────────┘
//	│  (afplay)    │     │  (OpenAI)   │
//	└──────────────┘     └─────────────┘
//
// Note: This is NOT streaming audio. For true bidirectional streaming,
// see the streaming-audio example which uses OpenAudioSession().
//
// Prerequisites:
//   - brew install sox  (for microphone capture)
//   - export OPENAI_API_KEY=your-key
//
// Run with:
//
//	go run .
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	if os.Getenv("OPENAI_API_KEY") == "" {
		fmt.Println("⚠️  Set OPENAI_API_KEY environment variable")
		fmt.Println("   export OPENAI_API_KEY=your-key")
		os.Exit(1)
	}

	// Check for sox
	if _, err := exec.LookPath("rec"); err != nil {
		fmt.Println("⚠️  'rec' command not found. Install sox:")
		fmt.Println("   brew install sox")
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("🎸 ════════════════════════════════════════════════════════════")
	fmt.Println("          CLASSIC ROCK INTERVIEW - VOICE EDITION")
	fmt.Println("   ════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("   This demo shows batch voice processing:")
	fmt.Println("   • Microphone capture (sox)")
	fmt.Println("   • Speech-to-Text (Whisper)")
	fmt.Println("   • LLM response (GPT-4o)")
	fmt.Println("   • Text-to-Speech (OpenAI TTS)")
	fmt.Println()
	fmt.Println("   Note: This is batch mode, not streaming.")
	fmt.Println("   See streaming-audio example for true streaming.")
	fmt.Println()
	fmt.Println("   Press Ctrl+C to exit")
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════")
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\n👋 Thanks for participating! Goodbye!")
		cancel()
		os.Exit(0)
	}()

	// Create and run the voice interview
	interview := NewVoiceInterview()
	if err := interview.Run(ctx); err != nil {
		fmt.Printf("Interview error: %v\n", err)
		os.Exit(1)
	}
}

// VoiceInterview manages the voice-based interview session.
type VoiceInterview struct {
	ttsService tts.Service
	conv       *sdk.Conversation
	apiKey     string

	// State
	questionCount int
	silenceCount  int
}

// NewVoiceInterview creates a new voice interview session.
func NewVoiceInterview() *VoiceInterview {
	return &VoiceInterview{
		apiKey: os.Getenv("OPENAI_API_KEY"),
	}
}

// Run executes the voice interview.
func (vi *VoiceInterview) Run(ctx context.Context) error {
	fmt.Println("🎤 Initializing...")

	// Create TTS Service
	vi.ttsService = tts.NewOpenAI(vi.apiKey)
	fmt.Println("   ✓ TTS service initialized")

	// Open SDK Conversation
	conv, err := sdk.Open("./rock-interview.pack.json", "interviewer",
		sdk.WithTTS(vi.ttsService),
	)
	if err != nil {
		return fmt.Errorf("failed to open conversation: %w", err)
	}
	defer conv.Close()
	vi.conv = conv
	fmt.Println("   ✓ Conversation loaded")

	fmt.Println()
	fmt.Println("🎙️  Ready!")
	fmt.Println()

	return vi.runInterviewLoop(ctx)
}

func (vi *VoiceInterview) runInterviewLoop(ctx context.Context) error {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Initial greeting
	resp, err := vi.conv.Send(ctx, "Hello! I'm ready for the classic rock interview.")
	if err != nil {
		return fmt.Errorf("initial send failed: %w", err)
	}

	if err := vi.speakResponse(ctx, resp); err != nil {
		fmt.Printf("   (TTS error: %v)\n", err)
	}

	// Main loop
	for vi.questionCount < 5 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Capture user's spoken answer
		userResponse, err := vi.captureUserTurn(ctx)
		if err != nil {
			fmt.Printf("   (Audio error: %v)\n", err)
			continue
		}

		// Handle silence
		if userResponse == "" {
			vi.silenceCount++
			if vi.silenceCount >= 2 {
				fmt.Println("\n😔 [No response - ending]")
				vi.speakText(ctx, "No worries, we can try again later. Thanks!")
				return nil
			}
			fmt.Println("\n🤔 [No speech detected]")
			vi.speakText(ctx, "Hello? Feel free to speak when you're ready.")
			continue
		}
		vi.silenceCount = 0

		fmt.Printf("\n👤 [You said]: \"%s\"\n", userResponse)

		// Get LLM response
		resp, err := vi.conv.Send(ctx, userResponse)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		// Speak response
		fmt.Println()
		if err := vi.speakResponse(ctx, resp); err != nil {
			fmt.Printf("   (TTS error: %v)\n", err)
		}

		// Check for completion
		text := resp.Text()
		if strings.Contains(text, "/50") || strings.Contains(text, "final score") ||
			strings.Contains(strings.ToLower(text), "thank you for") {
			fmt.Println("\n🎸 Interview complete!")
			break
		}

		vi.questionCount++
	}

	return nil
}

// captureUserTurn records audio and transcribes it.
func (vi *VoiceInterview) captureUserTurn(ctx context.Context) (string, error) {
	fmt.Println()
	fmt.Println("🎤 [Listening... speak now!]")

	// Create temp file
	tmpFile, err := os.CreateTemp("", "recording_*.wav")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Record with sox
	recordCtx, cancelRecord := context.WithTimeout(ctx, 12*time.Second)
	defer cancelRecord()

	cmd := exec.CommandContext(recordCtx, "rec",
		"-q",          // Quiet
		"-r", "16000", // 16kHz
		"-c", "1", // Mono
		"-b", "16", // 16-bit
		tmpPath,
		"trim", "0", "6", // Max 6 seconds
	)

	// Show progress
	fmt.Print("   Recording: ")
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	dots := []string{"🔴", "🔴", "🔴", "⚪", "⚪"}
	i := 0

recordLoop:
	for {
		select {
		case <-done:
			fmt.Println(" ✓")
			break recordLoop
		case <-ticker.C:
			fmt.Print(dots[i%len(dots)] + " ")
			i++
		case <-ctx.Done():
			cmd.Process.Kill()
			return "", ctx.Err()
		}
	}

	// Check file size
	info, err := os.Stat(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}
	fmt.Printf("   Captured: %d bytes\n", info.Size())
	if info.Size() < 1000 {
		return "", nil
	}

	// Transcribe
	fmt.Print("   Transcribing: ")
	transcript, err := vi.transcribeAudio(ctx, tmpPath)
	if err != nil {
		fmt.Println("❌")
		return "", fmt.Errorf("transcription failed: %w", err)
	}
	fmt.Println("✓")

	return transcript, nil
}

// transcribeAudio sends audio to Whisper API.
func (vi *VoiceInterview) transcribeAudio(ctx context.Context, audioPath string) (string, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}

	writer.WriteField("model", "whisper-1")
	writer.WriteField("language", "en")
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.openai.com/v1/audio/transcriptions", &buf)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+vi.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("whisper API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return strings.TrimSpace(result.Text), nil
}

// speakText synthesizes and plays text.
func (vi *VoiceInterview) speakText(ctx context.Context, text string) error {
	fmt.Printf("🔊 %s\n", text)

	config := tts.SynthesisConfig{
		Voice:  tts.VoiceNova,
		Format: tts.FormatMP3,
		Speed:  1.0,
	}
	audioReader, err := vi.ttsService.Synthesize(ctx, text, config)
	if err != nil {
		return err
	}
	defer audioReader.Close()

	tmpFile := fmt.Sprintf("/tmp/speak_%d.mp3", time.Now().UnixNano())
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, audioReader); err != nil {
		f.Close()
		return err
	}
	f.Close()
	defer os.Remove(tmpFile)

	return playAudioFile(tmpFile)
}

// speakResponse speaks an LLM response.
func (vi *VoiceInterview) speakResponse(ctx context.Context, resp *sdk.Response) error {
	text := resp.Text()

	fmt.Println("🎸 [Interviewer]:")
	fmt.Printf("   %s\n", text)

	audioReader, err := vi.conv.SpeakResponse(ctx, resp,
		sdk.WithTTSVoice(tts.VoiceNova),
		sdk.WithTTSSpeed(1.05),
	)
	if err != nil {
		return err
	}
	defer audioReader.Close()

	tmpFile := fmt.Sprintf("/tmp/interview_%d.mp3", time.Now().UnixNano())
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, audioReader); err != nil {
		f.Close()
		return err
	}
	f.Close()

	fmt.Println("   🔊 [Playing...]")
	if err := playAudioFile(tmpFile); err != nil {
		fmt.Printf("   (Could not play: %v)\n", err)
	}

	os.Remove(tmpFile)
	return nil
}

// playAudioFile plays audio using system commands.
func playAudioFile(filepath string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("afplay", filepath)
	case "linux":
		if _, err := exec.LookPath("aplay"); err == nil {
			cmd = exec.Command("aplay", filepath)
		} else if _, err := exec.LookPath("paplay"); err == nil {
			cmd = exec.Command("paplay", filepath)
		} else if _, err := exec.LookPath("play"); err == nil {
			cmd = exec.Command("play", "-q", filepath)
		} else {
			return fmt.Errorf("no audio player found")
		}
	case "windows":
		cmd = exec.Command("powershell", "-c",
			fmt.Sprintf("(New-Object Media.SoundPlayer '%s').PlaySync()", filepath))
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	return cmd.Run()
}
