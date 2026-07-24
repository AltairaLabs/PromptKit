//go:build portaudio

// Package main demonstrates OpenAI Realtime API with bidirectional audio streaming.
//
// This example shows:
//   - Using OpenVoice() with the OpenAI Realtime API
//   - Real-time bidirectional audio at 24kHz over a bound audio.Session
//   - Conversation.Start driving mic → LLM → speaker with no hand-rolled pump
//   - Observing assistant text, input transcription, and tool calls via
//     WithVoiceObserver while Start manages the audio
//
// Requirements:
//   - OpenAI API key with Realtime API access
//   - Model: gpt-realtime
//   - Microphone input
//   - PortAudio library (brew install portaudio on macOS)
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run -tags portaudio .            # interactive (default)
//	go run -tags portaudio . tools      # function-calling demo
//	go run -tags portaudio . translator # live translation
//
// Note: The OpenAI Realtime API is in preview and requires special access.
// Visit https://platform.openai.com/docs/guides/realtime to learn more.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	rtaudio "github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/examples/audiohelper"
)

const (
	// OpenAI Realtime uses 24kHz for both input and output.
	sampleRate = 24000
	channels   = 1
)

func main() {
	fmt.Println("OpenAI Realtime API - Voice Chat")
	fmt.Println("=================================")
	fmt.Println()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	mode := "interactive"
	if len(os.Args) > 1 {
		mode = strings.ToLower(os.Args[1])
	}

	ctx := context.Background()

	switch mode {
	case "interactive", "voice", "audio":
		if err := runInteractiveMode(ctx, apiKey); err != nil {
			log.Fatalf("Interactive mode failed: %v", err)
		}
	case "tools":
		if err := runToolsDemo(ctx, apiKey); err != nil {
			log.Fatalf("Tools demo failed: %v", err)
		}
	case "translator":
		if err := runTranslatorMode(ctx, apiKey); err != nil {
			log.Fatalf("Translator mode failed: %v", err)
		}
	default:
		fmt.Printf("Unknown mode: %s\n", mode)
		fmt.Println("Available modes:")
		fmt.Println("  interactive - Voice chat with audio input/output (default)")
		fmt.Println("  tools       - Demo function calling during streaming")
		fmt.Println("  translator  - Real-time translation demo")
		os.Exit(1)
	}
}

// realtimeStreamingConfig is the shared OpenAI Realtime streaming setup. voice is
// the output voice; withTools adds the weather tool the tools demo exercises.
func realtimeStreamingConfig(voice string, withTools bool) *providers.StreamingInputConfig {
	cfg := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			SampleRate: sampleRate,
			Channels:   channels,
			Encoding:   "pcm16",
			BitDepth:   16,
			ChunkSize:  4800, // 100ms at 24kHz
		},
		Metadata: map[string]interface{}{
			"voice":               voice,
			"modalities":          []string{"text", "audio"},
			"input_transcription": true,
		},
	}
	if withTools {
		cfg.Tools = []providers.StreamingToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the current weather for a location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "The city and state, e.g. San Francisco, CA",
						},
						"unit": map[string]interface{}{
							"type": "string",
							"enum": []string{"celsius", "fahrenheit"},
						},
					},
					"required": []string{"location"},
				},
			},
		}
	}
	return cfg
}

// newAudioSession opens the shared pure-Go PortAudio session (24kHz mic + 24kHz
// speaker). Conversation.Start starts and stops it — callers only Close it.
func newAudioSession() (rtaudio.Session, error) {
	return audiohelper.NewSession(
		audiohelper.WithCaptureRate(sampleRate),
		audiohelper.WithPlaybackRate(sampleRate),
	)
}

// runInteractiveMode runs the main voice chat mode.
func runInteractiveMode(ctx context.Context, apiKey string) error {
	session, err := newAudioSession()
	if err != nil {
		return fmt.Errorf("failed to open audio session: %w", err)
	}
	defer session.Close()

	conv, err := sdk.OpenVoice(
		"./openai-realtime.pack.json", "assistant",
		sdk.WithModel("gpt-realtime"),
		sdk.WithAPIKey(apiKey),
		sdk.WithStreamingConfig(realtimeStreamingConfig("alloy", false)),
		sdk.WithAudioSession(session),
		sdk.WithVoiceObserver(newDisplayObserver(false)),
	)
	if err != nil {
		return fmt.Errorf("failed to open voice conversation: %w", err)
	}
	defer conv.Close()

	fmt.Println("Connected to OpenAI Realtime API!")
	fmt.Println("Listening... Speak into your microphone.")
	fmt.Println("OpenAI's server-side VAD will detect when you're done speaking.")
	fmt.Println("Press Ctrl+C to exit.")
	fmt.Println()
	return runUntilSignal(ctx, conv)
}

