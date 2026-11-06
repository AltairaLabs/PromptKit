package stage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// emitAndReturnStage drains its input, emits msg (when set) and err as an
// error element, then returns err: the shape of a stage that reports its own
// failure twice.
func emitAndReturnStage(name, msg string, err error) *StageFunc {
	return NewStageFunc(name, StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for range in { //nolint:revive // drain
			}
			if msg != "" {
				out <- userElement(msg)
			}
			out <- NewErrorElement(err)
			return err
		})
}

// TestExecuteSync_EmittedAndReturnedErrorSkipsUnaryProvider: an upstream that
// emits its error and also returns it fails the turn; the provider must not be
// called, and the caller gets the error once.
func TestExecuteSync_EmittedAndReturnedErrorSkipsUnaryProvider(t *testing.T) {
	provider := &stageErrCountingProvider{Provider: mock.NewProvider("mock", "m", false)}
	p, err := NewPipelineBuilder().Chain(
		emitAndReturnStage("stt", "", errStageBoom),
		NewProviderStage(provider, nil, nil, nil),
	).Build()
	require.NoError(t, err)

	in := make(chan StreamElement, 1)
	in <- userElement("hi")
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)

	errs := errorElements(out)
	assert.Zero(t, provider.callCount(), "no model call after the upstream failed")
	require.Len(t, errs, 1, "the error must reach the caller once")
	assert.ErrorIs(t, errs[0], errStageBoom)
}

// TestExecuteSync_EmittedAndReturnedErrorSkipsStreamingTurn: the same on the
// streaming path, where the upstream emitted the turn's user message first.
func TestExecuteSync_EmittedAndReturnedErrorSkipsStreamingTurn(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	p, err := NewPipelineBuilder().Chain(
		emitAndReturnStage("stt", "transcribed", errStageBoom),
		NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState()),
	).Build()
	require.NoError(t, err)

	in := make(chan StreamElement, 1)
	in <- userElement("audio")
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)

	errs := errorElements(out)
	assert.Empty(t, prov.requests, "no model call for the failed turn")
	require.Len(t, errs, 1, "the error must reach the caller once")
	assert.ErrorIs(t, errs[0], errStageBoom)
}

// TestExecute_BranchEmittedAndReturnedErrorSignalsEveryReader: on a
// competitive fan-out, an upstream that emits and returns its error still
// signals every reader, and the caller gets the error once.
func TestExecute_BranchEmittedAndReturnedErrorSignalsEveryReader(t *testing.T) {
	signaled := make(chan string, 2)
	reader := func(name string) *StageFunc {
		return NewStageFunc(name, StageTypeTransform,
			func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				for e := range in {
					if isTurnFailureSignal(&e) {
						signaled <- name
					}
					out <- e
				}
				return nil
			})
	}
	p, err := NewPipelineBuilder().
		AddStage(emitAndReturnStage("a", "", errStageBoom)).AddStage(reader("p")).AddStage(reader("q")).
		Branch("a", "p", "q").Build()
	require.NoError(t, err)

	in := make(chan StreamElement, 1)
	in <- userElement("hi")
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)

	errs := errorElements(out)
	require.Len(t, errs, 1, "the caller gets the error once")
	assert.ErrorIs(t, errs[0], errStageBoom)
	require.Len(t, signaled, 2, "both readers get the signal")
}

// TestCompletionTotals_FailureReachesCallerOnce: whichever of a failed stage's
// error element and its turn-failure signal reaches the collector first, the
// other is not delivered again. Other errors still are.
func TestCompletionTotals_FailureReachesCallerOnce(t *testing.T) {
	stageErr := NewStageError("a", StageTypeTransform, errStageBoom)
	other := NewErrorElement(fmt.Errorf("frame 3 could not be decoded"))

	signalFirst := &completionTotals{}
	sig, plain := newTurnFailureSignal(stageErr), NewErrorElement(errStageBoom)
	require.True(t, signalFirst.observe(&sig))
	assert.False(t, signalFirst.observe(&plain), "the error element after its signal is a repeat")
	assert.True(t, signalFirst.observe(&other))

	plainFirst := &completionTotals{}
	sig, plain = newTurnFailureSignal(stageErr), NewErrorElement(errStageBoom)
	require.True(t, plainFirst.observe(&plain))
	assert.False(t, plainFirst.observe(&sig), "the signal after its error element is a repeat")
	assert.True(t, plainFirst.observe(&other))
}

// TestExecute_UnrelatedSentinelDoesNotSwallowTheFailure: a non-fatal report
// of a bare sentinel by one stage is not another stage's failure that merely
// wraps the same sentinel. Both reach the caller, in either order.
func TestExecute_UnrelatedSentinelDoesNotSwallowTheFailure(t *testing.T) {
	failing := NewStageFunc("b", StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for range in { //nolint:revive // drain
			}
			return fmt.Errorf("b failed: %w", io.EOF)
		})
	reporter := func(before bool) *StageFunc {
		return NewStageFunc("c", StageTypeTransform,
			func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				if before {
					out <- NewErrorElement(io.EOF)
				}
				for e := range in {
					out <- e
				}
				if !before {
					out <- NewErrorElement(io.EOF)
				}
				return nil
			})
	}
	for _, before := range []bool{true, false} {
		p, err := NewPipelineBuilder().Chain(failing, reporter(before)).Build()
		require.NoError(t, err)

		in := make(chan StreamElement, 1)
		in <- userElement("hi")
		close(in)
		out, err := p.Execute(context.Background(), in)
		require.NoError(t, err)

		errs := errorElements(out)
		require.Len(t, errs, 2, "report before the failure: %v", before)
		var stageErr *StageError
		var sawFailure bool
		for _, e := range errs {
			if errors.As(e, &stageErr) && stageErr.StageName == "b" {
				sawFailure = true
			}
		}
		assert.True(t, sawFailure, "the caller must get stage b's failure (report before: %v)", before)
	}
}
