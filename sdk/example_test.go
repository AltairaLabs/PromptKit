package sdk_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	"github.com/AltairaLabs/PromptKit/sdk/v2/session"
)

// examplePack writes a one-prompt pack with a get_weather tool to the
// temporary directory and returns its path. A real program passes its own
// pack file.
func examplePack() string {
	path := filepath.Join(os.TempDir(), "promptkit-sdk-example.pack.json")
	const pack = `{
	"id": "assistant",
	"version": "1.0.0",
	"description": "Example assistant",
	"prompts": {
		"chat": {"id": "chat", "name": "Chat", "system_template": "You are a helpful assistant."}
	},
	"tools": {
		"get_weather": {
			"name": "get_weather",
			"description": "Current weather for a city",
			"parameters": {"type": "object", "properties": {"city": {"type": "string"}}}
		}
	}
}`
	if err := os.WriteFile(path, []byte(pack), 0o600); err != nil {
		log.Fatal(err)
	}
	return path
}

// openExample opens the example pack against a mock provider, so the
// examples run without an API key. A real program usually omits
// WithProvider and lets Open detect one from the environment.
func openExample(opts ...sdk.Option) *sdk.Conversation {
	opts = append([]sdk.Option{
		sdk.WithProvider(mock.NewProvider("mock", "mock-model", false)),
		sdk.WithSkipSchemaValidation(),
	}, opts...)
	conv, err := sdk.Open(examplePack(), "chat", opts...)
	if err != nil {
		log.Fatal(err)
	}
	return conv
}

// Example opens a pack, sends one message and prints the reply.
func Example() {
	conv, err := sdk.Open(examplePack(), "chat",
		sdk.WithProvider(mock.NewProvider("mock", "mock-model", false)),
		sdk.WithSkipSchemaValidation(),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "Hello!")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Text())
	// Output: Mock response from mock model mock-model
}

// ExampleOpen shows the error types Open returns: a missing pack file is a
// *PackError that still matches ErrPackNotFound.
func ExampleOpen() {
	_, err := sdk.Open("./no-such.pack.json", "chat",
		sdk.WithProvider(mock.NewProvider("mock", "mock-model", false)))

	var packErr *sdk.PackError
	fmt.Println(errors.As(err, &packErr), packErr.Path)
	fmt.Println(errors.Is(err, sdk.ErrPackNotFound))
	// Output:
	// true ./no-such.pack.json
	// true
}

// ExampleConversation_Send shows telling a provider failure from a hook
// denial on a failed turn.
func ExampleConversation_Send() {
	conv := openExample()
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "What's the weather in Paris?")
	var provErr *sdk.ProviderError
	switch {
	case errors.As(err, &provErr):
		fmt.Println("provider", provErr.Provider, "failed with status", provErr.StatusCode)
	case err != nil:
		if vErr, ok := sdk.AsValidationError(err); ok {
			fmt.Println("denied by", vErr.ValidatorType)
		}
	default:
		fmt.Println(resp.Text())
	}
	// Output: Mock response from mock model mock-model
}

// ExampleConversation_Stream prints a reply as it arrives.
func ExampleConversation_Stream() {
	conv := openExample()
	defer conv.Close()

	var text strings.Builder
	for chunk := range conv.Stream(context.Background(), "Tell me a story") {
		if chunk.Error != nil {
			log.Fatal(chunk.Error)
		}
		text.WriteString(chunk.Text)
	}
	fmt.Println(text.String())
	// Output: Mock response from mock model mock-model
}

// ExampleConversation_OnTool registers the handler for a pack tool. The
// handler's return value is serialized to JSON for the model.
func ExampleConversation_OnTool() {
	conv := openExample()
	defer conv.Close()

	conv.OnTool("get_weather", func(args map[string]any) (any, error) {
		city, _ := args["city"].(string)
		return map[string]any{"city": city, "forecast": "sunny"}, nil
	})

	resp, err := conv.Send(context.Background(), "What's the weather in Paris?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Text())
	// Output: Mock response from mock model mock-model
}

// ExampleConversation_OnToolCtx registers a handler that receives the turn's
// context, for cancellation and deadlines.
func ExampleConversation_OnToolCtx() {
	conv := openExample()
	defer conv.Close()

	conv.OnToolCtx("get_weather", func(ctx context.Context, args map[string]any) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return map[string]any{"forecast": "sunny"}, nil
	})
}

