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
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// fatal prints to stderr and exits non-zero. It deliberately does not use the
// stdlib log package: runtime/logger routes stdlib log through slog, so once we
// quiet the pipeline to WARN a log.Fatal message would be filtered out and the
// program would exit silently.
func fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

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
			fatal("--live needs ANTHROPIC_API_KEY")
		}
		opts = []sdk.Option{sdk.WithModel("claude-haiku-4-5"), sdk.WithAPIKey(key)}
	} else {
		provider := mock.NewProviderWithRepository("mock", "mock-model", false,
			mock.NewInMemoryMockRepository("Noted — thanks."))
		opts = []sdk.Option{sdk.WithProvider(providers.Provider(provider))}
	}

	if err := run(*live, opts); err != nil {
		fatal(err)
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
	wg.Go(func() {
		// A turn's reply may arrive as one chunk (mock) or as streamed deltas
		// (Claude). Accumulate the deltas and print one line per turn: flush on
		// FinishReason (Claude marks the end of a response) or on a trailing
		// empty chunk (the mock's per-turn separator).
		var buf strings.Builder
		flush := func() {
			if buf.Len() > 0 {
				fmt.Printf("  %-11s %s\n", "assistant:", buf.String())
				buf.Reset()
			}
		}
		for chunk := range respCh {
			if chunk.Delta != "" {
				buf.WriteString(chunk.Delta)
			}
			if chunk.FinishReason != nil || (chunk.Delta == "" && chunk.Content == "") {
				flush()
			}
		}
		flush()
	})

	if err := feed(context.Background(), conv); err != nil {
		return err
	}
	_ = conv.Close()
	wg.Wait()
	return nil
}
