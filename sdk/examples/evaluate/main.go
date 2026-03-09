// Package main demonstrates standalone eval execution with sdk.Evaluate().
//
// This example shows:
//   - Running evals from a PromptPack against a conversation snapshot
//   - No live provider or agent connection needed — just messages in, results out
//   - Filtering evals by trigger (every_turn vs on_session_complete)
//   - Running evals from inline definitions (no pack file needed)
//   - Receiving eval results via the EventBus for reactive workflows
//
// Run with:
//
//	go run .
package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	ctx := context.Background()

	// Example 1: Evaluate a conversation snapshot against a pack file.
	// The pack defines three evals:
	//   - greeting_check (every_turn): looks for "hello"
	//   - polite_check   (every_turn): looks for "please" AND "thank"
	//   - farewell_check  (on_session_complete): looks for "goodbye"
	fmt.Println("=== Example 1: Pack-based evals (every_turn) ===")
	results, err := sdk.Evaluate(ctx, sdk.EvaluateOpts{
		PackPath:             "./evaluate.pack.json",
		SkipSchemaValidation: true,
		Messages: []types.Message{
			types.NewUserMessage("Hi there!"),
			types.NewAssistantMessage("Hello! How can I help you today?"),
		},
		SessionID: "demo-session",
		TurnIndex: 1,
	})
	if err != nil {
		log.Fatalf("Evaluate failed: %v", err)
	}
	printResults(results)

	// Example 2: Run session-complete evals from the same pack.
	// Only farewell_check runs since it has trigger: on_session_complete.
	fmt.Println("\n=== Example 2: Pack-based evals (on_session_complete) ===")
	results, err = sdk.Evaluate(ctx, sdk.EvaluateOpts{
		PackPath:             "./evaluate.pack.json",
		SkipSchemaValidation: true,
		Trigger:              evals.TriggerOnSessionComplete,
		Messages: []types.Message{
			types.NewUserMessage("Thanks for your help!"),
			types.NewAssistantMessage("You're welcome! Goodbye and have a great day!"),
		},
		SessionID: "demo-session",
	})
	if err != nil {
		log.Fatalf("Evaluate failed: %v", err)
	}
	printResults(results)

	// Example 3: Inline eval definitions — no pack file needed.
	// Useful for programmatic or dynamic eval scenarios.
	fmt.Println("\n=== Example 3: Inline eval definitions ===")
	inlineDefs := []evals.EvalDef{
		{
			ID:      "has_code_block",
			Type:    "contains",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"patterns": []any{"```"}},
		},
		{
			ID:      "mentions_error",
			Type:    "contains",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"patterns": []any{"error"}},
		},
	}
	results, err = sdk.Evaluate(ctx, sdk.EvaluateOpts{
		EvalDefs: inlineDefs,
		Messages: []types.Message{
			types.NewUserMessage("How do I read a file in Go?"),
			types.NewAssistantMessage("Here's how to read a file:\n\n```go\ndata, err := os.ReadFile(\"file.txt\")\n```"),
		},
	})
	if err != nil {
		log.Fatalf("Evaluate failed: %v", err)
	}
	printResults(results)

	// Example 4: Receive eval results via the EventBus.
	// The EventBus lets you react to eval outcomes asynchronously —
	// useful for logging, alerting, or triggering downstream actions.
	fmt.Println("\n=== Example 4: EventBus-driven eval results ===")

	bus := events.NewEventBus()
	var mu sync.Mutex
	counters := map[string]int{"passed": 0, "failed": 0}

	for _, et := range []events.EventType{events.EventEvalCompleted, events.EventEvalFailed} {
		eventType := et
		bus.Subscribe(eventType, func(e *events.Event) {
			data, ok := e.Data.(*events.EvalEventData)
			if !ok {
				return
			}
			mu.Lock()
			if eventType == events.EventEvalCompleted {
				counters["passed"]++
			} else {
				counters["failed"]++
			}
			mu.Unlock()
			fmt.Printf("  [EVENT] %s: %s — %s\n", eventType, data.EvalID, data.Explanation)
		})
	}

	// Reuse the inline defs from Example 3, adding a check that will fail.
	eventDefs := append(inlineDefs, evals.EvalDef{
		ID:      "apology",
		Type:    "contains",
		Trigger: evals.TriggerEveryTurn,
		Params:  map[string]any{"patterns": []any{"sorry"}},
	})
	_, err = sdk.Evaluate(ctx, sdk.EvaluateOpts{
		EvalDefs: eventDefs,
		Messages: []types.Message{
			types.NewAssistantMessage("Here's how:\n\n```go\ndata, err := os.ReadFile(\"file.txt\")\n```"),
		},
		EventBus: bus,
	})
	if err != nil {
		log.Fatalf("Evaluate failed: %v", err)
	}

	// Close the bus to flush pending events before reading counters.
	bus.Close()

	mu.Lock()
	fmt.Printf("\n  Summary: %d passed, %d failed\n", counters["passed"], counters["failed"])
	mu.Unlock()
}

func printResults(results []evals.EvalResult) {
	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %s — %s\n", status, r.EvalID, r.Explanation)
	}
	if len(results) == 0 {
		fmt.Println("  (no evals matched the trigger)")
	}
}
