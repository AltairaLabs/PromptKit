// Command multitrack-ingestion is a generic demo of sdk.MultiTrackIngestion:
// two speakers' audio arrive on separate tracks, are transcribed independently,
// and the merged speaker-labeled transcript drives a per-turn assistant over a
// WithIngestion duplex.
//
// Keyless by default (synthetic audio + scripted STT + a mock LLM). Pass --live
// to use real Claude for the assistant:
//
//	export ANTHROPIC_API_KEY=sk-...
//	go run . --live
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	live := flag.Bool("live", false, "use real Claude for the assistant (needs ANTHROPIC_API_KEY)")
	verbose := flag.Bool("verbose", false, "show pipeline INFO/DEBUG logs")
	flag.Parse()

	// Quiet the pipeline's own INFO logs by default so the demo output is just
	// the transcript and replies; --verbose restores them.
	if *verbose {
		logger.SetVerbose(true)
	} else {
		logger.SetLevel(slog.LevelWarn)
	}

	var opts []sdk.Option
	if *live {
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			log.Fatal("--live needs ANTHROPIC_API_KEY")
		}
		opts = []sdk.Option{sdk.WithModel("claude-haiku-4-5"), sdk.WithAPIKey(key)}
	} else {
		provider := mock.NewProviderWithRepository("mock", "mock-model", false,
			mock.NewInMemoryMockRepository("Noted — thanks."))
		opts = []sdk.Option{sdk.WithProvider(providers.Provider(provider))}
	}

	if err := run(*live, opts); err != nil {
		log.Fatal(err)
	}
}

func run(live bool, providerOpts []sdk.Option) error {
	fmt.Println("🎙  multi-track ingestion demo")
	if live {
		fmt.Println("   mode: --live (real Claude)")
	} else {
		fmt.Println("   mode: keyless (mock LLM, scripted STT)")
	}
	fmt.Println()

	onTranscript := func(speaker, text string) {
		fmt.Printf("  %-11s %s\n", speaker+":", text)
	}
	conv, err := newConversationWithOpts(onTranscript, providerOpts)
	if err != nil {
		return err
	}
	defer func() { _ = conv.Close() }()

	respCh, err := conv.Response()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// On this path each turn's reply arrives as one non-empty chunk, so
		// printing every non-empty chunk shows one assistant line per turn.
		for chunk := range respCh {
			if chunk.Content != "" {
				fmt.Printf("  %-11s %s\n", "assistant:", chunk.Content)
			}
		}
	}()

	if err := feed(context.Background(), conv); err != nil {
		return err
	}
	_ = conv.Close()
	wg.Wait()
	return nil
}
