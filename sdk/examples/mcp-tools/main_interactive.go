// Package main demonstrates connecting to MCP (Model Context Protocol) tool servers via the SDK.
//
// This example shows two patterns for MCP server configuration:
//   - Simple registration with WithMCP (name, command, args)
//   - Builder pattern with NewMCPServer for env vars, timeouts, and tool filters
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

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	// --- Pattern 1: Simple MCP server registration ---
	// All tools from the server are exposed to the LLM.
	//
	//   conv, err := sdk.Open("./mcp-tools.pack.json", "assistant",
	//       sdk.WithMCP("everything", "npx", "@modelcontextprotocol/server-everything"),
	//   )

	// --- Pattern 2: Builder pattern with timeout and tool filter ---
	// Only expose "echo" and "get-sum" out of 14 available tools.
	everything := sdk.NewMCPServer("everything", "npx", "@modelcontextprotocol/server-everything").
		WithTimeout(10000).
		WithToolFilter(&mcp.ToolFilter{
			Allowlist: []string{"echo", "get-sum"},
		})

	conv, err := sdk.Open("./mcp-tools.pack.json", "assistant",
		sdk.WithMCPServer(everything),
	)
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// The LLM can now call mcp__everything__echo and mcp__everything__get-sum.
	ctx := context.Background()
	resp, err := conv.Send(ctx, "Please echo the message: Hello from the SDK!")
	if err != nil {
		log.Fatalf("Send failed: %v", err)
	}
	fmt.Println("Response:", resp.Text())

	resp, err = conv.Send(ctx, "What is 17 + 25?")
	if err != nil {
		log.Fatalf("Send failed: %v", err)
	}
	fmt.Println("Response:", resp.Text())
}
