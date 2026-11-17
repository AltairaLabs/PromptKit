//go:build !portaudio

// Package main is the realtime showcase in scripted text mode: it opens the
// same realtime session as the voice mode (OpenAI Realtime or Gemini Live),
// types four requests into it and prints the event log. It needs no
// microphone, so it shows the output guardrail, the input guardrail, the
// picture tool and per-call cost, but not barge-in.
//
// For the voice mode (microphone and speaker), build with the portaudio tag:
//
//	go run -tags portaudio . -provider openai
//
// Run with:
//
//	export OPENAI_API_KEY=your-key   # or GEMINI_API_KEY with -provider gemini
//	go run . -provider openai
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// turnTimeout bounds how long the script waits for each reply.
const turnTimeout = 90 * time.Second

// script is what the text mode types, in order.
var script = []string{
	"Say the word pineapple, then tell me one fact about it.",
	"What is the capital of France? Answer in one sentence.",
	"I'd like a banana smoothie recipe.",
	"Show me the picture, and tell me the number on it.",
}

func main() {
	setup, err := parseFlags()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Realtime showcase - %s (%s), scripted text mode\n", setup.name, setup.model)
	fmt.Println("For voice (barge-in), run: go run -tags portaudio . -provider ...")
	fmt.Println()
	if err := runScript(setup); err != nil {
		log.Fatal(err)
	}
}

// runScript opens the session, sends each scripted line and waits for its
// reply, then closes the session.
func runScript(setup *providerSetup) error {
	bus := events.NewEventBus()
	store := statestore.NewMemoryStore()
	elog := newEventLog(os.Stdout, store, conversationID)
	elog.attach(bus)

	opts := append(sessionOptions(bus, store),
		sdk.WithProvider(setup.build(setup.model)),
		sdk.WithStreamingConfig(setup.streamingConfig()))
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
	turns := make(chan string, len(script))
	go readReplies(resp, elog, turns)

	for _, line := range script {
		elog.logf("you type", "%q", line)
		if err := conv.SendText(context.Background(), line); err != nil {
			return err
		}
		if err := waitReply(turns); err != nil {
			return err
		}
	}
	err = conv.Close()
	bus.Close()
	elog.end()
	return err
}

// readReplies reads Response(), shows each chunk to the log, and reports
// each finished reply's finish reason on turns. A tool-call turn is not a
// reply: the model answers after the tool's result.
func readReplies(resp <-chan providers.StreamChunk, elog *eventLog, turns chan<- string) {
	for c := range resp {
		elog.observe(c)
		if done, _ := c.Metadata["assistant_turn_complete"].(bool); !done {
			continue
		}
		if fr, _ := c.Metadata["finish_reason"].(string); fr != "tool_use" {
			select {
			case turns <- fr:
			default: // nobody is waiting (the script is done)
			}
		}
	}
	close(turns)
}

// waitReply waits for the next reply to finish.
func waitReply(turns <-chan string) error {
	select {
	case _, ok := <-turns:
		if !ok {
			return errors.New("the session ended")
		}
		// Let the reply's message.created and cost lines print before the
		// next request.
		time.Sleep(time.Second)
		return nil
	case <-time.After(turnTimeout):
		return errors.New("no reply within the timeout")
	}
}
