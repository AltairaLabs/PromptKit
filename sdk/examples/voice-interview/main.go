//go:build portaudio

// Package main demonstrates a voice-enabled interview system using PromptKit's
// stage-based pipeline architecture.
//
// This example showcases:
//   - Stage-based pipeline with streaming support
//   - Both ASM (Audio Streaming Model) and VAD (Voice Activity Detection) modes
//   - TTS integration for voice output in VAD mode
//   - Optional webcam integration for multimodal context
//   - Rich terminal UI with progress tracking
//   - Multiple interview topics
//
// Usage:
//
//	# ASM mode (native bidirectional audio with Gemini)
//	go run . --mode asm --topic classic-rock
//
//	# VAD mode (turn-based with TTS)
//	go run . --mode vad --topic programming
//
//	# With webcam for visual context
//	go run . --mode asm --topic space --webcam
//
//	# List available topics
//	go run . --list-topics
//
// Requirements:
//   - GEMINI_API_KEY environment variable
//   - OPENAI_API_KEY environment variable (for VAD mode - STT/TTS)
//   - PortAudio library installed
//   - ffmpeg for webcam support (optional)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/examples/voice-interview/audio"
	"github.com/AltairaLabs/PromptKit/sdk/examples/voice-interview/interview"
	"github.com/AltairaLabs/PromptKit/sdk/examples/voice-interview/ui"
	"github.com/AltairaLabs/PromptKit/sdk/examples/voice-interview/video"
)

