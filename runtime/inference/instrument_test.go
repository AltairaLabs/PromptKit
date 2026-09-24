package inference_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeInferProvider is a minimal inference.Provider whose Infer behavior is
// scripted per test, so Instrument's timing, event, and pass-through
// behavior can be observed independently of any real vendor backend.
type fakeInferProvider struct {
	resp  inference.Response
	err   error
	sleep time.Duration
}

func (f *fakeInferProvider) Infer(_ context.Context, _ inference.Request) (inference.Response, error) {
	if f.sleep > 0 {
		time.Sleep(f.sleep)
	}
	return f.resp, f.err
}

// waitForEvent blocks until an event arrives on ch or the timeout elapses.
// The event bus dispatches asynchronously via a worker pool, so tests must
// wait rather than assume synchronous delivery.
func waitForEvent(t *testing.T, ch <-chan *events.Event, timeout time.Duration) *events.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(timeout):
		t.Fatal("timed out waiting for event")
		return nil
	}
}

func TestInstrument_Success_EmitsCompletedEvent(t *testing.T) {
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-1", "session-1", "conv-1")

	ch := make(chan *events.Event, 1)
	bus.Subscribe(events.EventInferenceCallCompleted, func(e *events.Event) {
		ch <- e
	})

	fake := &fakeInferProvider{
		resp: inference.Response{
			Model: "facebook/bart-large-mnli",
			Usage: inference.Usage{InputTokens: 12, Cost: 0.0004},
		},
		sleep: 5 * time.Millisecond,
	}
	p := inference.Instrument(fake, "hf", "huggingface")

	ctx := inference.WithEmitter(context.Background(), emitter)
	resp, err := p.Infer(ctx, inference.Request{Model: "facebook/bart-large-mnli"})
	require.NoError(t, err)
	assert.Equal(t, "facebook/bart-large-mnli", resp.Model)

	e := waitForEvent(t, ch, 500*time.Millisecond)
	data, ok := e.Data.(*events.InferenceCallCompletedData)
	require.True(t, ok, "unexpected data type %T", e.Data)
	assert.Equal(t, "hf", data.Provider)
	assert.Equal(t, "huggingface", data.Source)
	assert.Equal(t, "inference", data.Capability)
	assert.Equal(t, "facebook/bart-large-mnli", data.Model)
	assert.Equal(t, 12, data.InputTokens)
	assert.InDelta(t, 0.0004, data.Cost, 0.00001)
	assert.Greater(t, data.Duration, time.Duration(0))
}

func TestInstrument_Success_FallsBackToRequestModel(t *testing.T) {
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-1b", "session-1b", "conv-1b")

	ch := make(chan *events.Event, 1)
	bus.Subscribe(events.EventInferenceCallCompleted, func(e *events.Event) {
		ch <- e
	})

	fake := &fakeInferProvider{resp: inference.Response{}} // no Model in response
	p := inference.Instrument(fake, "hf", "huggingface")

	ctx := inference.WithEmitter(context.Background(), emitter)
	_, err := p.Infer(ctx, inference.Request{Model: "req-model"})
	require.NoError(t, err)

	e := waitForEvent(t, ch, 500*time.Millisecond)
	data, ok := e.Data.(*events.InferenceCallCompletedData)
	require.True(t, ok, "unexpected data type %T", e.Data)
	assert.Equal(t, "req-model", data.Model)
}

func TestInstrument_Error_EmitsFailedEvent(t *testing.T) {
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-2", "session-2", "conv-2")

	ch := make(chan *events.Event, 1)
	bus.Subscribe(events.EventInferenceCallFailed, func(e *events.Event) {
		ch <- e
	})

	wantErr := errors.New("rate limit exceeded")
	fake := &fakeInferProvider{err: wantErr}
	p := inference.Instrument(fake, "hf", "huggingface")

	ctx := inference.WithEmitter(context.Background(), emitter)
	_, err := p.Infer(ctx, inference.Request{})
	require.Error(t, err)
	assert.Equal(t, wantErr, err)

	e := waitForEvent(t, ch, 500*time.Millisecond)
	data, ok := e.Data.(*events.InferenceCallFailedData)
	require.True(t, ok, "unexpected data type %T", e.Data)
	assert.Equal(t, "hf", data.Provider)
	assert.Equal(t, "rate limit exceeded", data.Error)
}

func TestInstrument_NoEmitterOnContext_PassesThroughWithoutPanic(t *testing.T) {
	fake := &fakeInferProvider{
		resp: inference.Response{Model: "m"},
	}
	p := inference.Instrument(fake, "hf", "huggingface")

	resp, err := p.Infer(context.Background(), inference.Request{})
	require.NoError(t, err)
	assert.Equal(t, "m", resp.Model)
}

func TestInstrument_NilProvider_ReturnsNil(t *testing.T) {
	// Divergence case: nil in must give nil out, but a real provider in must
	// give back a working wrapper — otherwise an implementation that always
	// returns nil (or always returns a non-nil wrapper regardless of p) would
	// also pass a nil-only check.
	assert.Nil(t, inference.Instrument(nil, "hf", "huggingface"))

	fake := &fakeInferProvider{resp: inference.Response{Model: "m"}}
	wrapped := inference.Instrument(fake, "hf", "huggingface")
	require.NotNil(t, wrapped)

	resp, err := wrapped.Infer(context.Background(), inference.Request{})
	require.NoError(t, err)
	assert.Equal(t, "m", resp.Model)
}

// A nil emitter (a runner or adapter configured without one) must not hide an
// emitter an outer caller already put on the context.
func TestWithEmitter_NilDoesNotReplaceAnOuterEmitter(t *testing.T) {
	bus := events.NewEventBus()
	outer := events.NewEmitter(bus, "run-1", "session-1", "conv-1")
	ch := make(chan *events.Event, 1)
	bus.Subscribe(events.EventInferenceCallCompleted, func(e *events.Event) { ch <- e })

	ctx := inference.WithEmitter(context.Background(), outer)
	ctx = inference.WithEmitter(ctx, nil)
	_, err := inference.Instrument(&fakeInferProvider{}, "p", "t").Infer(ctx, inference.Request{})

	require.NoError(t, err)
	ev := waitForEvent(t, ch, time.Second)
	assert.Equal(t, events.EventInferenceCallCompleted, ev.Type)
}
