// Package main demonstrates a complete voice AI pipeline using the PromptKit SDK.
//
// This example shows:
//   - Setting up VAD (Voice Activity Detection)
//   - Turn detection for natural conversation flow
//   - TTS (Text-to-Speech) for spoken responses
//   - Handling user interruptions
//
// Note: This example simulates the audio pipeline since it doesn't capture
// real microphone input. In a production app, you would replace the
// simulated audio with real microphone input.
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		fmt.Println("⚠️  Set OPENAI_API_KEY environment variable to run this example")
		fmt.Println("   export OPENAI_API_KEY=your-key")
		os.Exit(1)
	}

	fmt.Println("=== Voice AI Pipeline Demo ===")
	fmt.Println()

	// Demo 1: VAD + Turn Detection concepts
	vadTurnDemo()

	// Demo 2: TTS Integration
	ttsIntegrationDemo()

	// Demo 3: Interruption handling
	interruptionDemo()
}

// vadTurnDemo demonstrates VAD and turn detection working together.
func vadTurnDemo() {
	fmt.Println("🎤 VAD + Turn Detection Demo")
	fmt.Println("----------------------------")
	fmt.Println()

	// Create VAD with default parameters
	vad, err := audio.NewSimpleVAD(audio.DefaultVADParams())
	if err != nil {
		fmt.Printf("Failed to create VAD: %v\n", err)
		return
	}

	// Create turn detector (basic implementation)
	turnDetector := &simpleTurnDetector{
		silenceFrames:    0,
		silenceThreshold: 40, // ~800ms at 20ms frames
		isSpeaking:       false,
	}

	ctx := context.Background()

	fmt.Println("Simulating a voice interaction:")
	fmt.Println("  User: [silence] -> [speaking] -> [pause] -> [speaking] -> [silence]")
	fmt.Println()

	// Simulate: silence -> speech -> short pause -> more speech -> end
	patterns := []struct {
		label    string
		duration int // number of 20ms frames
		speech   bool
	}{
		{"Waiting...", 10, false},
		{"User starts speaking", 25, true},
		{"Natural pause (thinking)", 10, false},
		{"User continues", 30, true},
		{"User finished", 50, false},
	}

	for _, p := range patterns {
		fmt.Printf("  📍 %s\n", p.label)
		for i := 0; i < p.duration; i++ {
			var audioData []byte
			if p.speech {
				audioData = generateTone(320, 440, 0.25)
			} else {
				audioData = generateSilence(320)
			}

			// Process through VAD
			_, _ = vad.Analyze(ctx, audioData)
			vadState := vad.State()

			// Process VAD state for turn detection
			endOfTurn, _ := turnDetector.ProcessVADState(ctx, vadState)

			// Log significant events
			if vadState == audio.VADStateSpeaking && !turnDetector.wasSpeaking {
				fmt.Println("     🟢 Speech started")
				turnDetector.wasSpeaking = true
			}
			if endOfTurn {
				fmt.Println("     🔴 Turn complete! Ready to process user input.")
			}

			time.Sleep(5 * time.Millisecond)
		}
	}

	fmt.Println()
}

// ttsIntegrationDemo shows the complete LLM -> TTS flow.
func ttsIntegrationDemo() {
	fmt.Println("🔊 LLM -> TTS Integration Demo")
	fmt.Println("------------------------------")
	fmt.Println()

	// Create TTS service
	ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

	// Open conversation with TTS
	conv, err := sdk.Open("./assistant.pack.json", "assistant",
		sdk.WithTTS(ttsService),
	)
	if err != nil {
		fmt.Printf("Failed to open pack: %v\n", err)
		return
	}
	defer conv.Close()

	ctx := context.Background()

	// Simulate a voice interaction
	userText := "What's the weather like today?"
	fmt.Printf("  👤 User (transcribed): \"%s\"\n", userText)

	// Get LLM response
	resp, err := conv.Send(ctx, userText)
	if err != nil {
		fmt.Printf("Failed to get response: %v\n", err)
		return
	}
	fmt.Printf("  🤖 Assistant: \"%s\"\n", resp.Text())

	// Convert to speech
	fmt.Println("  🔊 Converting response to speech...")
	audioReader, err := conv.SpeakResponse(ctx, resp,
		sdk.WithTTSVoice(tts.VoiceNova),
	)
	if err != nil {
		fmt.Printf("Failed to synthesize speech: %v\n", err)
		return
	}

	// Save to file for playback
	file, _ := os.Create("response.mp3")
	io.Copy(file, audioReader)
	audioReader.Close()
	file.Close()

	fmt.Println("  ✓ Saved to response.mp3")
	fmt.Println("  ▶️  Play with: afplay response.mp3")
	fmt.Println()
}

