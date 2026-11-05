package stage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// errStageBoom is the failure the test stages below report.
var errStageBoom = errors.New("stage boom")

// failingStage closes its output and returns err — the shape of TemplateStage
// when a system-prompt variable is unresolved. When emit is set it also puts
// err on its output first, the shape of a stage that reports twice.
func failingStage(name string, err error, emit bool) *StageFunc {
	return NewStageFunc(name, StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for range in { //nolint:revive // drain the turn's input before failing
			}
			if emit {
				out <- NewErrorElement(err)
			}
			return err
		})
}

// endOfInputProbe forwards its input and records, at the moment its input
// ended — the point where ProviderStage decides to call the model — whether
// an error element had arrived and whether its context had been canceled.
type endOfInputProbe struct {
	mu        sync.Mutex
	sawEOF    bool
	inputErrs []error
	ctxErrEOF error
}

func (p *endOfInputProbe) stage() *StageFunc {
	return NewStageFunc("probe", StageTypeTransform,
		func(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			var errs []error
			for e := range in {
				if e.Error != nil {
					errs = append(errs, e.Error)
				}
				out <- e
			}
			p.mu.Lock()
			p.sawEOF = true
			p.inputErrs = errs
			p.ctxErrEOF = ctx.Err()
			p.mu.Unlock()
			return nil
		})
}

func (p *endOfInputProbe) observed() (sawEOF bool, inputErrs []error, ctxErr error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sawEOF, p.inputErrs, p.ctxErrEOF
}

func userElement(text string) StreamElement {
	return NewMessageElement(&types.Message{Role: "user", Content: text})
}

func errorElements(out <-chan StreamElement) []error {
	var errs []error
	for e := range out {
		if e.Error != nil {
			errs = append(errs, e.Error)
		}
	}
	return errs
}

// TestExecuteSync_StageErrorEndsTheTurn is #2312: a stage returning an error
// must fail the turn. ExecuteSync returns that error, and a downstream stage
// receives it in-band, as an error element before its input ends, so it can
// tell the turn failed rather than completed. Its context is not canceled: it
// still drains as before.
func TestExecuteSync_StageErrorEndsTheTurn(t *testing.T) {
	probe := &endOfInputProbe{}
	cause := fmt.Errorf("%w: chat: [{{user_name}}]", ErrTemplateUnresolved)
	p, err := NewPipelineBuilder().Chain(failingStage("template", cause, false), probe.stage()).Build()
	require.NoError(t, err)

	_, execErr := p.ExecuteSync(context.Background(), userElement("hi"))

	require.Error(t, execErr, "a failing stage must fail ExecuteSync")
	assert.ErrorIs(t, execErr, ErrTemplateUnresolved)
	var stageErr *StageError
	require.ErrorAs(t, execErr, &stageErr)
	assert.Equal(t, "template", stageErr.StageName)

	sawEOF, inputErrs, ctxErr := probe.observed()
	require.True(t, sawEOF)
	require.Len(t, inputErrs, 1, "the failure must reach the downstream stage before its input ends, once")
	assert.ErrorIs(t, inputErrs[0], ErrTemplateUnresolved)
	assert.NoError(t, ctxErr, "a stage failure must not cancel the other stages")
}

// TestExecuteSync_TemplateErrorSkipsProvider runs the real stages from the
// report: an unresolved system-prompt variable must not reach the model.
func TestExecuteSync_TemplateErrorSkipsProvider(t *testing.T) {
	turnState := NewTurnState()
	turnState.Template = &prompt.Template{TaskType: "chat", RawTemplate: "The user's name is {{user_name}}."}
	provider := &stageErrCountingProvider{Provider: mock.NewProvider("mock", "m", false)}

	p, err := NewPipelineBuilder().Chain(
		NewTemplateStageWithTurnState(nil, turnState),
		NewProviderStageWithTurnState(provider, nil, nil, nil, nil, nil, turnState),
	).Build()
	require.NoError(t, err)

	// Repeat: the cancellation must be ordered before the provider sees its
	// input end, not merely likely to win a race.
	for i := 0; i < 50; i++ {
		_, execErr := p.ExecuteSync(context.Background(), userElement("Hello!"))
		require.ErrorIs(t, execErr, ErrTemplateUnresolved, "run %d", i)
	}
	assert.Zero(t, provider.callCount(), "no provider call after the template stage failed")
}

