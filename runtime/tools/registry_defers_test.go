package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// declaredDeferrer defers every call and says, before running one, whether it
// will (tools.DeferringExecutor).
type declaredDeferrer struct{ defers bool }

func (d *declaredDeferrer) Name() string { return "declared-deferrer" }

func (d *declaredDeferrer) Defers(*tools.ToolDescriptor, tools.ToolCall) bool { return d.defers }

func (d *declaredDeferrer) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (d *declaredDeferrer) ExecuteAsync(
	context.Context, *tools.ToolDescriptor, json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	return &tools.ToolExecutionResult{Status: tools.ToolStatusPending}, nil
}

// The admission gate is not asked about a call its executor says it will
// defer; it is asked about any other.
func TestRegistry_ExecuteAsyncAdmittedSkipsTheGateForADeferredCall(t *testing.T) {
	for _, defers := range []bool{true, false} {
		reg := tools.NewRegistry()
		exec := &declaredDeferrer{defers: defers}
		reg.RegisterExecutor(exec)
		require.NoError(t, reg.Register(&tools.ToolDescriptor{
			Name: "ask", Description: "ask", Mode: exec.Name(),
			InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{}`),
		}))
		asked := 0
		res, err := reg.ExecuteAsyncAdmitted(context.Background(), "ask", json.RawMessage(`{}`),
			func() error { asked++; return nil })
		require.NoError(t, err)
		assert.Equal(t, tools.ToolStatusPending, res.Status)
		if defers {
			assert.Zero(t, asked, "a deferred call is admitted when it is settled")
		} else {
			assert.Equal(t, 1, asked)
		}
	}
}
