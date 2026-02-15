// Package main demonstrates how to use pack evals with the PromptKit SDK.
//
// It opens a conversation with an eval-annotated pack, sends messages, and
// exports eval metrics in Prometheus text format.
//
// Usage:
//
//	go run ./examples/sdk-evals
//
// No API key required — uses a mock provider with canned responses.
// To use a real provider instead:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/sdk-evals --live
package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register built-in eval handlers
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

const packPath = "./examples/sdk-evals/assistant.pack.json"

func main() {
	fmt.Println("=== SDK Evals Example ===")
	fmt.Println()

	// 1. Load pack to extract eval definitions for MetricResultWriter.
	pack, err := prompt.LoadPack(packPath)
	if err != nil {
		log.Fatalf("Failed to load pack: %v", err)
	}
	fmt.Printf("Loaded pack %q with %d eval(s)\n\n", pack.Name, len(pack.Evals))

	// 2. Create a MetricCollector and wire it to a MetricResultWriter.
	collector := evals.NewMetricCollector()
	metricWriter := evals.NewMetricResultWriter(collector, pack.Evals)

	// 3. Create an EvalRunner with the default handler registry, then
	//    wrap it in an InProcDispatcher for synchronous in-process execution.
	registry := evals.NewEvalTypeRegistry()
	runner := evals.NewEvalRunner(registry)
	dispatcher := evals.NewInProcDispatcher(runner, nil)

	// 4. Build SDK options — use mock provider unless --live flag is passed.
	opts := []sdk.Option{
		sdk.WithEvalDispatcher(dispatcher),
		sdk.WithResultWriters(metricWriter),
	}

	live := len(os.Args) > 1 && os.Args[1] == "--live"
	if !live {
		repo := mock.NewInMemoryMockRepository(`{"response": "The capital of France is Paris."}`)
		provider := mock.NewProviderWithRepository("mock", "mock-model", false, repo)
		opts = append(opts, sdk.WithProvider(provider))
		fmt.Println("Using mock provider (pass --live to use a real LLM)")
		fmt.Println()
	}

	// 5. Open a conversation with eval support enabled.
	conv, err := sdk.Open(packPath, "chat", opts...)
	if err != nil {
		log.Fatalf("Failed to open conversation: %v", err)
	}

	ctx := context.Background()

	// 6. Send a few messages — turn evals run automatically after each Send().
	questions := []string{
		`What is the capital of France?`,
		`What is 2 + 2?`,
		`List three programming languages.`,
	}

	for i, q := range questions {
		fmt.Printf("Turn %d: %s\n", i+1, q)
		resp, sendErr := conv.Send(ctx, q)
		if sendErr != nil {
			log.Printf("  Error: %v", sendErr)
			continue
		}
		fmt.Printf("  Response: %s\n\n", truncate(resp.Text(), 120))
	}

	// 7. Close the conversation — this triggers session-level evals synchronously.
	conv.Close()

	// 8. Export Prometheus metrics.
	fmt.Println("=== Prometheus Metrics ===")
	fmt.Println()

	var buf bytes.Buffer
	if err := collector.WritePrometheus(&buf); err != nil {
		log.Fatalf("Failed to write metrics: %v", err)
	}

	if buf.Len() == 0 {
		fmt.Println("(no metrics recorded — evals may not have matched any triggers)")
	} else {
		fmt.Print(buf.String())
	}

	os.Exit(0)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
