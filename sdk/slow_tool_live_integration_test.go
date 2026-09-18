//go:build integration

package sdk_test

// Live coverage for #2017: a tool call slower than the pipeline's DEFAULT idle
// timeout (30s) must not cancel its own turn.
//
// The mock-provider tests prove the mechanism. This proves the thing a
// consumer actually hits: a real model, a real tool round-trip, and the
// default configuration — no WithIdleTimeout, no shortened window. Before the
// fix this turn came back empty after ~30s with the tool result stranded.
//
// Deliberately cheap: one small model, one tool, two provider calls. The cost
// is wall-clock (the tool sleeps), not tokens.
//
// Run:
//
//	ANTHROPIC_API_KEY=... go test -tags integration ./sdk/ -run TestLive_SlowTool -v

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
)

// slowToolLivePackJSON declares one tool with a generous TimeoutMs, so the
// tool's own deadline is not what bounds this turn.
const slowToolLivePackJSON = `{
	"id": "live-slow-tool",
	"version": "1.0.0",
	"description": "Live pack with one slow tool",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You run builds. When asked to build something, call run_build with the target, then report its status in one short sentence.",
			"tools": ["run_build"]
		}
	},
	"tools": {
		"run_build": {
			"name": "run_build",
			"description": "Run the project build for a target and return its status",
			"mode": "local",
			"timeout_ms": 120000,
			"parameters": {
				"type": "object",
				"properties": {
					"target": {"type": "string", "description": "Build target"}
				},
				"required": ["target"]
			}
		}
	}
}`

// slowToolLiveDelay exceeds DefaultIdleTimeoutSeconds (30s) so the default
// configuration is what is under test. Override to shorten a local run — but a
// value under 30s tests nothing.
func slowToolLiveDelay(t *testing.T) time.Duration {
	t.Helper()
	if v := os.Getenv("SLOW_TOOL_DELAY"); v != "" {
		d, err := time.ParseDuration(v)
		require.NoError(t, err, "SLOW_TOOL_DELAY")
		return d
	}
	return 35 * time.Second
}

func TestLive_SlowToolSurvivesDefaultIdleTimeout(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	provider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID:       "claude-live",
		Type:     "claude",
		Model:    envOrLive("SLOW_TOOL_MODEL", "claude-haiku-4-5"),
		BaseURL:  "https://api.anthropic.com/v1",
		Defaults: providers.ProviderDefaults{MaxTokens: 512},
	})
	require.NoError(t, err)

	dir := t.TempDir()
	packPath := dir + "/slow-tool.pack.json"
	require.NoError(t, os.WriteFile(packPath, []byte(slowToolLivePackJSON), 0o644))

	// No WithIdleTimeout and no WithExecutionTimeout: the defaults are the
	// point. The 30s idle window is what used to kill this turn.
	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	delay := slowToolLiveDelay(t)

	var mu sync.Mutex
	var ran bool
	conv.OnTool("run_build", func(args map[string]any) (any, error) {
		time.Sleep(delay)
		mu.Lock()
		ran = true
		mu.Unlock()
		return map[string]any{"target": args["target"], "status": "passed", "tests": 412}, nil
	})

	start := time.Now()
	resp, err := conv.Send(context.Background(), "Build the ./... target and tell me how it went.")
	elapsed := time.Since(start)

	require.NoError(t, err, "turn failed after %s", elapsed)

	mu.Lock()
	toolRan := ran
	mu.Unlock()
	require.True(t, toolRan, "the tool never ran, so this test proves nothing")

	assert.Greater(t, elapsed, delay,
		"the turn returned before the tool could finish")
	assert.NotEmpty(t, strings.TrimSpace(resp.Text()),
		"empty answer: the tool result never reached the model — the idle timer cut the round short")
	t.Logf("turn completed in %s with tool delay %s: %q", elapsed, delay, resp.Text())
}
