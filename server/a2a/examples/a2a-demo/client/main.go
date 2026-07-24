// Command client discovers and calls the demo A2A agent.
//
// Usage:
//
//	# Start the server first:
//	go run ./examples/a2a-demo/server
//
//	# Then in another terminal:
//	go run ./examples/a2a-demo/client
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
)

func main() {
	ctx := context.Background()
	client := a2a.NewClient("http://localhost:9999")

	// Discover agent capabilities.
	card, err := client.Discover(ctx)
	if err != nil {
		log.Fatalf("Discover: %v", err)
	}

	fmt.Printf("Agent: %s\n", card.Name)
	fmt.Printf("Description: %s\n", card.Description)
	fmt.Printf("Skills: %d\n", len(card.Skills))
	for _, s := range card.Skills {
		fmt.Printf("  - %s: %s\n", s.ID, s.Description)
	}
	fmt.Println()

	// Send a message.
	text := "What is the capital of France?"
	fmt.Printf("User: %s\n", text)

	task, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{{Text: &text}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
	if err != nil {
		log.Fatalf("SendMessage: %v", err)
	}

	fmt.Printf("Task state: %s\n", task.Status.State)

	// Print response from artifacts.
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			if part.Text != nil {
				fmt.Printf("Agent: %s\n", strings.TrimSpace(*part.Text))
			}
		}
	}
}
