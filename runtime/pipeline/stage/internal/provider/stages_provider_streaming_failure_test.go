package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestProviderStage_Streaming_TurnFailureSignalSkipsPendingTurn: in streaming
// mode, an upstream turn failure arriving after the turn's user message must
// not fire that turn when the input then closes. The failure is passed on, and
// the half-received turn is not emitted as if it had been answered.
func TestProviderStage_Streaming_TurnFailureSignalSkipsPendingTurn(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	ps := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, core.NewTurnState())

	in := make(chan core.StreamElement, 2)
	in <- userElement("half a turn")
	in <- core.NewTurnFailureSignal(core.NewStageError("stt", core.StageTypeTransform, errStageBoom))
	close(in)
	out := make(chan core.StreamElement, 8)

	require.NoError(t, ps.Process(context.Background(), in, out))

	assert.Empty(t, prov.requests, "no model call for a turn that failed upstream")
	var errs []error
	var msgs []types.Message
	for e := range out {
		if e.Error != nil {
			errs = append(errs, e.Error)
		}
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	require.Len(t, errs, 1, "the failure must be passed on once")
	assert.ErrorIs(t, errs[0], errStageBoom)
	assert.Empty(t, msgs, "the failed turn's input must not be emitted as an answered turn")
}

// TestProviderStage_Streaming_TurnFailureKeepsEarlierTurns: turns completed
// before the failure keep their replies; only the turn in progress is dropped.
func TestProviderStage_Streaming_TurnFailureKeepsEarlierTurns(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	ps := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, core.NewTurnState())

	in := make(chan core.StreamElement, 4)
	in <- userElement("first")
	in <- core.NewEndOfTurnElement()
	in <- userElement("second, cut off")
	in <- core.NewTurnFailureSignal(core.NewStageError("stt", core.StageTypeTransform, errStageBoom))
	close(in)
	out := make(chan core.StreamElement, 16)

	require.NoError(t, ps.Process(context.Background(), in, out))

	require.Len(t, prov.requests, 1, "only the completed turn reaches the model")
	var replies []string
	for e := range out {
		if e.Message != nil && e.Message.Role == core.RoleAssistant {
			replies = append(replies, e.Message.Content)
		}
	}
	assert.Equal(t, []string{"reply-1"}, replies)
}
