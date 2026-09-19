package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
)

// slowToolPackJSON declares a tool with a long TimeoutMs, so the tool's own
// deadline is not what this test is measuring.
const slowToolPackJSON = `{
	"id": "integration-test-slow-tool",
	"version": "1.0.0",
	"description": "Pack with a slow tool",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant with tools.",
			"tools": ["slow_build"]
		}
	},
	"tools": {
		"slow_build": {
			"name": "slow_build",
			"description": "Runs a build that takes a while",
			"mode": "local",
			"timeout_ms": 60000,
			"parameters": {
				"type": "object",
				"properties": {
					"target": {"type": "string"}
				},
				"required": ["target"]
			}
		}
	}
}`

// slowToolProvider scripts one tool call followed by a text answer.
func slowToolProvider(t *testing.T) *mock.ToolProvider {
	t.Helper()
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Running the build",
		ToolCalls: []mock.ToolCall{{
			Name:      "slow_build",
			Arguments: map[string]interface{}{"target": "./..."},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "The build passed.",
	})
	return mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
}

// TestSlowTool_SurvivesIdleTimeout_EndToEnd is #2017 through the whole SDK
// path: a real pipeline, a real tool round-trip, and a tool that takes several
// times the idle window. Before the fix the idle timer cancelled the context
// the tool was running in, whatever ExecutionTimeout the caller had set, and
// the turn came back empty — the tool ran, but its result never reached the
// model for the round that would have answered.
func TestSlowTool_SurvivesIdleTimeout_EndToEnd(t *testing.T) {
	const (
		idle      = 100 * time.Millisecond
		toolDelay = 400 * time.Millisecond // 4x the idle window
	)

	var mu sync.Mutex
	var toolRan bool

	packPath := writePackFile(t, slowToolPackJSON)
	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(slowToolProvider(t)),
		sdk.WithSkipSchemaValidation(),
		sdk.WithIdleTimeout(idle),
		sdk.WithExecutionTimeout(30*time.Second),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	conv.OnTool("slow_build", func(args map[string]any) (any, error) {
		time.Sleep(toolDelay)
		mu.Lock()
		toolRan = true
		mu.Unlock()
		return map[string]any{"target": args["target"], "status": "ok"}, nil
	})

	resp, err := conv.Send(context.Background(), "Build the project")
	require.NoError(t, err, "a tool slower than the idle window must not fail its own turn")

	mu.Lock()
	ran := toolRan
	mu.Unlock()
	assert.True(t, ran, "the tool handler never ran")
	assert.Contains(t, resp.Text(), "build passed",
		"the tool result never reached the model; the round was cut short mid-flight")
}
