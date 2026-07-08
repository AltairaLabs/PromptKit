package statestore_test

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ExampleMemoryStore shows saving and loading conversation state with the
// in-memory Store implementation — suitable for development, testing, and
// single-instance deployments; no external database required.
func ExampleMemoryStore() {
	store := statestore.NewMemoryStore(statestore.WithNoTTL())
	defer store.Close()

	ctx := context.Background()
	_ = store.Save(ctx, &statestore.ConversationState{
		ID:       "conv-1",
		Messages: []types.Message{types.NewTextMessage("user", "hi")},
	})

	state, err := store.Load(ctx, "conv-1")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(state.Messages[0].Content)
	// Output: hi
}
