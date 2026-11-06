package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// locateRound is a model round that calls the client tool "locate".
func locateRound() []providers.PredictionResponse {
	return []providers.PredictionResponse{{
		Content:   "Where?",
		ToolCalls: []types.MessageToolCall{{ID: "c-loc", Name: "locate", Args: []byte(`{}`)}},
	}}
}

// rejectLocate opens a conversation suspended on a deferred call to "locate"
// whose client config is cc, rejects the call with reason, resumes, and
// returns the text of the tool result the model saw.
func rejectLocate(t *testing.T, cc *tools.ClientConfig, reason string) string {
	t.Helper()
	provider := &roundsProvider{ToolProvider: mock.NewToolProvider("x", "m", false, nil), rounds: locateRound()}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	desc, err := conv.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.ClientConfig = cc
	conv.DeferClientTool("locate")

	ctx := context.Background()
	resp, err := conv.Send(ctx, "go")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())

	conv.RejectClientTool(ctx, "c-loc", reason)
	_, err = conv.Resume(ctx)
	require.NoError(t, err)
	return lastToolResult(t, provider, "c-loc").GetTextContent()
}

func declineConfig(strategy string) *tools.ClientConfig {
	return &tools.ClientConfig{Consent: &tools.ConsentConfig{Message: "Share location?", DeclineStrategy: strategy}}
}

func TestRejectClientTool_DeclineStrategyError(t *testing.T) {
	got := rejectLocate(t, declineConfig(tools.DeclineStrategyError), "user said no")
	assert.Equal(t, "Tool locate denied: user declined consent. Reason: user said no", got)
}

func TestRejectClientTool_DeclineStrategyFallback(t *testing.T) {
	got := rejectLocate(t, declineConfig(tools.DeclineStrategyFallback), "user said no")
	assert.Equal(t,
		"Tool locate unavailable: user declined consent. Use alternative approach. Reason: user said no", got)
}

func TestRejectClientTool_DeclineStrategyRetry(t *testing.T) {
	got := rejectLocate(t, declineConfig(tools.DeclineStrategyRetry), "user said no")
	assert.Equal(t,
		"Tool locate denied: user declined consent. Try a different approach. Reason: user said no", got)
}

// With a strategy and no reason, the model sees exactly the strategy text,
// the same text PromptArena's consent simulation gives it.
func TestRejectClientTool_DeclineStrategyWithoutReason(t *testing.T) {
	got := rejectLocate(t, declineConfig(tools.DeclineStrategyRetry), "")
	assert.Equal(t, tools.ConsentDeclinedMessage("locate", tools.DeclineStrategyRetry), got)
}

// Without a decline strategy the rejection text is what it always was, with or
// without consent configured.
func TestRejectClientTool_NoDeclineStrategyUnchanged(t *testing.T) {
	assert.Equal(t, "Tool rejected: user said no", rejectLocate(t, declineConfig(""), "user said no"))
	assert.Equal(t, "Tool rejected: user said no", rejectLocate(t, nil, "user said no"))
}

const locateToolYAML = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: locate
spec:
  description: Client tool
  mode: client
  input_schema:
    type: object
  output_schema:
    type: object
  client:
    consent:
      required: true
      message: Share location?
      decline_strategy: fallback
`

// A tool file a host loads into its registry (the form PromptArena loads)
// carries the decline strategy to RejectClientTool.
func TestRejectClientTool_DeclineStrategyFromHostToolFile(t *testing.T) {
	reg := tools.NewRegistry()
	require.NoError(t, reg.LoadToolFromBytes("locate.tool.yaml", []byte(locateToolYAML)))
	require.True(t, reg.Get("locate").ClientConfig.Consent.Required)

	provider := &roundsProvider{ToolProvider: mock.NewToolProvider("x", "m", false, nil), rounds: locateRound()}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat",
		WithProvider(provider), WithSkipSchemaValidation(), WithToolRegistry(reg))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.DeferClientTool("locate")

	ctx := context.Background()
	resp, err := conv.Send(ctx, "go")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())
	conv.RejectClientTool(ctx, "c-loc", "")
	_, err = conv.Resume(ctx)
	require.NoError(t, err)

	assert.Equal(t, "Tool locate unavailable: user declined consent. Use alternative approach.",
		lastToolResult(t, provider, "c-loc").GetTextContent())
}
