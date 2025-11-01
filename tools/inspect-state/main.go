package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func main() {
	// Create a memory store (this won't have the actual runtime data,
	// but we can show how to inspect it programmatically)
	store := statestore.NewMemoryStore()

	// In a real scenario, the store would be populated by the engine
	// For now, let's show how to query it
	ctx := context.Background()

	// List all conversations
	conversationIDs, err := store.List(ctx, statestore.ListOptions{
		Limit: 100,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing states: %v\n", err)
		os.Exit(1)
	}

	if len(conversationIDs) == 0 {
		fmt.Println("No conversation states found in memory store.")
		fmt.Println("\nNote: MemoryStore is ephemeral - state only exists during engine runtime.")
		fmt.Println("To persist state across runs, configure Redis in arena.yaml:")
		fmt.Println()
		fmt.Println("state_store:")
		fmt.Println("  type: redis")
		fmt.Println("  redis:")
		fmt.Println("    address: localhost:6379")
		fmt.Println("    password: ''")
		fmt.Println("    db: 0")
		return
	}

	fmt.Printf("Found %d conversation state(s):\n\n", len(conversationIDs))

	for i, conversationID := range conversationIDs {
		state, err := store.Load(ctx, conversationID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading state %s: %v\n", conversationID, err)
			continue
		}

		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling state %d: %v\n", i, err)
			continue
		}
		fmt.Printf("=== Conversation %d: %s ===\n", i+1, state.ID)
		fmt.Println(string(data))
		fmt.Println()
	}
}
