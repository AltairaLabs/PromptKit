package sdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// TestRegisterExecExecutor_UnregisteredToolWarns covers #1949: a RuntimeConfig
// exec binding whose key matches no registered tool was dropped without a
// word, leaving the tool in its pack-declared mode. The same condition on the
// descriptor-override path already warns; this pins the exec path to match.
func TestRegisterExecExecutor_UnregisteredToolWarns(t *testing.T) {
	logs := captureLogs(t)

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name:        "fetch_invoices",
		Description: "Fetch invoices",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode:        "local",
	}))

	conv := &Conversation{
		toolRegistry: registry,
		config: &config{
			execToolConfigs: map[string]*tools.ExecConfig{
				"fetch_invoice": {Command: "/usr/bin/fetch-invoice"}, // misspelled key
			},
		},
	}
	conv.registerExecExecutor()

	out := logs.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "exec tool config skipped")
	assert.Contains(t, out, "fetch_invoice", "the warning must name the unmatched key")
	assert.Contains(t, out, "fetch_invoices", "the warning must list what is registered")

	td := registry.Get("fetch_invoices")
	require.NotNil(t, td)
	assert.Equal(t, "local", td.Mode, "a near-miss key must not rebind a different tool")
}

// TestRegisterExecExecutor_MatchedToolDoesNotWarn pins the quiet path: a
// binding that matches stays silent at Warn.
func TestRegisterExecExecutor_MatchedToolDoesNotWarn(t *testing.T) {
	logs := captureLogs(t)

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name:        "fetch_invoices",
		Description: "Fetch invoices",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode:        "local",
	}))

	conv := &Conversation{
		toolRegistry: registry,
		config: &config{
			execToolConfigs: map[string]*tools.ExecConfig{
				"fetch_invoices": {Command: "/usr/bin/fetch-invoices"},
			},
		},
	}
	conv.registerExecExecutor()

	assert.NotContains(t, logs.String(), "exec tool config skipped")
	assert.Equal(t, "exec", registry.Get("fetch_invoices").Mode)
}