func main() {
	// Parse command-line flags
	mode := flag.String("mode", "asm", "Audio mode: 'asm' (native audio) or 'vad' (turn-based with TTS)")
	topic := flag.String("topic", "classic-rock", "Interview topic (use --list-topics to see options)")
	enableWebcam := flag.Bool("webcam", false, "Enable webcam for visual context")
	listTopics := flag.Bool("list-topics", false, "List available interview topics")
	packPath := flag.String("pack", "./interview.pack.json", "Path to PromptPack file")
	noUI := flag.Bool("no-ui", false, "Disable rich terminal UI (use simple output)")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	// Configure logging level
	logger.SetVerbose(*verbose)

	// Verify logger is working
	logger.Info("Logger initialized", "verbose", *verbose)
	logger.Debug("Debug logging enabled (only visible with --verbose)")

	// List topics and exit
	if *listTopics {
		printTopics()
		return
	}

	// Check for API key (SDK auto-detects from environment, but we validate early for better error messages)
	if os.Getenv("GEMINI_API_KEY") == "" {
		log.Fatal("GEMINI_API_KEY environment variable is required")
	}

	// Load question bank
	questionBank, err := interview.GetQuestionBank(*topic)
	if err != nil {
		log.Fatalf("Failed to load topic: %v", err)
	}

	// Determine interview mode
	var interviewMode interview.InterviewMode
	switch strings.ToLower(*mode) {
	case "asm", "audio":
		interviewMode = interview.ModeASM
	case "vad", "tts":
		interviewMode = interview.ModeVAD
	default:
		log.Fatalf("Unknown mode: %s. Use 'asm' or 'vad'", *mode)
	}

	// Print banner
	printBanner(questionBank, interviewMode, *enableWebcam)

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\nShutting down...")
		cancel()
	}()

	// Initialize audio system
	audioSystem, err := audio.NewAudioSystem()
	if err != nil {
		log.Fatalf("Failed to initialize audio: %v", err)
	}
	defer audioSystem.Close()

	// Initialize webcam if requested (only supported in ASM mode)
	var webcam *video.WebcamCapture
	if *enableWebcam {
		if interviewMode != interview.ModeASM {
			fmt.Println("⚠️  Webcam is only supported in ASM mode (--mode asm), disabling")
		} else {
			webcam = video.NewWebcamCapture(video.DefaultWebcamConfig())
			if !video.IsWebcamAvailable() {
				fmt.Println("⚠️  Webcam not available, continuing without video")
				webcam = nil
			}
		}
	}

	// Prepare initial variables BEFORE opening conversation
	// (Required for ASM mode where pipeline is built at open time)
	state := interview.NewInterviewState("interview-1", questionBank, interviewMode)
	initialVars := state.GetVariables()

	// Set visual instructions based on webcam status
	if webcam != nil {
		initialVars["visual_instructions"] = `Visual Context (Webcam Enabled):
You can see the user through their webcam. Use this visual context to:

GESTURE RECOGNITION:
- THUMBS UP = User is ready to continue, move to the next question
- THUMBS DOWN = User needs a hint or is struggling
- WAVING HAND = User wants to skip this question
- NODDING = User agrees or confirms
- SHAKING HEAD = User disagrees or says no

ENGAGEMENT CUES:
- If they look confused or uncertain, offer encouragement or a hint
- If they seem confident, you can be more challenging
- Acknowledge gestures naturally (e.g., "I see you're giving me a thumbs up - let's move on!")

IMPORTANT:
- React to gestures promptly when you see them
- Keep visual observations natural and conversational
- Do NOT comment on appearance, only engagement and gestures`
	} else {
		initialVars["visual_instructions"] = "" // No visual context available
	}

	// Create conversation based on mode
	var conv *sdk.Conversation
	switch interviewMode {
	case interview.ModeASM:
		// ASM mode: native bidirectional audio streaming
		// SDK auto-detects Gemini from GEMINI_API_KEY; we specify the model for Live API support
		conv, err = sdk.OpenDuplex(
			*packPath,
			"interviewer",
			sdk.WithModel("gemini-2.0-flash-exp"), // Required for Gemini Live API (native audio streaming)
			sdk.WithVariables(initialVars),
			sdk.WithStreamingConfig(&providers.StreamingInputConfig{
				Config: types.StreamingMediaConfig{
					Type:       types.ContentTypeAudio,
					SampleRate: audio.InputSampleRate,
					Channels:   audio.Channels,
					Encoding:   "pcm",
					BitDepth:   16,
					ChunkSize:  3200,
				},
				// Request audio responses from Gemini
				// Note: Gemini Live API does NOT support TEXT+AUDIO simultaneously
				Metadata: map[string]interface{}{
					"response_modalities": []string{"AUDIO"},
				},
			}),
		)
	case interview.ModeVAD:
		// VAD mode: turn-based conversation using pipeline stages
		// Requires OPENAI_API_KEY for STT (Whisper) and TTS
		openaiKey := os.Getenv("OPENAI_API_KEY")
		if openaiKey == "" {
			log.Fatal("OPENAI_API_KEY environment variable required for VAD mode (speech services)")
		}

		// Create STT and TTS services from runtime packages
		sttService := stt.NewOpenAI(openaiKey)
		ttsService := tts.NewOpenAI(openaiKey)

		// VAD mode uses OpenDuplex with WithVADMode - pipeline handles VAD → STT → LLM → TTS
		// SDK auto-detects Gemini from GEMINI_API_KEY
		conv, err = sdk.OpenDuplex(
			*packPath,
			"interviewer",
			sdk.WithModel("gemini-2.0-flash-exp"), // Use latest model for best results
			sdk.WithVariables(initialVars),
			sdk.WithVADMode(sttService, ttsService, &sdk.VADModeConfig{
				SilenceDuration:   1200 * time.Millisecond,
				MinSpeechDuration: 200 * time.Millisecond,
				SampleRate:        audio.InputSampleRate,
				Language:          "en",
				Voice:             "nova",
				Speed:             1.0,
			}),
		)
	}

	if err != nil {
		log.Fatalf("Failed to create conversation: %v", err)
	}
	defer conv.Close()

	// Create interview controller
	config := interview.DefaultControllerConfig()
	config.Mode = interviewMode
	config.EnableWebcam = *enableWebcam && webcam != nil
	config.VerboseLogging = *verbose
	controller := interview.NewController(conv, audioSystem, questionBank, config)
	if webcam != nil {
		controller.SetWebcam(webcam)
	}

	// VAD mode speech services are now handled by the pipeline via WithVADMode()
	// No manual setup needed - the controller uses the same duplex streaming approach as ASM mode

	// Start the interview
	if err := controller.Start(ctx); err != nil {
		log.Fatalf("Failed to start interview: %v", err)
	}
	defer controller.Stop()

	// Run UI or simple mode
	if *noUI {
		runSimpleMode(ctx, controller)
	} else {
		if err := ui.RunUI(controller.State(), controller.Events()); err != nil {
			log.Printf("UI error: %v", err)
		}
	}

	// Print summary
	printSummary(controller.State().GetSummary())
}

