// Command tools demonstrates using a remote A2A agent as a tool
// within an SDK conversation.
//
// Usage:
//
//	# Start the server first:
//	go run ./examples/a2a-demo/server
//
//	# Then in another terminal:
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/a2a-demo/tools
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	ctx := context.Background()

	// Create a client pointing at the demo A2A server.
	client := a2a.NewClient("http://localhost:9999")

	// Bridge the remote agent's skills into tool descriptors.
	bridge := a2a.NewToolBridge(client)
	tools, err := bridge.RegisterAgent(ctx)
	if err != nil {
		log.Fatalf("RegisterAgent: %v", err)
	}
	fmt.Printf("Registered %d A2A tools\n", len(tools))
	for _, t := range tools {
		fmt.Printf("  - %s: %s\n", t.Name, t.Description)
	}
	fmt.Println()

	// Open a conversation with the A2A tools available.
	conv, err := sdk.Open("../assistant.pack.json", "chat",
		sdk.WithA2ATools(bridge),
	)
	if err != nil {
		log.Fatalf("Open: %v", err)
	}
	defer conv.Close()

	// Send a message that should trigger the A2A tool.
	resp, err := conv.Send(ctx, "Use the demo assistant tool to answer: What is quantum computing?")
	if err != nil {
		log.Fatalf("Send: %v", err)
	}

	fmt.Printf("Response: %s\n", resp.Text())
}
