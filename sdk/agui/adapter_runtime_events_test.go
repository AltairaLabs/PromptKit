package agui

import (
	"context"
	"testing"
	"time"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// busSender is a Sender whose Send publishes runtime events to a bus, then
// holds the turn open until the test releases it.
type busSender struct {
	mockSender
	bus     *events.EventBus
	publish []*events.Event
	release chan struct{}
}

func (b *busSender) EventBus() events.Bus { return b.bus }

func (b *busSender) Send(ctx context.Context, _ any, _ ...sdk.SendOption) (*sdk.Response, error) {
	for _, e := range b.publish {
		b.bus.Publish(e)
	}
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return newTestResponse("done", nil), nil
}

func newBusSender(t *testing.T, publish ...*events.Event) *busSender {
	t.Helper()
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	return &busSender{bus: bus, publish: publish, release: make(chan struct{})}
}

// runUntilRaw runs one RunSend and reads its events. It releases the turn once
// a RAW event arrives, or after wait when none has, and returns every event.
func runUntilRaw(t *testing.T, s *busSender, wait time.Duration, opts ...AdapterOption) []aguievents.Event {
	t.Helper()
	a := newTestAdapter(s, s, opts...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- a.RunSend(ctx, userMsg("hi")) }()

	var evts []aguievents.Event
	timer := time.NewTimer(wait)
	defer timer.Stop()
	released := false
	release := func() {
		if !released {
			released = true
			close(s.release)
		}
	}
	for ch := a.Events(); ch != nil; {
		select {
		case ev, ok := <-ch:
			if !ok {
				ch = nil
				continue
			}
			evts = append(evts, ev)
			if ev.Type() == aguievents.EventTypeRaw {
				release()
			}
		case <-timer.C:
			release()
		}
	}
	require.NoError(t, <-errCh)
	requireValidSequence(t, evts)
	return evts
}

func TestWithRuntimeEvents_ForwardsBusEventsAsRaw(t *testing.T) {
	published := &events.Event{
		Type:      events.EventProviderCallStarted,
		SessionID: "s1",
		Data:      &events.ProviderCallStartedData{Provider: "mock", Model: "m1"},
	}
	s := newBusSender(t, published)

	evts := runUntilRaw(t, s, 2*time.Second, WithRuntimeEvents())

	raws := eventsOf[*aguievents.RawEvent](evts)
	require.Len(t, raws, 1, "the bus event must reach the stream:\n%s", describeEvents(evts))
	require.NotNil(t, raws[0].Source)
	assert.Equal(t, "promptkit", *raws[0].Source)
	got, ok := raws[0].Event.(events.Event)
	require.True(t, ok, "payload = %T, want events.Event", raws[0].Event)
	assert.Equal(t, events.EventProviderCallStarted, got.Type)
	assert.Equal(t, "s1", got.SessionID)
	data, ok := got.Data.(*events.ProviderCallStartedData)
	require.True(t, ok)
	assert.Equal(t, "m1", data.Model)
}

func TestWithRuntimeEvents_ForwardsOnlyNamedTypes(t *testing.T) {
	s := newBusSender(t,
		&events.Event{Type: events.EventProviderCallStarted, Data: &events.ProviderCallStartedData{}},
		&events.Event{Type: events.EventToolCallStarted, Data: &events.ToolCallStartedData{ToolName: "lookup"}},
	)

	evts := runUntilRaw(t, s, 2*time.Second, WithRuntimeEvents(events.EventToolCallStarted))

	raws := eventsOf[*aguievents.RawEvent](evts)
	require.Len(t, raws, 1, "only the named type is forwarded:\n%s", describeEvents(evts))
	assert.Equal(t, events.EventToolCallStarted, raws[0].Event.(events.Event).Type)
}

func TestRuntimeEvents_OffByDefault(t *testing.T) {
	s := newBusSender(t, &events.Event{Type: events.EventProviderCallStarted, Data: &events.ProviderCallStartedData{}})

	evts := runUntilRaw(t, s, 100*time.Millisecond)

	assert.Empty(t, eventsOf[*aguievents.RawEvent](evts), "no RAW event without WithRuntimeEvents")
	assert.Equal(t, aguievents.EventTypeRunFinished, evts[len(evts)-1].Type())
}

func TestRuntimeEvents_NilBusForwardsNothing(t *testing.T) {
	a := newTestAdapter(&mockSender{resp: newTestResponse("ok", nil)}, &mockEventBusProvider{}, WithRuntimeEvents())

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)
	assert.Empty(t, eventsOf[*aguievents.RawEvent](evts))
}
