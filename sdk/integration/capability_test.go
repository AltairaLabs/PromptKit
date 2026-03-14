package integration

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// Test capability that registers a custom tool
// ---------------------------------------------------------------------------

// testCapability is a minimal Capability that registers a single tool and
// tracks whether its handler was invoked.
type testCapability struct {
	toolCalled atomic.Bool
}

func (c *testCapability) Name() string { return "test-cap" }

func (c *testCapability) Init(_ sdk.CapabilityContext) error { return nil }

func (c *testCapability) RegisterTools(registry *tools.Registry) {
	_ = registry.Register(&tools.ToolDescriptor{
		Name:        "test-cap__ping",
		Description: "Returns pong",
		Mode:        "local",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"properties": { "result": { "type": "string" } },
			"required": ["result"]
		}`),
	})

	registry.RegisterExecutor(&pingExecutor{cap: c})
}

func (c *testCapability) Close() error { return nil }

// pingExecutor is a local executor for the test-cap__ping tool.
type pingExecutor struct {
	cap *testCapability
}

func (e *pingExecutor) Name() string { return "local" }

func (e *pingExecutor) Execute(_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage) (json.RawMessage, error) {
	e.cap.toolCalled.Store(true)
	return json.RawMessage(`{"result":"pong"}`), nil
}

// ---------------------------------------------------------------------------
// 8.1 — Explicit capability registers and exposes tools
// ---------------------------------------------------------------------------

func TestCapability_ExplicitCapabilityRegistersTools(t *testing.T) {
	cap := &testCapability{}
	conv := openTestConv(t,
		sdk.WithCapability(cap),
	)

	// Verify the tool was registered in the conversation's tool registry.
	reg := conv.ToolRegistry()
	require.NotNil(t, reg)

	tool := reg.Get("test-cap__ping")
	require.NotNil(t, tool, "capability tool test-cap__ping should be registered")
	assert.Equal(t, "Returns pong", tool.Description)
}

// ---------------------------------------------------------------------------
// 8.2 — Workflow auto-infers WorkflowCapability
// ---------------------------------------------------------------------------

func TestCapability_WorkflowAutoInference(t *testing.T) {
	wc := openTestWorkflow(t)

	// A workflow pack should auto-infer WorkflowCapability, which registers
	// the workflow__transition tool. AvailableEvents being non-empty proves
	// the capability was initialized and the state machine is active.
	avail := wc.AvailableEvents()
	assert.NotEmpty(t, avail, "workflow should have available events in entry state")
	assert.Contains(t, avail, "Escalate")
	assert.Contains(t, avail, "Resolve")
}
