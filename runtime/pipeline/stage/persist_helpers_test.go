package stage

// Copies of the persist test helpers that stage's own tests use (#2876): the
// tests that chain a persist stage with the provider stage, the duplex stage,
// the cue stage or playout hold stayed in stage.

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// fakeEventStore captures appended events for assertions.
// Implements events.EventStore.
type fakeEventStore struct {
	mu     sync.Mutex
	events []*events.Event
}

func (f *fakeEventStore) Append(_ context.Context, e *events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}
func (f *fakeEventStore) OnEvent(*events.Event) {}
func (f *fakeEventStore) Query(_ context.Context, _ *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}
func (f *fakeEventStore) QueryRaw(_ context.Context, _ *events.EventFilter) ([]*events.StoredEvent, error) {
	return nil, nil
}
func (f *fakeEventStore) Stream(_ context.Context, _ string) (<-chan *events.Event, error) {
	return nil, nil
}
func (f *fakeEventStore) Close() error { return nil }

// snapshot returns a copy of recorded events safe for inspection.
func (f *fakeEventStore) snapshot() []*events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*events.Event, len(f.events))
	copy(out, f.events)
	return out
}

// filterByType returns events whose Type matches.
func (f *fakeEventStore) filterByType(t events.EventType) []*events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*events.Event
	for _, e := range f.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// realMessage builds a message the way a live turn carries it: a user message
// with its text in Parts and Content empty, an assistant reply with its text
// in Content and no Parts.
func realMessage(role, text string) types.Message {
	if role == core.RoleUser {
		t := text
		return types.Message{
			Role:  role,
			Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: &t}},
		}
	}
	return types.Message{Role: role, Content: text}
}

func interruptedReply(content string) types.Message {
	return types.Message{Role: "assistant", Content: content, FinishReason: types.FinishReasonInterrupted}
}

// recordingSummarizer records the batch it was asked to summarize.
type recordingSummarizer struct {
	batch []types.Message
}

func (r *recordingSummarizer) Summarize(_ context.Context, msgs []types.Message) (string, error) {
	r.batch = msgs
	return "summary", nil
}
