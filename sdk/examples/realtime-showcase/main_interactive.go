//go:build portaudio

// Package main is the realtime showcase in voice mode: talk to OpenAI
// Realtime or Gemini Live through your microphone and hear the replies, while
// the terminal prints every event the session produces — messages, barge-in
// with how much of the reply you heard, guardrail blocks, the picture tool,
// and what each call cost.
//
// Requirements: PortAudio (brew install portaudio), headphones (the open
// microphone would otherwise hear the reply and barge in on it), and
// OPENAI_API_KEY or GEMINI_API_KEY.
//
// Run with:
//
//	go run -tags portaudio . -provider openai
//	go run -tags portaudio . -provider gemini
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	"github.com/AltairaLabs/PromptKit/sdk/v2/examples/audiohelper"
)

func main() {
	setup, err := parseFlags()
	if err != nil {
		log.Fatal(err)
	}
	if err := runVoice(setup); err != nil {
		log.Fatal(err)
	}
}

// runVoice opens the voice session and runs it until Ctrl+C.
func runVoice(setup *providerSetup) error {
	session, err := audiohelper.NewSession(
		audiohelper.WithCaptureRate(setup.inputRate),
		audiohelper.WithPlaybackRate(outputSampleRate),
	)
	if err != nil {
		return fmt.Errorf("open the microphone and speaker: %w", err)
	}
	defer session.Close()

	bus := events.NewEventBus()
	store := statestore.NewMemoryStore()
	elog := newEventLog(os.Stdout, store, conversationID)
	elog.attach(bus)

	// OpenVoice with an audio session paces the reply audio to the speaker
	// and tracks what was heard; WithPlayoutTracking says so explicitly.
	opts := append(sessionOptions(bus, store),
		sdk.WithProvider(setup.build(setup.model)),
		setup.judgeProvider(),
		sdk.WithStreamingConfig(setup.streamingConfig()),
		sdk.WithAudioSession(session),
		sdk.WithPlayoutTracking(),
		sdk.WithVoiceObserver(elog.observe),
	)
	cueOpts, heard := setup.cueOptions()
	opts = append(opts, cueOpts...)
	conv, err := sdk.OpenVoice(packPath, promptName, opts...)
	if err != nil {
		return fmt.Errorf("open the voice session: %w", err)
	}
	if err := registerTools(conv); err != nil {
		_ = conv.Close()
		return err
	}

	fmt.Printf("Realtime showcase - %s (%s)\n", setup.name, setup.model)
	fmt.Println("Guardrail messages: " + heard)
	fmt.Println("Listening. Use headphones. Try: \"tell me a long story\" (then talk over it),")
	fmt.Println("\"say the word pineapple\", \"tell me about Bruno\", then \"sure, tell me a story about him\",")
	fmt.Println("\"who is the uncle nobody mentions in Encanto?\", \"show me the picture\". Ctrl+C to quit.")
	fmt.Println()

	runErr := runUntilSignal(conv)
	closeErr := conv.Close()
	bus.Close()
	elog.end()
	if runErr != nil {
		return runErr
	}
	return closeErr
}

// runUntilSignal runs the session (microphone -> model -> speaker) until
// Ctrl+C or the session ends.
func runUntilSignal(conv *sdk.Conversation) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	errCh := make(chan error, 1)
	go func() { errCh <- conv.Start(ctx) }()
	select {
	case <-sig:
		fmt.Println()
		cancel()
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}
