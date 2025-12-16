// Package main demonstrates Voice Activity Detection (VAD) in PromptKit.
//
// This example shows:
//   - Creating a SimpleVAD analyzer
//   - Processing audio frames and detecting speech
//   - Handling VAD state transitions
//   - Configuring VAD parameters
//
// Run with:
//
//	go run .
package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

func main() {
	fmt.Println("=== Voice Activity Detection (VAD) Demo ===")
	fmt.Println()

	// Example 1: Basic VAD with default parameters
	basicVADDemo()

	// Example 2: Custom VAD parameters
	customVADDemo()

	// Example 3: State change monitoring
	stateChangeDemo()
}

// basicVADDemo shows simple VAD usage with defaults.
func basicVADDemo() {
	fmt.Println("📢 Basic VAD Demo")
	fmt.Println("-----------------")

	// Create VAD with default parameters
	params := audio.DefaultVADParams()
	vad, err := audio.NewSimpleVAD(params)
	if err != nil {
		fmt.Printf("Failed to create VAD: %v\n", err)
		return
	}

	fmt.Printf("Created VAD: %s\n", vad.Name())
	fmt.Printf("Parameters:\n")
	fmt.Printf("  - Confidence threshold: %.2f\n", params.Confidence)
	fmt.Printf("  - Start threshold: %.2fs\n", params.StartSecs)
	fmt.Printf("  - Stop threshold: %.2fs\n", params.StopSecs)
	fmt.Printf("  - Min volume: %.4f\n", params.MinVolume)
	fmt.Printf("  - Sample rate: %d Hz\n", params.SampleRate)
	fmt.Println()

	ctx := context.Background()

	// Simulate audio with silence -> speech -> silence pattern
	fmt.Println("Simulating audio pattern: silence -> speech -> silence")
	fmt.Println()

	// Silence frames
	fmt.Println("  Sending silence frames...")
	for i := 0; i < 5; i++ {
		silence := generateSilence(320) // 20ms at 16kHz
		prob, _ := vad.Analyze(ctx, silence)
		fmt.Printf("    Frame %d: probability=%.2f, state=%s\n", i+1, prob, vad.State())
	}

	// Speech frames
	fmt.Println("  Sending speech frames...")
	for i := 0; i < 20; i++ {
		speech := generateTone(320, 440, 0.3) // 440Hz tone with amplitude 0.3
		prob, _ := vad.Analyze(ctx, speech)
		fmt.Printf("    Frame %d: probability=%.2f, state=%s\n", i+1, prob, vad.State())
	}

	// Silence again
	fmt.Println("  Sending silence frames (end of utterance)...")
	for i := 0; i < 50; i++ {
		silence := generateSilence(320)
		prob, _ := vad.Analyze(ctx, silence)
		if i%10 == 0 {
			fmt.Printf("    Frame %d: probability=%.2f, state=%s\n", i+1, prob, vad.State())
		}
	}

	fmt.Println()
}

