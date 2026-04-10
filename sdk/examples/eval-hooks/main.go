// Package main demonstrates custom EvalHook implementations with the
// PromptKit SDK.
//
// Two hooks are registered:
//
//  1. countingHook — a pure-Go observer that tallies results by eval ID
//     and prints a summary at shutdown. Shows the simplest possible
//     EvalHook shape.
//
//  2. ExecEvalHook — spawns an external subprocess per eval result and
//     writes the JSON-encoded result to its stdin. The example ships
//     with a minimal bash consumer (log-result.sh) that logs each
//     result and appends it to eval-results.ndjson. Shows how to
//     integrate with external systems (datastores, dashboards,
//     pipelines) without writing Go glue code.
//
// Run it:
//
//	go run .
//
// After running, inspect ./eval-results.ndjson to see each result.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register built-in eval handlers
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

const packPath = "./assistant.pack.json"

// ---------------------------------------------------------------------------
// countingHook — minimal in-process EvalHook.
// ---------------------------------------------------------------------------

type countingHook struct {
	mu     sync.Mutex
	counts map[string]int
}

func newCountingHook() *countingHook {
	return &countingHook{counts: map[string]int{}}
}

func (c *countingHook) Name() string { return "counter" }

func (c *countingHook) OnEvalResult(
	_ context.Context, def *evals.EvalDef, _ *evals.EvalContext, result *evals.EvalResult,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := def.ID
	if result.Error != "" {
		key += " (error)"
	} else if result.Skipped {
		key += " (skipped)"
	}
	c.counts[key]++
}

func (c *countingHook) Print() {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]string, 0, len(c.counts))
	for k := range c.counts {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	fmt.Println("\n=== Counting hook summary ===")
	for _, id := range ids {
		fmt.Printf("  %-30s %d\n", id, c.counts[id])
	}
}

// ---------------------------------------------------------------------------
// ExecEvalHook — shells out to an external command, piping the eval
// result as JSON on stdin. Fire-and-forget: stdout is ignored and
// errors are logged but do not propagate.
// ---------------------------------------------------------------------------

type ExecEvalHook struct {
	name    string
	command string
	args    []string
	timeout time.Duration
}

func NewExecEvalHook(name, command string, args ...string) *ExecEvalHook {
	return &ExecEvalHook{
		name:    name,
		command: command,
		args:    args,
		timeout: 5 * time.Second,
	}
}

func (e *ExecEvalHook) Name() string { return e.name }

func (e *ExecEvalHook) OnEvalResult(
	ctx context.Context, _ *evals.EvalDef, _ *evals.EvalContext, result *evals.EvalResult,
) {
	payload, err := json.Marshal(result)
	if err != nil {
		log.Printf("[%s] marshal error: %v", e.name, err)
		return
	}

	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, e.command, e.args...) //#nosec G204 -- example
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		log.Printf("[%s] exec error: %v (stderr: %s)", e.name, err, bytes.TrimSpace(stderr.Bytes()))
		return
	}
	if stderr.Len() > 0 {
		// Let the script surface its one-line summary to the user.
		fmt.Fprint(os.Stderr, stderr.String())
	}
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	fmt.Println("=== SDK Eval Hooks Example ===")

	// Resolve the bash script next to main.go, regardless of cwd.
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("executable path: %v", err)
	}
	// When run via `go run .`, os.Executable() points at a temp build.
	// Fall back to cwd if the script isn't next to the binary.
	scriptPath := filepath.Join(filepath.Dir(exe), "log-result.sh")
	if _, statErr := os.Stat(scriptPath); statErr != nil {
		scriptPath = "./log-result.sh"
	}

	counter := newCountingHook()
	exec := NewExecEvalHook("bash-logger", "bash", scriptPath)

	// Mock provider with a canned response — no API key needed. Every
	// turn returns the same JSON body, which is sufficient to drive
	// the eval hooks since we're demonstrating hook invocation, not
	// model quality.
	repo := mock.NewInMemoryMockRepository(`{"response": "This is a demo reply."}`)
	provider := mock.NewProviderWithRepository("mock", "mock-model", false, repo)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithEvalHook(counter),
		sdk.WithEvalHook(exec),
	)
	if err != nil {
		log.Fatalf("open: %v", err)
	}

	ctx := context.Background()
	for i, q := range []string{
		"What is the capital of France?",
		"What is 2 + 2?",
		"List three programming languages.",
	} {
		fmt.Printf("\nTurn %d: %s\n", i+1, q)
		resp, sendErr := conv.Send(ctx, q)
		if sendErr != nil {
			log.Printf("  send error: %v", sendErr)
			continue
		}
		fmt.Printf("  -> %s\n", resp.Text())
	}

	// Close triggers session-level evals and flushes async turn hooks.
	conv.Close()

	counter.Print()
	fmt.Println("\nRaw results appended to ./eval-results.ndjson")
}