// runToolsDemo demonstrates function calling during realtime streaming. The tool
// call is surfaced to the observer, which runs the (simulated) tool and prints
// the result.
func runToolsDemo(ctx context.Context, apiKey string) error {
	session, err := newAudioSession()
	if err != nil {
		return fmt.Errorf("failed to open audio session: %w", err)
	}
	defer session.Close()

	conv, err := sdk.OpenVoice(
		"./openai-realtime.pack.json", "assistant",
		sdk.WithModel("gpt-realtime"),
		sdk.WithAPIKey(apiKey),
		sdk.WithStreamingConfig(realtimeStreamingConfig("alloy", true)),
		sdk.WithAudioSession(session),
		sdk.WithVoiceObserver(newDisplayObserver(true)),
	)
	if err != nil {
		return fmt.Errorf("failed to open voice conversation: %w", err)
	}
	defer conv.Close()

	fmt.Println("Connected! Ask about the weather to trigger a tool call.")
	fmt.Println("Press Ctrl+C to exit.")
	fmt.Println()
	return runUntilSignal(ctx, conv)
}

// runTranslatorMode runs a real-time English→Spanish translator.
func runTranslatorMode(ctx context.Context, apiKey string) error {
	session, err := newAudioSession()
	if err != nil {
		return fmt.Errorf("failed to open audio session: %w", err)
	}
	defer session.Close()

	conv, err := sdk.OpenVoice(
		"./openai-realtime.pack.json", "translator",
		sdk.WithModel("gpt-realtime"),
		sdk.WithAPIKey(apiKey),
		sdk.WithVariables(map[string]string{"target_language": "Spanish"}),
		sdk.WithStreamingConfig(realtimeStreamingConfig("shimmer", false)),
		sdk.WithAudioSession(session),
		sdk.WithVoiceObserver(newDisplayObserver(false)),
	)
	if err != nil {
		return fmt.Errorf("failed to open voice conversation: %w", err)
	}
	defer conv.Close()

	fmt.Println("Connected to OpenAI Realtime Translator!")
	fmt.Println("Speak in English and hear the Spanish translation.")
	fmt.Println("Press Ctrl+C to exit.")
	fmt.Println()
	return runUntilSignal(ctx, conv)
}

// runUntilSignal drives the voice session with Conversation.Start (mic → LLM →
// speaker) and returns cleanly on Ctrl+C.
func runUntilSignal(ctx context.Context, conv *sdk.Conversation) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() { errCh <- conv.Start(ctx) }()

	select {
	case <-sigChan:
		fmt.Println("\n\nShutting down...")
		cancel()
		<-errCh // let Start unwind
		return nil
	case err := <-errCh:
		return err
	}
}

// newDisplayObserver returns a WithVoiceObserver callback that prints the
// conversation as it streams — the caller's transcribed speech, the assistant's
// text, and (when withTools) any tool calls, which it runs and reports. Start
// plays the audio; this only displays. The returned closure is called from a
// single goroutine, so its running state needs no locking.
func newDisplayObserver(withTools bool) func(providers.StreamChunk) {
	assistantOpen := false
	// closeAssistantLine ends the current "Assistant: ..." line (if any) so an
	// interleaved event prints on its own line; the assistant re-prefixes when its
	// next delta arrives.
	closeAssistantLine := func() {
		if assistantOpen {
			fmt.Println()
			assistantOpen = false
		}
	}
	return func(c providers.StreamChunk) {
		// User transcripts (Whisper) arrive asynchronously — often mid-reply, in the
		// middle of the assistant's line. Break the line first so the turn is legible
		// instead of spliced into the assistant's words.
		if t, ok := c.Metadata["input_transcription"].(string); ok && t != "" {
			closeAssistantLine()
			fmt.Printf("\033[33m[You said: %s]\033[0m\n", t)
		}
		if c.Delta != "" {
			if !assistantOpen {
				fmt.Print("\033[34mAssistant:\033[0m ")
				assistantOpen = true
			}
			fmt.Print(c.Delta)
		}
		if withTools {
			for _, tc := range c.ToolCalls {
				closeAssistantLine()
				args := string(tc.Args)
				fmt.Printf("\033[35m[Tool call: %s(%s)]\033[0m\n", tc.Name, args)
				fmt.Printf("\033[35m[Tool result: %s]\033[0m\n", executeToolCall(tc.Name, args))
			}
		}
		// Close the line at each assistant turn boundary so consecutive replies don't
		// run together. assistant_turn_complete is emitted per turn; FinishReason is
		// the terminal (e.g. pending_tools) case.
		if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
			closeAssistantLine()
		}
		if c.FinishReason != nil {
			closeAssistantLine()
		}
	}
}

// executeToolCall simulates executing a tool call for the tools demo.
func executeToolCall(name, arguments string) string {
	switch name {
	case "get_weather":
		var args struct {
			Location string `json:"location"`
			Unit     string `json:"unit"`
		}
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return fmt.Sprintf("Error parsing arguments: %v", err)
		}
		unit := args.Unit
		if unit == "" {
			unit = "fahrenheit"
		}
		temp := 72
		if unit == "celsius" {
			temp = 22
		}
		return fmt.Sprintf("The weather in %s is currently sunny with a temperature of %d degrees %s.",
			args.Location, temp, unit)
	default:
		return fmt.Sprintf("Unknown tool: %s", name)
	}
}
