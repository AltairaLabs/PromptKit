// Package main demonstrates RFC 0011 (Workflow States as Agents) using the SDK.
//
// It loads the pack compiled by packc from this directory's parent PromptArena
// example (../workflow-agent-loops.pack.json) and opens it as a multi-agent
// session. The `researcher` agent declares `state: research`, so it is opened
// as the pack's workflow pipeline ENTERED AT the `research` state, while
// `summarizer` is a plain single-prompt agent (RFC 0007). The only config
// difference between them is the one line `state: research`.
//
// The pack is the exact output of:
//
//	packc compile -c ../config.arena.yaml -o ../workflow-agent-loops.pack.json
//
// Run:
//
//	go run .
//
// It uses a built-in mock provider (no API keys). The mock returns a fixed
// reply, which is enough to show the wiring: the researcher is a workflow-backed
// pipeline sitting at its state, the summarizer is a plain conversation. With a
// real provider the researcher would drive the full research → summarize → done
// loop, exactly as running the workflow directly does.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	repo := mock.NewInMemoryMockRepository("(mock reply — set an API key for live LLM behaviour)")
	prov := mock.NewProviderWithRepository("mock", "mock-model", false, repo)

	sess, err := sdk.OpenMultiAgent("../workflow-agent-loops.pack.json",
		sdk.WithProvider(prov),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open multi-agent: %v\n", err)
		os.Exit(1)
	}
	defer sess.Close()

	fmt.Println("RFC 0011 — Workflow States as Agents")
	fmt.Println("=====================================")
	fmt.Println("Loaded ../workflow-agent-loops.pack.json (compiled from the PromptArena example).")
	fmt.Println()

	describe("research (entry)", sess.Entry())
	for name, agent := range sess.Members() {
		describe(name, agent)
	}

	fmt.Println("\nInvoking the state-backed researcher agent…")
	resp, err := sess.Send(context.Background(), "Research the PromptKit hook interfaces.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "send: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  reply: %s\n", resp.Text())
	if wc, ok := sess.Entry().(*sdk.WorkflowConversation); ok {
		fmt.Printf("  researcher is running the workflow — current state: %q\n", wc.CurrentState())
	}
}

// describe prints whether an agent is workflow-backed (RFC 0011) or a plain
// single-prompt agent (RFC 0007), by inspecting its concrete pipeline type.
func describe(name string, a sdk.Agent) {
	if wc, ok := a.(*sdk.WorkflowConversation); ok {
		fmt.Printf("- %-18s state-backed → workflow pipeline entered at state %q\n", name, wc.CurrentState())
		return
	}
	fmt.Printf("- %-18s plain single-prompt agent (RFC 0007)\n", name)
}
