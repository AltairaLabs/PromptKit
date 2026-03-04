// Package main demonstrates client-side tool execution with the PromptKit SDK.
//
// This example shows two modes:
//   - Synchronous: OnClientTool handler runs immediately when the LLM invokes the tool
//   - Deferred: No handler registered; pipeline suspends, caller provides result, then resumes
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

	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	// --- Mode 1: Synchronous handler ---
	fmt.Println("=== Synchronous Mode ===")
	syncExample()

	// --- Mode 2: Deferred (no handler) ---
	fmt.Println("\n=== Deferred Mode ===")
	deferredExample()

	// --- Mode 3: Streaming deferred ---
	fmt.Println("\n=== Streaming Deferred Mode ===")
	streamDeferredExample()
}

func syncExample() {
	conv, err := sdk.Open("./client-tools.pack.json", "assistant")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// Register a synchronous client tool handler.
	conv.OnClientTool("get_location", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		fmt.Printf("  [Client] Location requested (accuracy: %v)\n", req.Args["accuracy"])
		return map[string]any{"lat": 40.7128, "lon": -74.0060, "city": "New York"}, nil
	})

	resp, err := conv.Send(context.Background(), "Where am I right now?")
	if err != nil {
		log.Fatalf("Send failed: %v", err)
	}
	fmt.Printf("  Assistant: %s\n", resp.Text())
}

func deferredExample() {
	conv, err := sdk.Open("./client-tools.pack.json", "assistant")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// No OnClientTool handler — pipeline will suspend.
	ctx := context.Background()

	resp, err := conv.Send(ctx, "Where am I right now?")
	if err != nil {
		log.Fatalf("Send failed: %v", err)
	}

	if resp.HasPendingClientTools() {
		for _, tool := range resp.ClientTools() {
			fmt.Printf("  [Client] Tool requested: %s\n", tool.ToolName)
			fmt.Printf("  [Client] Consent message: %s\n", tool.ConsentMsg)

			// Simulate user granting consent and providing data.
			err := conv.SendToolResult(ctx, tool.CallID, map[string]any{
				"lat": 37.7749, "lon": -122.4194, "city": "San Francisco",
			})
			if err != nil {
				log.Fatalf("SendToolResult failed: %v", err)
			}
		}

		// Resume the pipeline with the provided results.
		resp, err = conv.Resume(ctx)
		if err != nil {
			log.Fatalf("Resume failed: %v", err)
		}
	}

	fmt.Printf("  Assistant: %s\n", resp.Text())
}

func streamDeferredExample() {
	conv, err := sdk.Open("./client-tools.pack.json", "assistant")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	ctx := context.Background()

	for chunk := range conv.Stream(ctx, "Where am I right now?") {
		if chunk.Error != nil {
			log.Fatalf("Stream error: %v", chunk.Error)
		}

		switch chunk.Type {
		case sdk.ChunkText:
			fmt.Print(chunk.Text)
		case sdk.ChunkClientTool:
			fmt.Printf("\n  [Client] Tool requested: %s\n", chunk.ClientTool.ToolName)

			err := conv.SendToolResult(ctx, chunk.ClientTool.CallID, map[string]any{
				"lat": 51.5074, "lon": -0.1278, "city": "London",
			})
			if err != nil {
				log.Fatalf("SendToolResult failed: %v", err)
			}

			// Resume streaming.
			fmt.Print("  Assistant: ")
			for resumeChunk := range conv.ResumeStream(ctx) {
				if resumeChunk.Error != nil {
					log.Fatalf("ResumeStream error: %v", resumeChunk.Error)
				}
				if resumeChunk.Type == sdk.ChunkText {
					fmt.Print(resumeChunk.Text)
				}
			}
			fmt.Println()
			return
		}
	}
	fmt.Println()
}
