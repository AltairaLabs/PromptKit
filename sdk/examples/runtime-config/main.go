// Package main demonstrates RuntimeConfig with external tools, evals, and hooks.
//
// This example shows:
//   - Declarative SDK setup via sdk.WithRuntimeConfig() (one line replaces 50+ lines of Go)
//   - Exec tools: a Python sentiment analysis tool bound via RuntimeConfig YAML
//   - Exec evals: Python-based tone and quality checks, evaluated with sdk.Evaluate()
//   - Exec hooks: a content policy filter (provider/filter) and audit logger (session/observe)
//   - The pack defines tool/eval names; RuntimeConfig binds them to Python scripts
//
// The pack is platform-agnostic — it never knows whether tools/evals are Go, Python, or HTTP.
// The RuntimeConfig is environment-specific — swap runtime.yaml per environment.
//
// Run with:
//
//	go run .
//
// No API keys needed — uses a mock provider.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	ctx := context.Background()

	// ── Part 1: Open a conversation with RuntimeConfig ──────────────
	//
	// WithRuntimeConfig loads runtime.yaml which configures:
	//   - Mock provider (no API keys)
	//   - Exec tool: sentiment_analysis → tools/sentiment.py
	//   - Exec evals: tone_check, response_quality → Python scripts
	//   - Exec hooks: content_policy (filter), audit_logger (observe)
	//
	// The pack (agent.pack.json) defines the tool schema, eval types, and
	// system prompt. It never references Python, file paths, or credentials.
	fmt.Println("=== Part 1: Conversation with RuntimeConfig ===")
	fmt.Println()

	conv, err := sdk.Open("./agent.pack.json", "support",
		sdk.WithRuntimeConfig("./runtime.yaml"),
		sdk.WithProvider(mock.NewProvider("mock", "mock-model", false)),
	)
	if err != nil {
		log.Fatalf("Failed to open: %v", err)
	}

	// Set template variables
	conv.SetVar("company_name", "Acme Corp")

	// Send a normal message (mock provider returns a canned response)
	resp, err := conv.Send(ctx, "Hi, I need help with my order")
	if err != nil {
		log.Fatalf("Send failed: %v", err)
	}
	fmt.Printf("Assistant: %s\n", resp.Text())

	// Send another message
	resp, err = conv.Send(ctx, "My order #12345 hasn't arrived yet")
	if err != nil {
		// Check if a hook blocked the response
		var hookErr *hooks.HookDeniedError
		if errors.As(err, &hookErr) {
			fmt.Printf("Blocked by %s: %s\n", hookErr.HookName, hookErr.Reason)
		} else {
			log.Fatalf("Send failed: %v", err)
		}
	} else {
		fmt.Printf("Assistant: %s\n", resp.Text())
	}

	conv.Close()

	// ── Part 2: Standalone evals with exec eval handlers ────────────
	//
	// sdk.Evaluate() runs evals from the pack against a conversation
	// snapshot. The tone_check and response_quality eval types are bound
	// to Python scripts via RuntimeConfig — the eval runner spawns them
	// as subprocesses, completely transparent to the pack.
	fmt.Println()
	fmt.Println("=== Part 2: Standalone Evals (exec eval handlers) ===")
	fmt.Println()

	// Simulate a conversation to evaluate
	messages := []types.Message{
		types.NewUserMessage("I'm really frustrated with your service!"),
		types.NewAssistantMessage(
			"I understand your frustration and I sincerely apologize for the inconvenience. " +
				"Let me help resolve this issue right away. Could you please share your order number " +
				"so I can look into what happened?",
		),
	}

	results, err := sdk.Evaluate(ctx, sdk.EvaluateOpts{
		PackPath:             "./agent.pack.json",
		SkipSchemaValidation: true,
		RuntimeConfigPath:    "./runtime.yaml",
		Messages:             messages,
		SessionID:            "eval-demo",
		TurnIndex:            1,
	})
	if err != nil {
		log.Fatalf("Evaluate failed: %v", err)
	}

	for _, r := range results {
		status := "PASS"
		if r.Score != nil && *r.Score < 0.5 {
			status = "FAIL"
		}
		scoreStr := "n/a"
		if r.Score != nil {
			scoreStr = fmt.Sprintf("%.2f", *r.Score)
		}
		fmt.Printf("  [%s] %-20s score=%s  %s\n", status, r.EvalID, scoreStr, r.Explanation)
	}

	// ── Part 3: Eval a poor-quality response ────────────────────────
	//
	// The response_quality eval should flag this as too short.
	fmt.Println()
	fmt.Println("=== Part 3: Eval a poor response ===")
	fmt.Println()

	poorMessages := []types.Message{
		types.NewUserMessage("Can you help me with my account?"),
		types.NewAssistantMessage("No."),
	}

	results, err = sdk.Evaluate(ctx, sdk.EvaluateOpts{
		PackPath:             "./agent.pack.json",
		SkipSchemaValidation: true,
		RuntimeConfigPath:    "./runtime.yaml",
		Messages:             poorMessages,
		SessionID:            "eval-demo-poor",
		TurnIndex:            1,
	})
	if err != nil {
		log.Fatalf("Evaluate failed: %v", err)
	}

	for _, r := range results {
		status := "PASS"
		if r.Score != nil && *r.Score < 0.5 {
			status = "FAIL"
		}
		scoreStr := "n/a"
		if r.Score != nil {
			scoreStr = fmt.Sprintf("%.2f", *r.Score)
		}
		fmt.Printf("  [%s] %-20s score=%s  %s\n", status, r.EvalID, scoreStr, r.Explanation)
	}

	// ── Part 4: Inline eval definitions with exec handlers ──────────
	//
	// You can also define evals inline (no pack needed) and still use
	// exec eval handlers from RuntimeConfig.
	fmt.Println()
	fmt.Println("=== Part 4: Inline evals with exec handlers ===")
	fmt.Println()

	inlineDefs := []evals.EvalDef{
		{
			ID:      "custom-tone",
			Type:    "tone_check",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"expected_tone": "professional"},
		},
		{
			ID:      "has-greeting",
			Type:    "contains",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{"patterns": []any{"hello", "hi", "welcome"}},
		},
	}

	greetingMessages := []types.Message{
		types.NewUserMessage("Hello!"),
		types.NewAssistantMessage(
			"Hello! Welcome to Acme Corp support. I'm happy to assist you today. " +
				"How can I help?",
		),
	}

	results, err = sdk.Evaluate(ctx, sdk.EvaluateOpts{
		EvalDefs:          inlineDefs,
		RuntimeConfigPath: "./runtime.yaml",
		Messages:          greetingMessages,
	})
	if err != nil {
		log.Fatalf("Evaluate failed: %v", err)
	}

	for _, r := range results {
		status := "PASS"
		if r.Score != nil && *r.Score < 0.5 {
			status = "FAIL"
		}
		scoreStr := "n/a"
		if r.Score != nil {
			scoreStr = fmt.Sprintf("%.2f", *r.Score)
		}
		fmt.Printf("  [%s] %-20s score=%s  %s\n", status, r.EvalID, scoreStr, r.Explanation)
	}
}
