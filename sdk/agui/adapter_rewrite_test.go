package agui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// redactHook rewrites every tool result to "<redacted:...>".
type redactHook struct{}

func (redactHook) Name() string { return "redact" }

func (redactHook) BeforeExecution(context.Context, hooks.ToolRequest) hooks.Decision {
	return hooks.Allow
}

func (redactHook) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

func (redactHook) RewriteResult(_ context.Context, _ hooks.ToolRequest, content string) (string, error) {
	return "<redacted:" + content + ">", nil
}

// When every call is answered, the TOOL_CALL_RESULT is the rewritten text
// the model was given.
func TestE2E_FullAnswerEmitsTheRewrittenResult(t *testing.T) {
	provider := newScriptedProvider(say("Checking.", call("c1", "get_location", `{}`)), say("Done."))
	conv, err := sdk.Open(writePack(t, toolPackJSON), "chat", sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(), sdk.WithToolHook(redactHook{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	bindClientTool(t, conv, "get_location")
	answer := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "c1", Result: "Paris"}}, nil
	}

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithToolResultProvider(answer)), "where am I?")
	require.NoError(t, err)
	assert.Equal(t, `<redacted:"Paris">`, resultContent(t, evts, "c1"))
}
