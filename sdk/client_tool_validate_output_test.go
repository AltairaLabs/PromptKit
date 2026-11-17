package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const locateOutputSchema = `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`

// suspendOnLocate opens a conversation whose first round calls the client tool
// "locate" (deferred: no handler), configured as validateOutput says, and
// returns it suspended on that call together with the provider.
func suspendOnLocate(t *testing.T, validateOutput bool) (*Conversation, *roundsProvider) {
	t.Helper()
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds: []providers.PredictionResponse{{
			Content:   "Where?",
			ToolCalls: []types.MessageToolCall{{ID: "c-loc", Name: "locate", Args: []byte(`{}`)}},
		}},
	}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	desc, err := conv.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.Mode = "client"
	desc.OutputSchema = json.RawMessage(locateOutputSchema)
	desc.ClientConfig = &tools.ClientConfig{ValidateOutput: validateOutput}

	resp, err := conv.Send(context.Background(), "go")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())
	return conv, provider
}

// lastToolResult is the result for callID in the last request the model saw.
func lastToolResult(t *testing.T, p *roundsProvider, callID string) *types.MessageToolResult {
	t.Helper()
	last := p.seen[len(p.seen)-1]
	for i := range last {
		if last[i].ToolResult != nil && last[i].ToolResult.ID == callID {
			return last[i].ToolResult
		}
	}
	t.Fatalf("no result for %s reached the model", callID)
	return nil
}

// With validate_output set, a deferred result that does not match the tool's
// output schema reaches the model as a failed call, the way a handler's
// invalid result does.
func TestClientToolValidateOutput_RejectsInvalidDeferredResult(t *testing.T) {
	conv, provider := suspendOnLocate(t, true)
	ctx := context.Background()

	require.NoError(t, conv.SendToolResult(ctx, "c-loc", map[string]any{"lat": 48.8}))
	_, err := conv.Resume(ctx)
	require.NoError(t, err)

	res := lastToolResult(t, provider, "c-loc")
	assert.Contains(t, res.Error, "result validation failed")
	assert.Contains(t, res.GetTextContent(), "Tool error: ")
	assert.Contains(t, res.GetTextContent(), "city")
	assert.NotContains(t, res.GetTextContent(), "48.8", "the rejected result is not passed on")
}

func TestClientToolValidateOutput_PassesValidDeferredResult(t *testing.T) {
	conv, provider := suspendOnLocate(t, true)
	ctx := context.Background()

	require.NoError(t, conv.SendToolResult(ctx, "c-loc", map[string]any{"city": "Paris"}))
	_, err := conv.Resume(ctx)
	require.NoError(t, err)

	res := lastToolResult(t, provider, "c-loc")
	assert.Empty(t, res.Error)
	assert.JSONEq(t, `{"city":"Paris"}`, res.GetTextContent())
}

// Without validate_output, a deferred result is passed on as supplied.
func TestClientToolValidateOutput_OffPassesResultThrough(t *testing.T) {
	conv, provider := suspendOnLocate(t, false)
	ctx := context.Background()

	require.NoError(t, conv.SendToolResult(ctx, "c-loc", map[string]any{"lat": 48.8}))
	_, err := conv.Resume(ctx)
	require.NoError(t, err)

	res := lastToolResult(t, provider, "c-loc")
	assert.Empty(t, res.Error)
	assert.JSONEq(t, `{"lat":48.8}`, res.GetTextContent())
}