// interruptionDemo demonstrates handling user interruptions.
func interruptionDemo() {
	fmt.Println("⏸️  Interruption Handling Demo")
	fmt.Println("-----------------------------")
	fmt.Println()

	fmt.Println("Available interruption strategies:")
	fmt.Println()
	fmt.Printf("  • %s: Continue speaking, ignore interruption\n", audio.InterruptionIgnore)
	fmt.Printf("  • %s: Immediately stop and listen\n", audio.InterruptionImmediate)
	fmt.Printf("  • %s: Finish current sentence, then listen\n", audio.InterruptionDeferred)
	fmt.Println()

	// Create VAD
	vad, _ := audio.NewSimpleVAD(audio.DefaultVADParams())

	// Create interruption handler with immediate strategy
	handler := audio.NewInterruptionHandler(audio.InterruptionImmediate, vad)

	ctx := context.Background()

	fmt.Println("Simulating: Assistant speaking, user interrupts")
	fmt.Println()

	// Track if interrupted
	interrupted := false
	handler.OnInterrupt(func() {
		interrupted = true
		fmt.Println("  ⚡ INTERRUPT: User started speaking!")
	})

	// Simulate assistant speaking (low VAD)
	fmt.Println("  🤖 Assistant is speaking...")
	for i := 0; i < 20 && !interrupted; i++ {
		silence := generateSilence(320)
		vad.Analyze(ctx, silence)
		handler.ProcessVADState(ctx, vad.State())
		time.Sleep(10 * time.Millisecond)
	}

	// User starts speaking (high VAD)
	fmt.Println("  👤 User starts talking...")
	for i := 0; i < 15 && !interrupted; i++ {
		speech := generateTone(320, 440, 0.3)
		vad.Analyze(ctx, speech)
		wasInterrupted, _ := handler.ProcessVADState(ctx, vad.State())
		if wasInterrupted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if interrupted {
		fmt.Println("  ✓ Interruption detected - assistant stops, listens to user")
	}

	fmt.Println()
	fmt.Println("=== Demo Complete ===")
	fmt.Println()
	fmt.Println("In a real application, you would:")
	fmt.Println("  1. Capture microphone audio and feed to VAD")
	fmt.Println("  2. Use turn detection to know when user finished")
	fmt.Println("  3. Send transcribed text to LLM")
	fmt.Println("  4. Convert response to speech with TTS")
	fmt.Println("  5. Handle interruptions for natural conversation")
}

// simpleTurnDetector is a basic turn detector for demonstration.
type simpleTurnDetector struct {
	silenceFrames    int
	silenceThreshold int
	isSpeaking       bool
	wasSpeaking      bool
}

func (d *simpleTurnDetector) Name() string { return "simple-turn-detector" }

func (d *simpleTurnDetector) ProcessAudio(_ context.Context, _ []byte) (bool, error) {
	return false, nil
}

//nolint:unparam // error is part of interface contract
func (d *simpleTurnDetector) ProcessVADState(_ context.Context, state audio.VADState) (bool, error) {
	if state == audio.VADStateSpeaking {
		d.isSpeaking = true
		d.silenceFrames = 0
		return false, nil
	}

	if d.isSpeaking && (state == audio.VADStateQuiet || state == audio.VADStateStopping) {
		d.silenceFrames++
		if d.silenceFrames >= d.silenceThreshold {
			d.isSpeaking = false
			d.silenceFrames = 0
			return true, nil // End of turn
		}
	}

	return false, nil
}

func (d *simpleTurnDetector) IsUserSpeaking() bool { return d.isSpeaking }

func (d *simpleTurnDetector) Reset() {
	d.silenceFrames = 0
	d.isSpeaking = false
}

// generateSilence creates near-zero audio samples.
func generateSilence(samples int) []byte {
	data := make([]byte, samples*2)
	for i := 0; i < len(data); i += 2 {
		noise := int16(rand.Intn(20) - 10)
		data[i] = byte(noise & 0xFF)
		data[i+1] = byte((noise >> 8) & 0xFF)
	}
	return data
}

// generateTone creates a sine wave tone.
func generateTone(samples int, freq float64, amplitude float64) []byte {
	data := make([]byte, samples*2)
	sampleRate := 16000.0
	for i := 0; i < samples; i++ {
		t := float64(i) / sampleRate
		sample := amplitude * math.Sin(2*math.Pi*freq*t)
		s16 := int16(sample * 32767)
		data[i*2] = byte(s16 & 0xFF)
		data[i*2+1] = byte((s16 >> 8) & 0xFF)
	}
	return data
}