// TestExecute_StageErrorDeliveredOnce covers a stage that already puts its
// error on the output and also returns it: the caller sees it once.
func TestExecute_StageErrorDeliveredOnce(t *testing.T) {
	p, err := NewPipelineBuilder().Chain(failingStage("emitter", errStageBoom, true)).Build()
	require.NoError(t, err)

	in := make(chan StreamElement, 1)
	in <- userElement("hi")
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)

	errs := errorElements(out)
	require.Len(t, errs, 1, "the error must reach the caller exactly once")
	assert.ErrorIs(t, errs[0], errStageBoom)
}

// TestExecute_StageErrorDeliveredWhenNotEmitted is the other half: a stage
// that only returns its error still gets it to the caller, as the last element.
func TestExecute_StageErrorDeliveredWhenNotEmitted(t *testing.T) {
	p, err := NewPipelineBuilder().Chain(failingStage("silent", errStageBoom, false)).Build()
	require.NoError(t, err)

	in := make(chan StreamElement, 1)
	in <- userElement("hi")
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)

	errs := errorElements(out)
	require.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], errStageBoom)
}

// TestExecute_CallerCancelAddsNoError: a stage that returns ctx.Err() because
// the caller canceled is not a stage failure, so the pipeline adds nothing.
func TestExecute_CallerCancelAddsNoError(t *testing.T) {
	started := make(chan struct{})
	waiter := NewStageFunc("waiter", StageTypeTransform,
		func(ctx context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	p, err := NewPipelineBuilder().Chain(waiter).Build()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan StreamElement)
	defer close(in)
	out, err := p.Execute(ctx, in)
	require.NoError(t, err)
	<-started
	cancel()

	assert.Empty(t, errorElements(out), "a caller cancel must not become a pipeline error")
}

// TestExecute_StageErrorLeavesDownstreamRunning: a failure does not cancel the
// stages after it. A downstream stage gets the error element, keeps a live
// context, and what it emits still reaches the caller, along with the error.
func TestExecute_StageErrorLeavesDownstreamRunning(t *testing.T) {
	afterFailure := NewStageFunc("after", StageTypeTransform,
		func(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for e := range in {
				out <- e
				if e.Error == nil {
					continue
				}
				if ctx.Err() != nil {
					return errors.New("after: context canceled by the upstream failure")
				}
				text := "after the failure"
				out <- StreamElement{Text: &text}
			}
			return nil
		})
	p, err := NewPipelineBuilder().Chain(failingStage("fails", errStageBoom, false), afterFailure).Build()
	require.NoError(t, err)

	in := make(chan StreamElement, 1)
	in <- userElement("hi")
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)

	var texts []string
	var errs []error
	for e := range out {
		if e.Text != nil {
			texts = append(texts, *e.Text)
		}
		if e.Error != nil {
			errs = append(errs, e.Error)
		}
	}
	assert.Equal(t, []string{"after the failure"}, texts, "the downstream stage must keep running")
	require.Len(t, errs, 1, "the in-band error is the caller's error; it must not be sent again")
	assert.ErrorIs(t, errs[0], errStageBoom)
}

// TestExecute_ErrorElementNotDoubledByGate: an upstream that emits its error
// and also returns it gets one error element downstream, not two.
func TestExecute_ErrorElementNotDoubledByGate(t *testing.T) {
	probe := &endOfInputProbe{}
	p, err := NewPipelineBuilder().Chain(failingStage("emitter", errStageBoom, true), probe.stage()).Build()
	require.NoError(t, err)

	_, execErr := p.ExecuteSync(context.Background(), userElement("hi"))
	require.ErrorIs(t, execErr, errStageBoom)

	_, inputErrs, _ := probe.observed()
	require.Len(t, inputErrs, 1)
}

