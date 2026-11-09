package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// predictAsStreamProvider is a tool-capable provider that does not stream. Like
// the real tool providers, PredictWithTools returns the tool calls as its
// second result, not on the response.
type predictAsStreamProvider struct {
	multiTurnRecordingProvider
	finish string
	calls  []types.MessageToolCall
}

func (p *predictAsStreamProvider) BuildTooling(d []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	return d, nil
}

func (p *predictAsStreamProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	return providers.PredictionResponse{Content: "reply", FinishReason: p.finish}, p.calls, nil
}

func (p *predictAsStreamProvider) PredictStreamWithTools(
	context.Context, providers.PredictionRequest, providers.ProviderTools, string,
) (<-chan providers.StreamChunk, error) {
	panic("does not stream")
}

// A streaming turn on a provider that does not stream (after a workflow
// handoff) is called whole and read back as one chunk. The round must report
// what the provider reported: its tool calls, and its finish reason. When the
// provider reports none, the stand-in uses the provider contract's values
// (types.FinishReasonToolUse / FinishReasonStop), not another vocabulary.
func TestStreamingRound_NonStreamingProviderReportsItsOwnResult(t *testing.T) {
	call := types.MessageToolCall{ID: "c1", Name: "lookup", Args: json.RawMessage(`{}`)}
	for _, tc := range []struct {
		name       string
		finish     string
		calls      []types.MessageToolCall
		wantFinish string
	}{
		{"tool calls, no reason", "", []types.MessageToolCall{call}, types.FinishReasonToolUse},
		{"text, no reason", "", nil, types.FinishReasonStop},
		{"reason reported", types.FinishReasonMaxOutputTokens, nil, types.FinishReasonMaxOutputTokens},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov := &predictAsStreamProvider{finish: tc.finish, calls: tc.calls}
			stage := NewProviderStage(prov, nil, nil, &ProviderConfig{Streaming: true})
			tools := []*providers.ToolDescriptor{{Name: "lookup"}}

			ch, err := stage.startStreamingRequest(context.Background(),
				providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}, tools, "auto")
			require.NoError(t, err)
			got, err := stage.processStreamChunks(context.Background(), ch, make(chan StreamElement, 8),
				roundRef{round: 1}, false)
			require.NoError(t, err)

			assert.Equal(t, "reply", got.content)
			assert.Equal(t, tc.wantFinish, got.finishReason)
			assert.Equal(t, tc.calls, got.toolCalls, "the provider's tool calls must survive the stand-in")
		})
	}
}