// customVADDemo shows configuring VAD for different use cases.
func customVADDemo() {
	fmt.Println("🎛️  Custom VAD Parameters Demo")
	fmt.Println("------------------------------")

	// Strict VAD - requires higher confidence, longer pauses
	strictParams := audio.VADParams{
		Confidence: 0.7,  // Higher confidence required
		StartSecs:  0.3,  // Need longer speech to trigger
		StopSecs:   1.2,  // Allow longer pauses
		MinVolume:  0.02, // Higher volume threshold
		SampleRate: 16000,
	}

	fmt.Println("Strict VAD (for noisy environments):")
	fmt.Printf("  - Confidence: %.2f (higher = more strict)\n", strictParams.Confidence)
	fmt.Printf("  - Start threshold: %.2fs (longer speech needed)\n", strictParams.StartSecs)
	fmt.Printf("  - Stop threshold: %.2fs (tolerates pauses)\n", strictParams.StopSecs)
	fmt.Println()

	// Sensitive VAD - for quiet environments
	sensitiveParams := audio.VADParams{
		Confidence: 0.3,   // Lower confidence OK
		StartSecs:  0.1,   // Quick start detection
		StopSecs:   0.5,   // Short silence = end
		MinVolume:  0.005, // Detect quiet speech
		SampleRate: 16000,
	}

	fmt.Println("Sensitive VAD (for quiet environments):")
	fmt.Printf("  - Confidence: %.2f (more sensitive)\n", sensitiveParams.Confidence)
	fmt.Printf("  - Start threshold: %.2fs (quick detection)\n", sensitiveParams.StartSecs)
	fmt.Printf("  - Stop threshold: %.2fs (quick end detection)\n", sensitiveParams.StopSecs)
	fmt.Println()

	// Validate parameters
	if err := strictParams.Validate(); err != nil {
		fmt.Printf("Invalid strict params: %v\n", err)
	} else {
		fmt.Println("✓ Strict params validated")
	}

	if err := sensitiveParams.Validate(); err != nil {
		fmt.Printf("Invalid sensitive params: %v\n", err)
	} else {
		fmt.Println("✓ Sensitive params validated")
	}

	// Test invalid parameters
	invalidParams := audio.VADParams{
		Confidence: 1.5, // Invalid: > 1.0
		StartSecs:  0.2,
		StopSecs:   0.8,
		MinVolume:  0.01,
		SampleRate: 16000,
	}
	if err := invalidParams.Validate(); err != nil {
		fmt.Printf("✗ Invalid params caught: %v\n", err)
	}

	fmt.Println()
}

// stateChangeDemo shows monitoring VAD state transitions.
func stateChangeDemo() {
	fmt.Println("🔔 VAD State Change Demo")
	fmt.Println("------------------------")

	vad, _ := audio.NewSimpleVAD(audio.DefaultVADParams())

	// Get the state change channel
	stateChanges := vad.OnStateChange()

	ctx := context.Background()

	// Start a goroutine to monitor state changes
	done := make(chan struct{})
	go func() {
		for {
			select {
			case event, ok := <-stateChanges:
				if !ok {
					return
				}
				fmt.Printf("  📍 State change: %s -> %s (confidence: %.2f, duration: %v)\n",
					event.PrevState, event.State, event.Confidence, event.Duration)
			case <-done:
				return
			}
		}
	}()

	fmt.Println("Simulating conversation with natural pauses...")
	fmt.Println()

	// Simulate: silence -> speaking -> pause -> speaking -> silence
	phases := []struct {
		name     string
		frames   int
		isSpeech bool
	}{
		{"Initial silence", 10, false},
		{"First utterance", 30, true},
		{"Natural pause", 5, false},
		{"Continued speech", 20, true},
		{"End of turn", 60, false},
	}

	for _, phase := range phases {
		fmt.Printf("  Phase: %s\n", phase.name)
		for i := 0; i < phase.frames; i++ {
			var data []byte
			if phase.isSpeech {
				data = generateTone(320, 440, 0.25)
			} else {
				data = generateSilence(320)
			}
			_, _ = vad.Analyze(ctx, data)
			time.Sleep(2 * time.Millisecond) // Simulate real-time processing
		}
	}

	// Allow state change events to be processed
	time.Sleep(50 * time.Millisecond)
	close(done)

	fmt.Println()
	fmt.Println("✅ VAD Demo Complete")
	fmt.Println()
	fmt.Println("VAD States:")
	fmt.Println("  - quiet: No voice activity detected")
	fmt.Println("  - starting: Voice beginning (within start threshold)")
	fmt.Println("  - speaking: Active speech detected")
	fmt.Println("  - stopping: Voice ending (within stop threshold)")
}

// generateSilence creates a buffer of near-zero audio samples.
func generateSilence(samples int) []byte {
	// 16-bit little-endian samples
	data := make([]byte, samples*2)
	for i := 0; i < len(data); i += 2 {
		// Add tiny random noise to simulate real silence
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
		// Convert to 16-bit signed
		s16 := int16(sample * 32767)
		data[i*2] = byte(s16 & 0xFF)
		data[i*2+1] = byte((s16 >> 8) & 0xFF)
	}
	return data
}