// TestExecute_CallerCancelSignalsNothingDownstream: a stage returning ctx.Err()
// after the caller canceled is not a failure, so no error element is sent on.
func TestExecute_CallerCancelSignalsNothingDownstream(t *testing.T) {
	probe := &endOfInputProbe{}
	ctx, cancel := context.WithCancel(context.Background())
	waiter := NewStageFunc("waiter", StageTypeTransform,
		func(ctx context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			cancel()
			<-ctx.Done()
			return ctx.Err()
		})
	p, err := NewPipelineBuilder().Chain(waiter, probe.stage()).Build()
	require.NoError(t, err)

	in := make(chan StreamElement)
	defer close(in)
	out, err := p.Execute(ctx, in)
	require.NoError(t, err)

	assert.Empty(t, errorElements(out))
	sawEOF, inputErrs, _ := probe.observed()
	require.True(t, sawEOF)
	assert.Empty(t, inputErrs, "a caller cancel must not be signaled as a failure")
}

// TestContextBuilder_ForwardsUpstreamErrorWithoutTruncating: with a token
// budget, ContextBuilderStage collects messages; an upstream failure's signal
// must still come out the other side.
func TestContextBuilder_ForwardsUpstreamErrorWithoutTruncating(t *testing.T) {
	cb := NewContextBuilderStage(&ContextBuilderPolicy{TokenBudget: 1000})
	in := make(chan StreamElement, 2)
	in <- userElement("hi")
	in <- newTurnFailureSignal(errStageBoom)
	close(in)
	out := make(chan StreamElement, 4)

	require.NoError(t, cb.Process(context.Background(), in, out))

	var msgs int
	var errs []error
	for e := range out {
		if e.Message != nil {
			msgs++
		}
		if e.Error != nil {
			errs = append(errs, e.Error)
		}
	}
	assert.Equal(t, 1, msgs)
	require.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], errStageBoom)
}

// TestTemplateStage_ForwardsUpstreamErrorWithoutRendering: a turn-failure
// signal passes through TemplateStage, which does not render (and so does not report
// a second, derived error) for a turn that already failed.
func TestTemplateStage_ForwardsUpstreamErrorWithoutRendering(t *testing.T) {
	turnState := NewTurnState()
	turnState.Template = &prompt.Template{TaskType: "chat", RawTemplate: "Hi {{missing}}."}
	ts := NewTemplateStageWithTurnState(nil, turnState)
	in := make(chan StreamElement, 1)
	in <- newTurnFailureSignal(errStageBoom)
	close(in)
	out := make(chan StreamElement, 1)

	require.NoError(t, ts.Process(context.Background(), in, out))
	require.ErrorIs(t, (<-out).Error, errStageBoom)
}

// TestExecute_MetricsStageAfterUpstreamFailure is a regression guard: a
// MetricsStage leaf behind a failing stage must not hang the pipeline. It did
// when a stage failure canceled the other stages, because MetricsStage
// returned on ctx.Done without closing its output.
func TestExecute_MetricsStageAfterUpstreamFailure(t *testing.T) {
	for i := 0; i < 5; i++ {
		forwardThenFail := NewStageFunc("a", StageTypeTransform,
			func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				for e := range in {
					out <- e
				}
				return errStageBoom
			})
		slow := NewStageFunc("slow", StageTypeTransform,
			func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				for e := range in {
					time.Sleep(20 * time.Millisecond)
					out <- e
				}
				return nil
			})
		p, err := NewPipelineBuilder().Chain(forwardThenFail, NewMetricsStage(slow)).Build()
		require.NoError(t, err)

		in := make(chan StreamElement, 4)
		for j := 0; j < 4; j++ {
			in <- userElement("x")
		}
		close(in)
		out, err := p.Execute(context.Background(), in)
		require.NoError(t, err)

		done := make(chan []error)
		go func() { done <- errorElements(out) }()
		select {
		case errs := <-done:
			require.Len(t, errs, 1)
			assert.ErrorIs(t, errs[0], errStageBoom)
		case <-time.After(2 * time.Second):
			t.Fatalf("run %d: pipeline output never closed", i)
		}
	}
}

// TestMetricsStage_ClosesOutputOnCancel: MetricsStage closes its output when it
// returns on a canceled context, so its downstream is not left waiting.
func TestMetricsStage_ClosesOutputOnCancel(t *testing.T) {
	emitted := make(chan struct{})
	inner := NewStageFunc("inner", StageTypeTransform,
		func(ctx context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			out <- userElement("x")
			close(emitted)
			<-ctx.Done()
			return ctx.Err()
		})
	ms := NewMetricsStage(inner)
	output := make(chan StreamElement) // nobody reads: the forward blocks
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- ms.Process(ctx, make(chan StreamElement), output) }()
	<-emitted
	cancel()

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("MetricsStage did not return on cancel")
	}
	select {
	case _, open := <-output:
		assert.False(t, open, "output must be closed")
	case <-time.After(2 * time.Second):
		t.Fatal("MetricsStage returned without closing its output")
	}
}

