package guardrails

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// eventSink collects validation events off the bus.
type eventSink struct {
	mu     sync.Mutex
	events []*events.Event
}

func (s *eventSink) record(e *events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *eventSink) typesSeen() []events.EventType {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]events.EventType, 0, len(s.events))
	for _, e := range s.events {
		out = append(out, e.Type)
	}
	return out
}

// awaitCount waits for n events. The bus delivers on a worker pool, so reading
// the sink straight after the call under test races the delivery and reports an
// empty slice.
func (s *eventSink) awaitCount(t *testing.T, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.events) >= n
	}, 2*time.Second, 5*time.Millisecond, "expected %d validation events", n)
}

// awaitQuiet gives the bus a chance to deliver anything else that was emitted,
// so "exactly these events and no more" assertions cannot pass by racing.
func (s *eventSink) awaitQuiet() {
	time.Sleep(50 * time.Millisecond)
}

func (s *eventSink) first(t events.EventType) *events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if e.Type == t {
			return e
		}
	}
	return nil
}

// observedAdapter wires an adapter to a bus and returns the sink watching the
// three validation event types.
func observedAdapter(t *testing.T, handler evals.EvalTypeHandler, evalType string) (*GuardrailHookAdapter, *eventSink) {
	t.Helper()
	bus := events.NewEventBus()
	sink := &eventSink{}
	for _, et := range []events.EventType{
		events.EventValidationStarted, events.EventValidationPassed, events.EventValidationFailed,
	} {
		bus.Subscribe(et, func(e *events.Event) { sink.record(e) })
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  evalType,
		params:    map[string]any{},
		direction: DirectionOutput,
		emitter:   events.NewEmitter(bus, "exec-1", "sess-1", "conv-1"),
	}
	t.Cleanup(func() { bus.Close() })
	return adapter, sink
}

// A guardrail that passes must still open and close a validation span.
// validation.started is the ONLY place the OTel listener creates the span and
// sets promptkit.guardrail=true; without it every later end is dropped into
// pendingEnds and no span is ever exported.
func TestGuardrailHookAdapter_EmitsStartedAndPassed(t *testing.T) {
	handler := &stubHandler{typeName: "test_pass", result: &evals.EvalResult{Score: floatPtr(1.0)}}
	adapter, sink := observedAdapter(t, handler, "test_pass")

	d := adapter.AfterCall(context.Background(),
		nil, &hooks.ProviderResponse{Message: types.Message{Content: "hello world"}})
	require.True(t, d.Allow)

	sink.awaitCount(t, 2)
	// ElementsMatch, not Equal: the bus dispatches on a worker pool, so
	// delivery order is not publish order and asserting a sequence here is
	// asserting a guarantee the bus does not make.
	assert.ElementsMatch(t,
		[]events.EventType{events.EventValidationStarted, events.EventValidationPassed},
		sink.typesSeen(),
		"a passing guardrail must emit both started and passed — the pass is the metrics denominator")

	started := sink.first(events.EventValidationStarted)
	require.NotNil(t, started)
	data, ok := started.Data.(*events.ValidationEventData)
	require.True(t, ok)
	assert.Equal(t, "test_pass", data.ValidatorName)
	assert.Equal(t, "test_pass", data.ValidatorType)
}

// The ProviderStage owns validation.failed (it alone knows Enforced/Direction
// and covers func-backed guardrails). The adapter must open the span but must
// NOT emit the failure, or every firing is counted twice.
func TestGuardrailHookAdapter_EmitsStartedButNotFailedOnFiring(t *testing.T) {
	handler := &stubHandler{typeName: "test_fail", result: &evals.EvalResult{
		Score: floatPtr(0.0), Explanation: "content violation detected",
	}}
	adapter, sink := observedAdapter(t, handler, "test_fail")

	d := adapter.AfterCall(context.Background(),
		nil, &hooks.ProviderResponse{Message: types.Message{Content: "bad content"}})
	require.False(t, d.Allow)

	sink.awaitCount(t, 1)
	sink.awaitQuiet()
	assert.ElementsMatch(t, []events.EventType{events.EventValidationStarted}, sink.typesSeen(),
		"the stage emits the failure; a second one here would double-count every firing")
}

// A guardrail with no emitter is the ordinary case for direct construction and
// must behave exactly as before.
func TestGuardrailHookAdapter_NilEmitterIsSafe(t *testing.T) {
	handler := &stubHandler{typeName: "test_pass", result: &evals.EvalResult{Score: floatPtr(1.0)}}
	adapter := &GuardrailHookAdapter{
		handler: handler, evalType: "test_pass", params: map[string]any{}, direction: DirectionOutput,
	}

	require.NotPanics(t, func() {
		d := adapter.AfterCall(context.Background(),
			nil, &hooks.ProviderResponse{Message: types.Message{Content: "hello"}})
		assert.True(t, d.Allow)
	})
}

// The emitter arrives through the existing functional-options constructor, so
// callers that do not pass one are unaffected.
func TestWithEmitter_SetsTheAdaptersEmitter(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	emitter := events.NewEmitter(bus, "e", "s", "c")

	hook, err := NewGuardrailHook("length", map[string]any{"max": 10}, WithEmitter(emitter))
	require.NoError(t, err)

	adapter, ok := hook.(*GuardrailHookAdapter)
	require.True(t, ok)
	assert.Same(t, emitter, adapter.emitter)
}
