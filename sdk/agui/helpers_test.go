package agui

import (
	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

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
