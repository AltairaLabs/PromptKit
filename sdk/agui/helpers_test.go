package agui

import (
	"context"
	"testing"
	"time"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// collectTimeout bounds how long a test waits for the adapter to close its
// channel. A healthy run closes it in milliseconds; this only stops a hung
// adapter from hanging the suite.
const collectTimeout = 5 * time.Second

// newTestAdapter creates an adapter from separate sender and event bus provider.
// This is used in tests where the sender and event bus may be separate mocks.
func newTestAdapter(sender Sender, ebp EventBusProvider, opts ...AdapterOption) *EventAdapter {
	return newAdapter(sender, ebp, opts...)
}

// collectEvents drains all events from the adapter's channel into a slice.
// This is useful for testing. It blocks until the channel is closed.
func collectEvents(ch <-chan aguievents.Event) []aguievents.Event {
	var result []aguievents.Event
	for ev := range ch {
		result = append(result, ev)
	}
	return result
}

// runAndCollect runs one adapter run while a concurrent reader drains the
// events, then checks the stream against the AG-UI lifecycle rules. Every
// adapter test goes through it, so every test is also a conformance test.
func runAndCollect(
	t *testing.T, a *EventAdapter, run func(ctx context.Context) error,
) ([]aguievents.Event, error) {
	t.Helper()
	return continueAndCollect(t, a, nil, run)
}

// continueAndCollect is runAndCollect for a run that continues a thread: the
// stream is checked together with the earlier runs' events, as one stream
// carrying the thread's runs in order, so a result for a call an earlier run
// left open is judged against that call.
func continueAndCollect(
	t *testing.T, a *EventAdapter, earlier []aguievents.Event, run func(ctx context.Context) error,
) ([]aguievents.Event, error) {
	t.Helper()
	collected := make(chan []aguievents.Event, 1)
	go func() { collected <- collectEvents(a.Events()) }()

	err := run(context.Background())

	var evts []aguievents.Event
	select {
	case evts = <-collected:
	case <-time.After(collectTimeout):
		t.Fatal("timed out waiting for the adapter to close its event channel")
	}
	requireValidSequence(t, append(append([]aguievents.Event{}, earlier...), evts...))
	return evts, err
}

// sendAndCollect is runAndCollect for the common RunSend case.
func sendAndCollect(t *testing.T, a *EventAdapter, text string) ([]aguievents.Event, error) {
	t.Helper()
	return runAndCollect(t, a, func(ctx context.Context) error { return a.RunSend(ctx, userMsg(text)) })
}

// eventTypes lists the types of evts in order.
func eventTypes(evts []aguievents.Event) []aguievents.EventType {
	out := make([]aguievents.EventType, len(evts))
	for i, ev := range evts {
		out[i] = ev.Type()
	}
	return out
}

// eventsOf returns the events of type T in order.
func eventsOf[T aguievents.Event](evts []aguievents.Event) []T {
	var out []T
	for _, ev := range evts {
		if e, ok := ev.(T); ok {
			out = append(out, e)
		}
	}
	return out
}