// ExampleConversation_Fork branches a conversation. The fork keeps the
// variables and tool handlers, and gets its own session ID.
func ExampleConversation_Fork() {
	conv := openExample()
	defer conv.Close()

	ctx := context.Background()
	conv.SetVar("traveler", "Alice")
	if _, err := conv.Send(ctx, "I want to plan a trip"); err != nil {
		log.Fatal(err)
	}

	branch, err := conv.Fork()
	if err != nil {
		log.Fatal(err)
	}
	defer branch.Close()

	traveler, _ := branch.GetVar("traveler")
	fmt.Println(traveler, strings.HasSuffix(branch.ID(), "-fork"))
	// Output: Alice true
}

// ExampleConversation_ResolveTool approves a call an OnToolAsync check held
// for review, then continues the turn. A handler failure is a *ToolError.
func ExampleConversation_ResolveTool() {
	conv := openExample()
	defer conv.Close()

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Process a refund for order #12345")
	if err != nil {
		log.Fatal(err)
	}
	for _, pending := range resp.PendingTools() {
		resolution, err := conv.ResolveTool(ctx, pending.ID)
		if err != nil {
			log.Fatal(err)
		}
		var toolErr *sdk.ToolError
		if errors.As(resolution.Error, &toolErr) {
			fmt.Println(toolErr.ToolName, "failed:", toolErr.Cause)
		}
	}
	if len(resp.PendingTools()) > 0 {
		if _, err := conv.Continue(ctx); err != nil {
			log.Fatal(err)
		}
	}
}

// ExampleWithStreamingVideo rate limits webcam frames to two a second and
// scales each kept frame to fit 640x480.
func ExampleWithStreamingVideo() {
	conv, err := sdk.OpenDuplex("./assistant.pack.json", "vision-chat",
		sdk.WithStreamingVideo(&sdk.VideoStreamConfig{
			TargetFPS:    2,
			MaxWidth:     640,
			MaxHeight:    480,
			Quality:      80,
			EnableResize: true,
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conv.Close()

	var jpeg []byte // a captured frame
	err = conv.SendFrame(context.Background(), &session.ImageFrame{
		Data:      jpeg,
		MIMEType:  "image/jpeg",
		Timestamp: time.Now(),
	})
	if err != nil {
		log.Fatal(err)
	}
}

// ExampleWithVADMode runs a voice conversation through speech-to-text, the
// model and text-to-speech. A nil config uses DefaultVADModeConfig.
func ExampleWithVADMode() {
	var sttService stt.Service // e.g. stt.NewOpenAI(apiKey)
	var ttsService tts.Service // e.g. tts.NewOpenAI(apiKey)

	conv, err := sdk.OpenDuplex("./assistant.pack.json", "voice-chat",
		sdk.WithVADMode(sttService, ttsService, &sdk.VADModeConfig{
			SilenceDuration: 500 * time.Millisecond,
			Voice:           "nova",
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conv.Close()
}

// ExampleWithAudioMonitor watches the audio levels of a duplex conversation.
func ExampleWithAudioMonitor() {
	conv, err := sdk.OpenDuplex("./voice.pack.json", "assistant",
		sdk.WithAudioMonitor(sdk.AudioMonitorOptions{
			Mode:       sdk.AudioMonitorOn,
			Rate:       24000,
			LevelMeter: true,
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conv.Close()

	go func() {
		for frame := range conv.AudioMonitor() {
			fmt.Printf("%s %.2f\n", frame.Direction, frame.Level)
		}
	}()
}

// ExampleAsValidationError shows unwrapping a validation failure returned by
// [sdk.Conversation.Send] when a hook or guardrail denies the turn.
// AsValidationError works on any error chain, so it is demonstrated here
// against a manually constructed error rather than a live Send call.
func ExampleAsValidationError() {
	err := fmt.Errorf("turn failed: %w", &sdk.ValidationError{
		ValidatorType: "banned_words",
		Message:       "response contained a banned word",
	})

	if vErr, ok := sdk.AsValidationError(err); ok {
		fmt.Println(vErr.ValidatorType, "-", vErr.Message)
	}

	var other error = errors.New("not a validation error")
	_, ok := sdk.AsValidationError(other)
	fmt.Println("matched:", ok)

	// Output:
	// banned_words - response contained a banned word
	// matched: false
}