// TestExecute_ProviderErrorKeepsPartialReplyThroughReorder is a regression
// guard for the OpenAI Realtime pipeline (ReorderInputTranscript): a duplex
// provider that emits its interrupted partial reply and an error, then returns
// the error, must still get that reply past TranscriptReorderStage to the save
// stage. It was dropped when a stage failure canceled the other stages.
func TestExecute_ProviderErrorKeepsPartialReplyThroughReorder(t *testing.T) {
	for i := 0; i < 50; i++ {
		prov := NewStageFunc("duplex_provider", StageTypeGenerate,
			func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				for range in { //nolint:revive // drain
				}
				out <- StreamElement{Message: &types.Message{Role: "user", Content: "hi"}}
				out <- StreamElement{Message: &types.Message{
					Role: "assistant", Content: "partial", FinishReason: types.FinishReasonInterrupted,
				}, EndOfStream: true}
				out <- NewErrorElement(errStageBoom)
				return errStageBoom
			})
		p, err := NewPipelineBuilder().
			Chain(prov, NewTranscriptReorderStage("[voice]"), NewPassthroughStage("sink")).
			Build()
		require.NoError(t, err)

		in := make(chan StreamElement)
		close(in)
		out, err := p.Execute(context.Background(), in)
		require.NoError(t, err)

		sawPartial := false
		var errs []error
		for e := range out {
			if e.Message != nil && e.Message.Content == "partial" {
				sawPartial = true
			}
			if e.Error != nil {
				errs = append(errs, e.Error)
			}
		}
		require.True(t, sawPartial, "run %d: the partial reply must reach the sink", i)
		require.Len(t, errs, 1, "run %d: the error must arrive once", i)
	}
}

// stageErrCountingProvider counts model calls.
type stageErrCountingProvider struct {
	providers.Provider
	mu    sync.Mutex
	calls int
}

func (p *stageErrCountingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record()
	return p.Provider.Predict(ctx, req)
}

func (p *stageErrCountingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record()
	return p.Provider.PredictStream(ctx, req)
}

func (p *stageErrCountingProvider) record() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
}

func (p *stageErrCountingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// TestDeliverTurnFailure_GivesUpWhenConsumerGone: with nobody reading and the
// execution's context done, delivering the turn's error must not block.
func TestDeliverTurnFailure_GivesUpWhenConsumerGone(t *testing.T) {
	output := make(chan StreamElement) // nobody reads
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	returned := make(chan struct{})
	go func() {
		deliverTurnFailure(ctx, output, errStageBoom)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("deliverTurnFailure blocked on an abandoned output")
	}
}

// TestDeliverTurnFailure_DeliversToReader: a reader that is there gets the
// error even when the context is already done.
func TestDeliverTurnFailure_DeliversToReader(t *testing.T) {
	output := make(chan StreamElement, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	deliverTurnFailure(ctx, output, errStageBoom)

	require.Len(t, output, 1)
	assert.ErrorIs(t, (<-output).Error, errStageBoom)
}

// TestExecuteSync_BranchUpstreamFailureSignalsEveryReader covers competitive
// (Branch) fan-out: every downstream stage still reading the shared channel
// gets the turn-failure signal before its input ends, and the caller always
// gets the error.
func TestExecuteSync_BranchUpstreamFailureSignalsEveryReader(t *testing.T) {
	for i := 0; i < 50; i++ {
		var signaled, unsignaled atomic.Int32
		forwardThenFail := NewStageFunc("a", StageTypeTransform,
			func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				for e := range in {
					out <- e
				}
				return errStageBoom
			})
		reader := func(name string) *StageFunc {
			return NewStageFunc(name, StageTypeTransform,
				func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
					defer close(out)
					saw := false
					for e := range in {
						if isTurnFailureSignal(&e) {
							saw = true
						}
					}
					if saw {
						signaled.Add(1)
					} else {
						unsignaled.Add(1) // a provider here would call the model
					}
					return nil
				})
		}
		p, err := NewPipelineBuilder().
			AddStage(forwardThenFail).AddStage(reader("p")).AddStage(reader("q")).
			Branch("a", "p", "q").Build()
		require.NoError(t, err)

		_, execErr := p.ExecuteSync(context.Background(), userElement("hi"))

		require.ErrorIs(t, execErr, errStageBoom, "run %d", i)
		require.Equal(t, int32(2), signaled.Load(), "run %d: both readers must get the signal", i)
		require.Zero(t, unsignaled.Load(), "run %d", i)
	}
}

