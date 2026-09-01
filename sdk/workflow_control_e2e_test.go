package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// controlE2EProvider speaks AND transitions on the first round, so the turn has
// something to show the user whether or not the destination state runs. The
// handoff tests transition with no content, which cannot tell "the destination
// stayed silent" apart from "the whole turn produced nothing".
type controlE2EProvider struct {
	*mock.ToolProvider
	seenPrompts []string
}

// Pin to the unary tool loop for determinism.
func (p *controlE2EProvider) SupportsStreaming() bool { return false }

func (p *controlE2EProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)
	return providers.PredictionResponse{Content: "no tools available"}, nil
}

func (p *controlE2EProvider) PredictWithTools(
	_ context.Context,
	req providers.PredictionRequest,
	_ providers.ProviderTools,
	_ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)

	if len(p.seenPrompts) == 1 {
		calls := []types.MessageToolCall{{
			ID:   "call-1",
			Name: "workflow__transition",
			Args: []byte(`{"event":"InfoComplete","context":"name and account collected"}`),
		}}
		return providers.PredictionResponse{Content: "intake speaking", ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "destination speaking", ToolCalls: nil}, nil, nil
}

// controlPack returns the shared workflow pack with the given control clause
// spliced onto the destination state. An empty clause leaves it undeclared.
func controlPack(t *testing.T, clause string) string {
	t.Helper()

	const anchor = `"processing": {
				"prompt_task": "process",`
	require.Contains(t, workflowPackJSON, anchor,
		"the shared pack changed shape; this splice needs updating")

	replacement := anchor
	if clause != "" {
		replacement = anchor + "\n\t\t\t\t" + clause
	}
	return strings.Replace(workflowPackJSON, anchor, replacement, 1)
}

// The end-to-end proof for RFC 0014. Everything else about `control` is tested
// at the resolver, which reports Stop; this runs a real pack through Send and
// asserts what the turn actually did, because a resolver returning Stop is only
// useful if the tool loop then ends the turn.
func TestWorkflowConversation_ControlDecidesWhoSpeaks(t *testing.T) {
	tests := []struct {
		name           string
		clause         string
		wantRounds     int
		wantReply      string
		wantNotInReply string
		why            string
	}{
		{
			name:           "control user ends the turn after the origin speaks",
			clause:         `"control": "user",`,
			wantRounds:     1,
			wantReply:      "intake speaking",
			wantNotInReply: "destination speaking",
			why:            "RFC 0014: the destination yields, so it must not produce a round",
		},
		{
			name:       "control agent runs the destination state in the same turn",
			clause:     `"control": "agent",`,
			wantRounds: 2,
			wantReply:  "destination speaking",
			why:        "RFC 0014: the destination keeps the turn and speaks",
		},
		{
			// The documented divergence, proven end to end rather than at the
			// resolver: packs written before v1.7.0 declare nothing and must
			// keep behaving as they did.
			name:       "absent control still runs the destination state",
			clause:     "",
			wantRounds: 2,
			wantReply:  "destination speaking",
			why:        "an undeclared control must not start yielding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packPath := writeWorkflowTestPack(t, controlPack(t, tt.clause))
			provider := &controlE2EProvider{
				ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
			}

			wc, err := OpenWorkflow(packPath, WithProvider(provider), WithSkipSchemaValidation())
			require.NoError(t, err)
			defer func() { require.NoError(t, wc.Close()) }()

			resp, err := wc.Send(context.Background(), "I need help with my account")
			require.NoError(t, err)

			// The transition commits either way: `control` decides who speaks
			// next, not whether the workflow moves.
			require.Equal(t, "processing", wc.CurrentState(),
				"the transition must commit regardless of control")

			require.Len(t, provider.seenPrompts, tt.wantRounds, tt.why)
			require.Contains(t, resp.Text(), tt.wantReply, tt.why)
			if tt.wantNotInReply != "" {
				require.NotContains(t, resp.Text(), tt.wantNotInReply, tt.why)
			}
			if tt.wantRounds == 2 {
				require.Equal(t, "You process requests.", provider.seenPrompts[1],
					"the second round must run as the destination state")
			}
		})
	}
}
