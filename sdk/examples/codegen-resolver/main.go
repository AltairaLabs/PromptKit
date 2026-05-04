// Package main demonstrates the SDK's MCP endpoint resolver pattern.
//
// The pack declares an MCP server by name only ("codegen"); a host-supplied
// resolver fills in URL/Headers at conversation open. This example wears
// both hats — it spins up a codegen-sandbox container, points a
// StaticMCPEndpointResolver at it, then opens a conversation and asks
// Claude Sonnet to write a small Go module.
//
// In production the resolver would talk to your sandbox pool / service
// discovery / Omnia. The pack/agent layer stays oblivious to provisioning.
//
// Run:
//
//	export ANTHROPIC_API_KEY=...
//	docker pull ghcr.io/altairalabs/codegen-sandbox:latest
//	go run .
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/sdk"
)

const (
	sandboxImage    = "ghcr.io/altairalabs/codegen-sandbox:latest"
	sandboxPort     = 8080
	healthBudget    = 30 * time.Second
	healthInterval  = 250 * time.Millisecond
	conversationCtx = 5 * time.Minute
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY not set")
	}

	// 1. Provision a sandbox. In prod, your resolver would call a pool
	//    API; here we shell out to docker for a self-contained demo.
	bgCtx := context.Background()
	hostPort, err := pickFreePort(bgCtx)
	if err != nil {
		log.Fatalf("pick port: %v", err)
	}
	containerID, err := startSandbox(bgCtx, hostPort)
	if err != nil {
		log.Fatalf("start sandbox: %v", err)
	}
	defer stopSandbox(containerID)

	sandboxURL := fmt.Sprintf("http://localhost:%d", hostPort)
	if err := waitForSSE(bgCtx, sandboxURL); err != nil {
		log.Fatalf("sandbox not ready: %v", err)
	}
	log.Printf("sandbox ready: %s (container %s)", sandboxURL, containerID[:12])

	// 2. Wire the resolver. Pack/agent code never sees this URL.
	resolver := &sdk.StaticMCPEndpointResolver{URL: sandboxURL}

	conv, err := sdk.Open("./codegen-resolver.pack.json", "codegen",
		sdk.WithModel("claude-sonnet-4-6"),
		sdk.WithAPIKey(apiKey),
		sdk.WithMCPServer(sdk.NewMCPServerByName("codegen")),
		sdk.WithMCPEndpoints(resolver),
	)
	if err != nil {
		log.Fatalf("open conversation: %v", err)
	}
	defer conv.Close()

	// 3. Ask the agent to do something real. It uses Bash/Write/run_tests
	//    via the MCP server we just resolved — tool calls round-trip to
	//    the running container.
	ctx, cancel := context.WithTimeout(context.Background(), conversationCtx)
	defer cancel()

	task := strings.TrimSpace(`
Inside /workspace, set up a Go module called example.com/reverse and
implement:

    func Reverse(s string) string

The function must reverse a Unicode string by runes (not bytes), so
"héllo" reverses to "olléh". Add a table-driven test (reverse_test.go)
covering ASCII, accented characters, an empty string, and a
mixed-case word. Run the tests with mcp__codegen__run_tests and only
declare done once they pass.
`)

	resp, err := conv.Send(ctx, task)
	if err != nil {
		log.Fatalf("send: %v", err)
	}

	fmt.Println("=== Agent reply ===")
	fmt.Println(resp.Text())
}

func pickFreePort(ctx context.Context) (int, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func startSandbox(ctx context.Context, hostPort int) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "run", "-d", "--rm",
		"-p", fmt.Sprintf("%d:%d", hostPort, sandboxPort),
		"-e", "DEV_MODE=1",
		sandboxImage,
	).Output()
	if err != nil {
		return "", fmt.Errorf("docker run: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func stopSandbox(containerID string) {
	if containerID == "" {
		return
	}
	// Use a fresh background context; the parent ctx may already be done by
	// the time we tear the container down (e.g. user canceled mid-run).
	if err := exec.CommandContext(context.Background(), "docker", "stop", containerID).Run(); err != nil {
		log.Printf("warning: docker stop %s: %v", containerID[:12], err)
	}
}

func waitForSSE(ctx context.Context, baseURL string) error {
	deadline := time.Now().Add(healthBudget)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/sse", http.NoBody)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}
		time.Sleep(healthInterval)
	}
	return fmt.Errorf("sandbox not ready within %s", healthBudget)
}
