// Package main demonstrates LLM-initiated workflow transitions using the SDK.
//
// This example opens a WorkflowConversation against the support.pack.json
// pack and lets the LLM decide when to transition between states by calling
// the workflow__transition tool.
//
// Run:
//
//	export OPENAI_API_KEY=sk-...   # or ANTHROPIC_API_KEY / GEMINI_API_KEY
//	go run .
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	wc, err := sdk.OpenWorkflow("../support.pack.json",
		sdk.WithContextCarryForward(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening workflow: %v\n", err)
		os.Exit(1)
	}
	defer wc.Close()

	fmt.Println("Customer Support Workflow (SDK Example)")
	fmt.Println("========================================")
	fmt.Printf("Starting state: %s\n\n", wc.CurrentState())

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("[%s] You: ", wc.CurrentState())
		if !scanner.Scan() {
			break
		}
		input := scanner.Text()
		if input == "" {
			continue
		}

		prevState := wc.CurrentState()
		resp, err := wc.Send(context.Background(), input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}

		fmt.Printf("[%s] Agent: %s\n", wc.CurrentState(), resp.Text())

		if wc.CurrentState() != prevState {
			fmt.Printf("  → transitioned to %s\n", wc.CurrentState())
		}
		if wc.IsComplete() {
			fmt.Println("  → workflow complete")
			break
		}
	}
}
