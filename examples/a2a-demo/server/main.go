// Command server exposes a PromptKit agent as an A2A-compliant server.
//
// Usage:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/a2a-demo/server
//
// The server listens on port 9999 and serves an agent card at
// http://localhost:9999/.well-known/agent.json.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	opener := sdk.A2AOpener("../assistant.pack.json", "chat")

	card := a2a.AgentCard{
		Name:               "Demo Assistant",
		Description:        "A helpful assistant exposed via A2A",
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []a2a.AgentSkill{
			{
				ID:          "general_qa",
				Name:        "General Q&A",
				Description: "Answer general knowledge questions",
			},
		},
	}

	server := a2a.NewServer(opener,
		a2a.WithCard(&card),
		a2a.WithPort(9999),
	)

	// Handle graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		os.Exit(0)
	}()

	fmt.Println("A2A server listening on http://localhost:9999")
	fmt.Println("Agent card: http://localhost:9999/.well-known/agent.json")
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