func printBanner(qb *interview.QuestionBank, mode interview.InterviewMode, webcamEnabled bool) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║         🎤 Voice Interview System - PromptKit Demo           ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Topic: %-52s ║\n", qb.Topic)
	fmt.Printf("║  Mode:  %-52s ║\n", mode.String())
	fmt.Printf("║  Questions: %-48d ║\n", len(qb.Questions))
	if webcamEnabled {
		fmt.Println("║  Webcam: Enabled                                             ║")
	}
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Controls:                                                   ║")
	fmt.Println("║    • Speak naturally into your microphone                    ║")
	fmt.Println("║    • Press Ctrl+C to end the interview                       ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
}

func printTopics() {
	fmt.Println("\n📚 Available Interview Topics:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	for _, topic := range interview.BuiltInTopics() {
		qb, _ := interview.GetQuestionBank(topic)
		if qb != nil {
			fmt.Printf("  • %-20s %s\n", topic, qb.Description)
		}
	}
	fmt.Println()
	fmt.Println("Usage: go run . --topic <topic-name>")
	fmt.Println()
}

func printSummary(summary *interview.InterviewSummary) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    📊 Interview Summary                      ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Topic: %-52s ║\n", summary.Topic)
	fmt.Printf("║  Score: %d/%d (%.0f%%) - Grade: %-27s ║\n",
		summary.TotalScore, summary.MaxScore, summary.Percentage, summary.Grade)
	fmt.Printf("║  Questions: %-48d ║\n", summary.QuestionsAsked)
	fmt.Printf("║  Hints Used: %-47d ║\n", summary.HintsUsed)
	fmt.Printf("║  Duration: %-49s ║\n", formatDuration(summary.Duration))
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Question Breakdown:                                         ║")
	for i, score := range summary.QuestionScores {
		marker := "✓"
		if score < 6 {
			marker = "✗"
		} else if score < 8 {
			marker = "~"
		}
		fmt.Printf("║    Q%d: %d/10 %s                                               ║\n", i+1, score, marker)
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
}

func runSimpleMode(ctx context.Context, controller *interview.Controller) {
	fmt.Println("🎙️  Listening... Speak into your microphone.")
	fmt.Println("Press Ctrl+C to end.")
	fmt.Println()

	events := controller.Events()
	transcripts := controller.Transcripts()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			switch event.Type {
			case interview.EventUserSpeaking:
				fmt.Print("█")
			case interview.EventUserSilent:
				fmt.Print("░")
			case interview.EventError:
				if err, ok := event.Data.(error); ok {
					fmt.Printf("\n⚠️  Error: %v\n", err)
				}
			case interview.EventInterviewCompleted:
				fmt.Println("\n\n✅ Interview completed!")
				return
			}
		case transcript := <-transcripts:
			fmt.Printf("\n🤖 %s\n", transcript)
		}
	}
}

func formatDuration(d interface{}) string {
	switch v := d.(type) {
	case int64:
		return fmt.Sprintf("%d:%02d", v/60, v%60)
	default:
		return fmt.Sprintf("%v", d)
	}
}
