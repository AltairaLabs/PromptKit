package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compositionExecPackJSON is a pack whose workflow entry state uses
// orchestration: composition so the pipeline runs a CompositionStage instead
// of the normal prompt-assembly → LLM path.
//
// The composition has a single tool step that calls "echo" with the user's
// input text.  The echo tool is registered via OnTool so no LLM provider is
// needed — the CompositionStage calls the tool directly through the registry.
const compositionExecPackJSON = `{
	"schema_version": "2025.1",
	"id": "composition-exec-test",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
	"prompts": {
		"_placeholder": {"id": "_placeholder", "name": "Placeholder", "version": "1.0.0", "system_template": "unused"}
	},
	"tools": {
		"echo": {
			"description": "Echoes its input back as output.",
			"parameters": {
				"type": "object",
				"properties": {
					"value": {"type": "string"}
				},
				"required": ["value"]
			}
		}
	},
	"workflow": {
		"version": 1,
		"entry": "compose",
		"states": {
			"compose": {
				"orchestration": "composition",
				"composition": "flow",
				"terminal": true
			}
		}
	},
	"compositions": {
		"flow": {
			"version": 1,
			"steps": [
				{
					"id": "a",
					"kind": "tool",
					"tool": "echo",
					"args": {"value": "${input}"}
				}
			],
			"output": "a"
		}
	}
}`

// TestComposition_EmbeddedState_RunsViaSend verifies the end-to-end composition
// execution path:
//
//  1. A workflow entry state with orchestration: composition is opened — no
//     prompt_task, no LLM provider required.
//  2. Send forwards the user message to the CompositionStage.
//  3. The stage executes the "flow" composition's single tool step ("echo")
//     via the local tool registry.
//  4. The echo tool returns the user's input, which becomes the response.
//
// The assertion checks that the echoed value appears in the response, proving
// the CompositionStage ran (not just that Send didn't error).
func TestComposition_EmbeddedState_RunsViaSend(t *testing.T) {
	packPath := createTestPackFile(t, compositionExecPackJSON)

	// A mock provider is wired so provider detection succeeds, but it is never
	// called — the composition state's tool step runs entirely through the local
	// tool registry without an LLM call.
	mockProv := mock.NewProvider("mock", "mock-model", false)
	wc, err := OpenWorkflow(packPath, WithSkipSchemaValidation(), WithProvider(mockProv))
	require.NoError(t, err, "OpenWorkflow should succeed for a composition-entry workflow")
	defer wc.Close()

	assert.Equal(t, "compose", wc.CurrentState())

	// Register the echo tool executor on the active conversation's tool registry.
	// The pipeline is already built at Open time, so OnTool() alone (which snapshots
	// handlers) is insufficient — we must register a live executor directly.
	echoExec := &echoToolExecutor{}
	wc.ActiveConversation().ToolRegistry().RegisterExecutor(echoExec)

	ctx := context.Background()
	resp, err := wc.Send(ctx, "hello from composition")
	require.NoError(t, err, "Send should succeed — no LLM needed, only a tool step")

	// The CompositionStage emits the composition output as the response.
	// The echo tool returns {"echoed":"hello from composition"}, which the
	// stage serialises as the assistant message Content.
	raw := resp.Text()
	require.NotEmpty(t, raw, "response must carry composition output")

	var result map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &result),
		"composition output should be valid JSON (the tool's return value)")
	assert.Equal(t, "hello from composition", result["echoed"],
		"composition must forward the user input through the echo tool")
}

// echoToolExecutor implements tools.Executor for the "echo" tool.
// It is registered on the conversation's ToolRegistry after Open so the
// CompositionStage can find it during tool-step execution. Executor registration
// replaces the snapshot-based localExecutor built at pipeline-construction time,
// which is the established pattern for integration tests (see tools_test.go).
type echoToolExecutor struct{}

func (e *echoToolExecutor) Name() string { return "local" }

func (e *echoToolExecutor) Execute(
	_ context.Context,
	descriptor *tools.ToolDescriptor,
	args json.RawMessage,
) (json.RawMessage, error) {
	if descriptor.Name != "echo" {
		return nil, nil
	}
	var in map[string]any
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	v, _ := in["value"].(string)
	out := map[string]string{"echoed": v}
	return json.Marshal(out)
}
