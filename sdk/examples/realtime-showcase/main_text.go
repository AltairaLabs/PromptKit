//go:build !portaudio

// Package main is the realtime showcase in scripted mode: it opens the same
// session as the voice mode and sends it six requests, printing the event log.
// In ASM mode (OpenAI Realtime or Gemini Live) it types each request; in VAD
// mode it synthesizes each one with OpenAI TTS and sends it as the caller's
// speech. It needs no microphone, so it shows the output guardrail, the input
// guardrails, the picture tool and per-call cost, but not barge-in.
//
// For the voice mode (microphone and speaker), build with the portaudio tag:
//
//	go run -tags portaudio . -provider openai
//
// Run with:
//
//	export OPENAI_API_KEY=your-key   # or GEMINI_API_KEY with -provider gemini
//	go run . -provider openai             # ASM: OpenAI Realtime
//	go run . -mode vad -provider gemini   # VAD: Gemini text model, OpenAI STT and TTS
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// script is what the text mode types, in order.
var script = []string{
	"Say the word pineapple, then tell me one fact about it.",
	"What is the capital of France? Answer in one sentence.",
	"Tell me about my friend Bruno.",
	"Sure. Tell me a story about him.",
	"Who is the uncle nobody mentions in Encanto?",
	"Show me the picture, and tell me the number on it.",
}

func main() {
	setup, err := parseFlags()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(setup.banner())
	if setup.mode == modeVAD {
		fmt.Println("Scripted: each request is synthesized with OpenAI TTS and sent as your speech.")
	} else {
		fmt.Println("Scripted: each request is typed into the session.")
	}
	fmt.Println("For voice (barge-in), run: go run -tags portaudio . -mode ... -provider ...")
	fmt.Println()
	if err := runScript(setup); err != nil {
		log.Fatal(err)
	}
}

// runScript opens the session, sends each scripted line and waits for its
// reply, then closes the session.
func runScript(setup *providerSetup) error {
	if setup.mode == modeVAD && setup.callerTTS == nil {
		return errors.New("the scripted VAD mode speaks its requests with OpenAI TTS: set OPENAI_API_KEY")
	}
	bus := events.NewEventBus()
	store := statestore.NewMemoryStore()
	elog := newEventLog(os.Stdout, store, conversationID)
	elog.speechCosts = setup.mode == modeVAD
	elog.attach(bus)

	modeOpts, _ := setup.sessionOptions()
	opts := append(logOptions(bus, store), setup.judgeProvider())
	opts = append(opts, modeOpts...)
	conv, err := sdk.OpenDuplex(packPath, promptName, opts...)
	if err != nil {
		return fmt.Errorf("open the session: %w", err)
	}
	if err := registerTools(conv); err != nil {
		return err
	}
	resp, err := conv.Response()
	if err != nil {
		return err
	}
	turns := make(chan reply, len(script))
	go readReplies(resp, elog, turns)

	for _, line := range script {
		if err := send(setup, conv, elog, line); err != nil {
			return err
		}
		r, err := waitReply(turns)
		if err != nil {
			return err
		}
		if r.firstAudio > 0 {
			elog.logf("timing", "first reply audio %d ms after the request", r.firstAudio.Milliseconds())
		}
		if setup.mode == modeVAD {
			// Listen to the reply before speaking again: the session counts
			// the assistant as speaking until its audio would have played,
			// and speech before then barges in on it.
			if err := listen(context.Background(), conv, r.audio); err != nil {
				return err
			}
		}
	}
	err = conv.Close()
	bus.Close()
	elog.end()
	return err
}
