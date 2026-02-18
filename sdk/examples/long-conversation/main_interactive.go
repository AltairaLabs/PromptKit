// Package main demonstrates long conversation context management.
//
// This example shows how to use the three-tier RAG context system:
//   - Hot window (WithContextWindow): load only recent messages
//   - Semantic retrieval (WithContextRetrieval): find relevant older messages
//   - Auto-summarization (WithAutoSummarize): compress old turns
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	ctx := context.Background()

	// 1. Create an in-memory state store for persistence.
	//    In production, use statestore.NewRedisStore() for distributed state.
	store := statestore.NewMemoryStore()

	// 2. Create an embedding provider for semantic retrieval.
	embProvider, err := openai.NewEmbeddingProvider()
	if err != nil {
		log.Fatalf("Failed to create embedding provider: %v", err)
	}

	// 3. Create a provider for auto-summarization.
	//    Using gpt-4o-mini keeps summarization fast and cheap.
	summaryProvider := openai.NewProvider(
		"summarizer",
		"gpt-4o-mini",
		"https://api.openai.com/v1",
		providers.ProviderDefaults{MaxTokens: 1024},
		false,
	)

	// 4. Open a conversation with all three context tiers.
	conv, err := sdk.Open("./assistant.pack.json", "assistant",
		sdk.WithStateStore(store),
		sdk.WithConversationID("demo-session-1"),
		sdk.WithContextWindow(4),                    // Hot window: last 4 messages
		sdk.WithContextRetrieval(embProvider, 3),     // Retrieve top 3 relevant older messages
		sdk.WithAutoSummarize(summaryProvider, 6, 4), // Summarize when >6 messages, batch of 4
	)
	if err != nil {
		log.Fatalf("Failed to open conversation: %v", err)
	}
	defer conv.Close()

	// 5. Simulate a multi-topic support conversation.
	//    The conversation spans billing, login issues, and email updates.
	//    With a hot window of 4, early messages scroll out — but semantic
	//    retrieval should find them when the topic is revisited.
	messages := []string{
		"Hi, my name is Alice and my account ID is AC-9182.",
		"I was charged $49.99 on December 15th but I canceled on December 10th.",
		"Can you check the status of my refund request REF-20241215?",
		"Also, I'm having trouble logging in on my phone. It says 'session expired'.",
		"The mobile app version is 3.2.1 on iOS 17.",
		"Going back to the billing issue — has the $49.99 been reversed yet?",
		"Actually, can you also update my email to alice.new@example.com?",
		"Remind me — what was the exact dollar amount I was incorrectly charged?",
	}

	fmt.Println("=== Long Conversation Context Demo ===")
	fmt.Println()

	for i, msg := range messages {
		fmt.Printf("--- Turn %d ---\n", i+1)
		fmt.Printf("User: %s\n", msg)

		resp, err := conv.Send(ctx, msg)
		if err != nil {
			log.Fatalf("Turn %d failed: %v", i+1, err)
		}

		text := resp.Text()
		if len(text) > 200 {
			text = text[:200] + "..."
		}
		fmt.Printf("Assistant: %s\n\n", text)
	}

	// 6. Show context management stats from the state store.
	fmt.Println("=== Context Management Stats ===")
	fmt.Println()

	msgCount, _ := store.MessageCount(ctx, "demo-session-1")
	summaries, _ := store.LoadSummaries(ctx, "demo-session-1")
	fmt.Printf("Messages in store:  %d\n", msgCount)
	fmt.Printf("Summaries created:  %d\n", len(summaries))
	for i, s := range summaries {
		fmt.Printf("  Summary %d (turns %d-%d): %s\n", i+1, s.StartTurn, s.EndTurn, truncate(s.Content, 120))
	}

	fmt.Println()
	fmt.Printf("Context window:     4 messages (only last 4 loaded per turn)\n")
	fmt.Printf("Retrieval top-K:    3 (up to 3 relevant older messages retrieved via embeddings)\n")
	fmt.Printf("Summarize trigger:  after 6 messages, batch size 4\n")
	fmt.Println()
	fmt.Println("On turn 8, the user asked about the billing amount ($49.99) from turn 2.")
	fmt.Println("With only a 4-message hot window, turn 2 is not in recent history.")
	fmt.Println("Semantic retrieval finds it via embedding similarity and includes it in context.")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
