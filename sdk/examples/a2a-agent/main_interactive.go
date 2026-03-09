// Package main demonstrates connecting to A2A (Agent-to-Agent) remote agents via the SDK.
//
// This example shows:
//   - Builder pattern with NewA2AAgent for auth, headers, timeouts, retry, and skill filters
//   - Bearer token authentication
//   - Skill filtering (expose only "echo", hide "reverse")
//
// Run with:
//
//	# First start the echo server (from examples/a2a-auth-test):
//	go run ./examples/a2a-auth-test/server
//
//	# Then run this example:
//	export OPENAI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	// Configure the A2A agent with authentication and skill filtering.
	// The echo server at localhost:9877 requires Bearer token auth and
	// exposes two skills: "echo" and "reverse". We only expose "echo".
	agentURL := envOrDefault("ECHO_AGENT_URL", "http://localhost:9877")
	agentToken := envOrDefault("ECHO_AGENT_TOKEN", "test-token-123")

	echoAgent := sdk.NewA2AAgent(agentURL).
		WithAuth("Bearer", agentToken).
		WithHeader("X-Request-Source", "sdk-example").
		WithTimeout(5000).
		WithRetryPolicy(3, 100, 2000). // 3 retries, 100ms initial, 2s max
		WithSkillFilter(&tools.A2ASkillFilter{
			Allowlist: []string{"echo"},
		})

	conv, err := sdk.Open("./a2a-agent.pack.json", "assistant",
		sdk.WithA2AAgent(echoAgent),
	)
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// The LLM can now call a2a__echo_agent__echo (but not reverse).
	ctx := context.Background()
	resp, err := conv.Send(ctx, "Please echo: Hello from the SDK!")
	if err != nil {
		log.Fatalf("Send failed: %v", err)
	}
	fmt.Println("Response:", resp.Text())
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
