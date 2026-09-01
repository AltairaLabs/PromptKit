package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// scriptedProvider records the system prompt of every round and replies from a
// fixed script, so the test asserts on which workflow state generated each
// round rather than on model behaviour.
type scriptedProvider struct {
	*mock.ToolProvider
	seenPrompts []string
}

func (p *scriptedProvider) SupportsStreaming() bool { return false }

func (p *scriptedProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)
	return providers.PredictionResponse{Content: "no tools"}, nil
}

func (p *scriptedProvider) PredictWithTools(
	_ context.Context,
	req providers.PredictionRequest,
	_ providers.ProviderTools,
	_ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)

	switch len(p.seenPrompts) {
	case 1:
		// Ask for a refund large enough to need a human.
		calls := []types.MessageToolCall{{
			ID:   "call-refund",
			Name: "process_refund",
			Args: []byte(`{"order_id":"12345","amount":150,"reason":"damaged product"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	case 2:
		// Resumed after approval: hand off to the confirmation state.
		calls := []types.MessageToolCall{{
			ID:   "call-transition",
			Name: "workflow__transition",
			Args: []byte(`{"event":"Approved","context":"Refunded $150 on order 12345 for a damaged product."}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	default:
		return providers.PredictionResponse{
			Content: "Your $150 refund for order 12345 is on its way — allow 3-5 business days.",
		}, nil, nil
	}
}

// The end-to-end interaction the example demonstrates, and the regression test
// for the two bugs it exposed:
//
//   - A turn suspended for human approval resumes by re-executing the SAME
//     pipeline, which re-runs PromptAssemblyStage and resets the system prompt.
//     The resumed turn must still be running the workflow's current state.
//   - When the resumed turn transitions, the destination state must generate
//     the reply in that same turn rather than leaving the conversation silent.
func TestHITLWorkflow_ConfirmationStateSpeaksAfterApproval(t *testing.T) {
	provider := &scriptedProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
	}

	wc, err := sdk.OpenWorkflow("./hitl-workflow.pack.json",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer func() { _ = wc.Close() }()

	conv := wc.ActiveConversation()
	conv.OnToolAsync(
		"process_refund",
		func(args map[string]any) sdktools.PendingResult {
			if amount, _ := args["amount"].(float64); amount > 100 {
				return sdktools.PendingResult{
					Reason:  "high_value_refund",
					Message: "Refund over $100 requires human approval",
				}
			}
			return sdktools.PendingResult{}
		},
		func(args map[string]any) (any, error) {
			return map[string]any{
				"status":    "completed",
				"refund_id": "RF-12345",
				"amount":    args["amount"],
			}, nil
		},
	)

	ctx := context.Background()
	require.Equal(t, "triage", wc.CurrentState())

	// Turn 1 suspends on the approval.
	resp, err := wc.Send(ctx, "I need a refund of $150 for order #12345, the product was damaged")
	require.NoError(t, err)

	pending := resp.PendingTools()
	require.Len(t, pending, 1, "the high-value refund must await approval")
	require.Equal(t, "process_refund", pending[0].Name)
	require.Equal(t, "triage", wc.CurrentState(), "state must not move while awaiting approval")

	// The human approves, and the conversation resumes.
	_, err = conv.ResolveTool(ctx, pending[0].ID)
	require.NoError(t, err)

	resumed, err := conv.Continue(ctx)
	require.NoError(t, err)

	// The workflow advanced and the CONFIRMATION state produced the reply,
	// inside the resumed turn, with no further user message.
	require.Equal(t, "confirmation", wc.CurrentState())
	require.Contains(t, resumed.Text(), "refund",
		"the confirmation state must have generated the reply")

	require.Len(t, provider.seenPrompts, 3)
	require.Contains(t, provider.seenPrompts[0], "refund triage agent",
		"round 1 runs as triage")
	require.Contains(t, provider.seenPrompts[1], "refund triage agent",
		"the resumed round must still be triage, not whatever the pipeline was rebuilt with")
	require.Contains(t, provider.seenPrompts[2], "refund confirmation agent",
		"the round after the transition must run as the confirmation state")

	// The brief the triage agent wrote is what the confirmation state was
	// given -- {{workflow_context}} in its template.
	require.Contains(t, provider.seenPrompts[2], "Refunded $150 on order 12345",
		"the transition's context argument must reach the destination prompt")
}