// TestExecute_BranchLosesNoElementToAReturnedReader: on a competitive fan-out
// a reader that returns early does not take elements with it; the other
// reader gets them all.
func TestExecute_BranchLosesNoElementToAReturnedReader(t *testing.T) {
	const n = 5
	quitter := NewStageFunc("p", StageTypeTransform,
		func(_ context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			close(out)
			return nil
		})
	var got atomic.Int32
	reader := NewStageFunc("q", StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for e := range in {
				if e.Message != nil {
					got.Add(1)
				}
			}
			return nil
		})
	p, err := NewPipelineBuilder().
		AddStage(NewPassthroughStage("a")).AddStage(quitter).AddStage(reader).
		Branch("a", "p", "q").Build()
	require.NoError(t, err)

	elems := make([]StreamElement, n)
	for i := range elems {
		elems[i] = userElement("x")
	}
	_, execErr := p.ExecuteSync(context.Background(), elems...)
	require.NoError(t, execErr)
	assert.Equal(t, int32(n), got.Load())
}

// TestProviderStage_NonFatalErrorElementStillCallsModel: an error element a
// stage emits as a non-fatal report (FramesToMessageStage, MediaComposeStage)
// is not a turn failure; the provider still answers the turn.
func TestProviderStage_NonFatalErrorElementStillCallsModel(t *testing.T) {
	provider := &stageErrCountingProvider{Provider: mock.NewProvider("mock", "m", false)}
	ps := NewProviderStage(provider, nil, nil, nil)
	in := make(chan StreamElement, 2)
	in <- userElement("hi")
	in <- NewErrorElement(errors.New("frame 3 could not be decoded"))
	close(in)
	out := make(chan StreamElement, 8)

	require.NoError(t, ps.Process(context.Background(), in, out))
	assert.Equal(t, 1, provider.callCount())
}

// TestProviderStage_TurnFailureSignalSkipsModel is the other side: the
// pipeline's turn-failure signal stops the model call and is passed on.
func TestProviderStage_TurnFailureSignalSkipsModel(t *testing.T) {
	provider := &stageErrCountingProvider{Provider: mock.NewProvider("mock", "m", false)}
	ps := NewProviderStage(provider, nil, nil, nil)
	in := make(chan StreamElement, 2)
	in <- userElement("hi")
	in <- newTurnFailureSignal(NewStageError("template", StageTypeTransform, ErrTemplateUnresolved))
	close(in)
	out := make(chan StreamElement, 8)

	require.NoError(t, ps.Process(context.Background(), in, out))
	assert.Zero(t, provider.callCount())
	errs := errorElements(out)
	require.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], ErrTemplateUnresolved)
}

// TestRecordingStage_DoesNotRecordTurnFailureSignal: the signal passes every
// recorder downstream of the failure (input and output position both), and the
// completion event already carries the failure, so it is not recorded; an
// ordinary error element still is.
func TestRecordingStage_DoesNotRecordTurnFailureSignal(t *testing.T) {
	for _, pos := range []RecordingPosition{RecordingPositionInput, RecordingPositionOutput} {
		store := &fakeEventStore{}
		rs := NewRecordingStage(store, RecordingStageConfig{Position: pos, SessionID: "s"})
		in := make(chan StreamElement, 2)
		in <- newTurnFailureSignal(NewStageError("template", StageTypeTransform, ErrTemplateUnresolved))
		in <- NewErrorElement(errStageBoom)
		close(in)
		out := make(chan StreamElement, 2)

		require.NoError(t, rs.Process(context.Background(), in, out))

		captured := store.filterByType(events.EventStreamInterrupted)
		require.Len(t, captured, 1, "position %v", pos)
		assert.Contains(t, captured[0].Data.(*events.StreamInterruptedData).Reason, "stage boom")
	}
}
