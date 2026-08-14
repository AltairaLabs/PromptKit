package guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// observedFuncGuardrail builds a func guardrail wired to a bus, mirroring what
// the provider stage does through hooks.EmitterAware.
func observedFuncGuardrail(t *testing.T, spec Spec) (hooks.ProviderHook, *eventSink) {
	t.Helper()
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	sink := &eventSink{}
	for _, et := range []events.EventType{
		events.EventValidationStarted, events.EventValidationPassed, events.EventValidationFailed,
	} {
		bus.Subscribe(et, func(e *events.Event) { sink.record(e) })
	}

	hook, err := spec.build(evals.NewEvalTypeRegistry())
	require.NoError(t, err)

	aware, ok := hook.(hooks.EmitterAware)
	require.True(t, ok, "a func guardrail must accept the conversation's emitter like any other")
	aware.SetEmitter(events.NewEmitter(bus, "exec-1", "sess-1", "conv-1"))
	return hook, sink
}

func userReq(text string) *hooks.ProviderRequest {
	return &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: text}},
	}
}

// A passing func guardrail must open and close its span, exactly as the
// eval-backed one does. Without started the OTel listener never creates the
// span and the end is discarded into pendingEnds.
func TestFuncGuardrail_EmitsStartedAndPassedOnOutput(t *testing.T) {
	hook, sink := observedFuncGuardrail(t, OutputFunc("clean-output",
		func(_ context.Context, _ *hooks.OutputRequest) hooks.Decision { return hooks.Allow }))

	d := hook.AfterCall(context.Background(), nil,
		&hooks.ProviderResponse{Message: types.Message{Content: "all good"}})
	require.True(t, d.Allow)

	sink.awaitCount(t, 2)
	assert.ElementsMatch(t,
		[]events.EventType{events.EventValidationStarted, events.EventValidationPassed},
		sink.typesSeen())

	started := sink.first(events.EventValidationStarted)
	require.NotNil(t, started)
	data, ok := started.Data.(*events.ValidationEventData)
	require.True(t, ok)
	assert.Equal(t, "clean-output", data.ValidatorName, "the caller's name identifies the guardrail")
}

func TestFuncGuardrail_EmitsStartedAndPassedOnInput(t *testing.T) {
	hook, sink := observedFuncGuardrail(t, InputFunc("clean-input",
		func(_ context.Context, _ *hooks.InputRequest) hooks.Decision { return hooks.Allow }))

	d := hook.BeforeCall(context.Background(), userReq("hello"))
	require.True(t, d.Allow)

	sink.awaitCount(t, 2)
	assert.ElementsMatch(t,
		[]events.EventType{events.EventValidationStarted, events.EventValidationPassed},
		sink.typesSeen())
}

// The stage emits the failure, as it does for eval-backed guardrails. A second
// one here would double-count every firing.
func TestFuncGuardrail_EmitsStartedButNotFailedOnFiring(t *testing.T) {
	hook, sink := observedFuncGuardrail(t, OutputFunc("blocker",
		func(_ context.Context, _ *hooks.OutputRequest) hooks.Decision {
			return hooks.Enforced("nope", nil)
		}))

	d := hook.AfterCall(context.Background(), nil,
		&hooks.ProviderResponse{Message: types.Message{Content: "bad"}})
	require.False(t, d.Allow)

	sink.awaitCount(t, 1)
	sink.awaitQuiet()
	assert.ElementsMatch(t, []events.EventType{events.EventValidationStarted}, sink.typesSeen())
}

// A guardrail whose function never runs — the input gate skipped it — must not
// open a span it will never close.
func TestFuncGuardrail_SkippedTurnEmitsNothing(t *testing.T) {
	hook, sink := observedFuncGuardrail(t, InputFunc("input-only",
		func(_ context.Context, _ *hooks.InputRequest) hooks.Decision { return hooks.Allow }))

	// Last message is a tool result, not user input: BeforeCall skips.
	d := hook.BeforeCall(context.Background(), &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "assistant", Content: "tool result"}},
	})
	require.True(t, d.Allow)

	sink.awaitQuiet()
	assert.Empty(t, sink.typesSeen(), "a guardrail that did not run must not open a span")
}

func TestFuncGuardrail_NilEmitterIsSafe(t *testing.T) {
	hook, err := OutputFunc("no-emitter",
		func(_ context.Context, _ *hooks.OutputRequest) hooks.Decision { return hooks.Allow },
	).build(evals.NewEvalTypeRegistry())
	require.NoError(t, err)

	require.NotPanics(t, func() {
		d := hook.AfterCall(context.Background(), nil,
			&hooks.ProviderResponse{Message: types.Message{Content: "hi"}})
		assert.True(t, d.Allow)
	})
}
