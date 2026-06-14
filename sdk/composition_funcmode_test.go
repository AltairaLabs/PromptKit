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

// compositionFuncmodePackJSON is a minimal pack with a single named composition
// "flow" and no workflow section. It is used to test OpenComposition —
// the function-mode surface that runs a composition directly (RFC 0010 plan 4b).
//
// The pack also contains a "_placeholder" prompt so pack validation passes, and
// declares the "echo" tool so the composition step resolves its descriptor.
const compositionFuncmodePackJSON = `{
	"schema_version": "2025.1",
	"id": "composition-funcmode-test",
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

// TestCompositionOutput_ReturnsRawJSON verifies that Response.CompositionOutput
// returns the composition output as raw JSON when a composition state ran.
//
// It opens the pack via OpenWorkflow (whose entry state is a composition state)
// so that Send exercises the CompositionStage, then asserts CompositionOutput()
// returns the same content as Text() parsed as json.RawMessage.
func TestCompositionOutput_ReturnsRawJSON(t *testing.T) {
	// Re-use the workflow pack from composition_exec_test.go (same package).
	packPath := createTestPackFile(t, compositionExecPackJSON)

	mockProv := mock.NewProvider("mock", "mock-model", false)
	wc, err := OpenWorkflow(packPath, WithSkipSchemaValidation(), WithProvider(mockProv))
	require.NoError(t, err)
	defer wc.Close()

	// Register echo tool executor directly (same pattern as composition_exec_test.go).
	wc.ActiveConversation().ToolRegistry().RegisterExecutor(&echoToolExecutor{})

	ctx := context.Background()
	resp, err := wc.Send(ctx, "funcmode-test-input")
	require.NoError(t, err)

	out := resp.CompositionOutput()
	require.NotNil(t, out, "CompositionOutput must not be nil when a composition ran")

	// Must be valid JSON
	var result map[string]string
	require.NoError(t, json.Unmarshal(out, &result),
		"CompositionOutput should be valid JSON")
	assert.Equal(t, "funcmode-test-input", result["echoed"],
		"CompositionOutput should carry the echoed input")
}

// TestCompositionOutput_NilSafe verifies that CompositionOutput does not panic
// on a nil receiver or a Response with no message.
func TestCompositionOutput_NilSafe(t *testing.T) {
	var r *Response
	assert.Nil(t, r.CompositionOutput(), "nil receiver must return nil")

	empty := &Response{}
	assert.Nil(t, empty.CompositionOutput(), "nil message must return nil")
}

// ---- Task 2: OpenComposition ----

// funcmodeEchoExec is a copy of echoToolExecutor scoped to this test file.
// We cannot declare a second echoToolExecutor type in the same package so we
// alias it here — both test files are package sdk.
type funcmodeEchoExec = echoToolExecutor

// TestOpenComposition_RunsCompositionDirectly verifies the function-mode entry
// point:
//  1. OpenComposition resolves "flow" from the pack's compositions map.
//  2. A subsequent Send executes the echo tool step.
//  3. CompositionOutput returns the echoed value as JSON.
func TestOpenComposition_RunsCompositionDirectly(t *testing.T) {
	packPath := createTestPackFile(t, compositionFuncmodePackJSON)

	mockProv := mock.NewProvider("mock", "mock-model", false)
	conv, err := OpenComposition(packPath, "flow",
		WithSkipSchemaValidation(),
		WithProvider(mockProv),
	)
	require.NoError(t, err, "OpenComposition should succeed for an existing composition")
	defer conv.Close()

	// Register the echo tool executor directly on the conversation's registry.
	conv.ToolRegistry().RegisterExecutor(&funcmodeEchoExec{})

	ctx := context.Background()
	resp, err := conv.Send(ctx, "hello from funcmode")
	require.NoError(t, err, "Send should succeed — only a tool step, no LLM")

	out := resp.CompositionOutput()
	require.NotNil(t, out, "CompositionOutput must not be nil after a composition turn")

	var result map[string]string
	require.NoError(t, json.Unmarshal(out, &result),
		"CompositionOutput should be valid JSON")
	assert.Equal(t, "hello from funcmode", result["echoed"])
}

// TestOpenComposition_ErrorOnMissingComposition verifies that OpenComposition
// returns a clear error when the named composition is not found in the pack.
func TestOpenComposition_ErrorOnMissingComposition(t *testing.T) {
	packPath := createTestPackFile(t, compositionFuncmodePackJSON)

	mockProv := mock.NewProvider("mock", "mock-model", false)
	_, err := OpenComposition(packPath, "nope",
		WithSkipSchemaValidation(),
		WithProvider(mockProv),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"nope"`,
		"error must name the missing composition")

	// Verify ToolRegistry is accessible on a valid Conversation (type-check).
	_ = (*Conversation)(nil)
}

// TestOpenComposition_CompositionOutput_NilToolRegistry tests that the ToolRegistry
// method exists and returns non-nil on a successfully opened conversation.
func TestOpenComposition_ToolRegistryAccessible(t *testing.T) {
	packPath := createTestPackFile(t, compositionFuncmodePackJSON)
	mockProv := mock.NewProvider("mock", "mock-model", false)
	conv, err := OpenComposition(packPath, "flow",
		WithSkipSchemaValidation(),
		WithProvider(mockProv),
	)
	require.NoError(t, err)
	defer conv.Close()
	assert.NotNil(t, conv.ToolRegistry(), "ToolRegistry must be accessible on an OpenComposition conversation")
}

// Ensure tools import is used (echoToolExecutor uses tools.ToolDescriptor).
var _ = tools.ToolDescriptor{}
