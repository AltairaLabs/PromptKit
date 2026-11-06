package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestProviderStage_Streaming_TurnFailureSignalSkipsPendingTurn: in streaming
// mode, an upstream turn failure arriving after the turn's user message must
// not fire that turn when the input then closes. The failure is passed on, and
// the half-received turn is not emitted as if it had been answered.
func TestProviderStage_Streaming_TurnFailureSignalSkipsPendingTurn(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	ps := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	in := make(chan StreamElement, 2)
	in <- userElement("half a turn")
	in <- newTurnFailureSignal(NewStageError("stt", StageTypeTransform, errStageBoom))
	close(in)
	out := make(chan StreamElement, 8)

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
	ps := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	in := make(chan StreamElement, 4)
	in <- userElement("first")
	in <- NewEndOfTurnElement()
	in <- userElement("second, cut off")
	in <- newTurnFailureSignal(NewStageError("stt", StageTypeTransform, errStageBoom))
	close(in)
	out := make(chan StreamElement, 16)

	require.NoError(t, ps.Process(context.Background(), in, out))

	require.Len(t, prov.requests, 1, "only the completed turn reaches the model")
	var replies []string
	for e := range out {
		if e.Message != nil && e.Message.Role == roleAssistant {
			replies = append(replies, e.Message.Content)
		}
	}
	assert.Equal(t, []string{"reply-1"}, replies)
}

// TestExecute_StreamingProviderSkipsTurnAfterUpstreamFailure runs it through
// the pipeline: an upstream stage (an STT stage, say) that emits the user
// message and then fails must not get that message answered.
func TestExecute_StreamingProviderSkipsTurnAfterUpstreamFailure(t *testing.T) {
	for i := 0; i < 20; i++ {
		prov := &multiTurnRecordingProvider{}
		emitThenFail := NewStageFunc("stt", StageTypeTransform,
			func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				for range in { //nolint:revive // drain
				}
				out <- userElement("transcribed")
				return errStageBoom
			})
		p, err := NewPipelineBuilder().Chain(
			emitThenFail,
			NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState()),
		).Build()
		require.NoError(t, err)

		_, execErr := p.ExecuteSync(context.Background(), userElement("audio"))

		require.ErrorIs(t, execErr, errStageBoom, "run %d", i)
		require.Empty(t, prov.requests, "run %d: no model call after the upstream failed", i)
	}
}
