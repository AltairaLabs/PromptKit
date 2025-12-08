// Package main demonstrates the simplest PromptKit SDK usage.
//
// This example shows:
//   - Opening a conversation from a pack file
//   - Sending a simple message
//   - Reading the response
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
	// Open a conversation from a pack file
	// The pack file defines the system prompt and configuration
	conv, err := sdk.Open("./hello.pack.json", "chat")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// Set template variables (optional)
	conv.SetVar("user_name", "World")

	// Send a message and get a response
	ctx := context.Background()
	resp, err := conv.Send(ctx, "Say hello!")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	// Print the response
	fmt.Println(resp.Text())

	// Send a follow-up (maintains conversation context)
	resp, err = conv.Send(ctx, "What's my name?")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}
	fmt.Println(resp.Text())
}
