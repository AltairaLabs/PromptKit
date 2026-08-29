package stage

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// runBroadcast drives the stage over the given elements and returns the
// message.created payloads a bus subscriber received, in order. want is the
// number expected; the bus dispatches through a worker pool, so we wait for
// that many rather than sleeping a fixed interval.
func runBroadcast(t *testing.T, elems []StreamElement, want int) []*events.MessageCreatedData {
	t.Helper()

	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	var mu sync.Mutex
	var got []*events.MessageCreatedData
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		if d, ok := e.Data.(*events.MessageCreatedData); ok {
			mu.Lock()
			got = append(got, d)
			mu.Unlock()
		}
	})

	s := NewMessageBroadcastStage(events.NewEmitter(bus, "run", "sess", "conv"))

	in := make(chan StreamElement, len(elems)+1)
	for _, e := range elems {
		in <- e
	}
	close(in)

	out := make(chan StreamElement, len(elems)+4)
	require.NoError(t, s.Process(context.Background(), in, out))
	for range out { //nolint:revive // draining
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A short settle so an unexpected EXTRA event is still caught.
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	out2 := make([]*events.MessageCreatedData, len(got))
	copy(out2, got)
	// The bus dispatches through a worker pool, so ARRIVAL order is not publish
	// order. Sorting by Index is not a convenience here — it is the point:
	// Index is what lets a live consumer reassemble a transcript from a bus
	// that makes no ordering promise.
	sort.Slice(out2, func(i, j int) bool { return out2[i].Index < out2[j].Index })
	return out2
}

func historyMsgElem(role, content string) StreamElement {
	m := types.Message{Role: role, Content: content, Source: "statestore"}
	e := NewMessageElement(&m)
	e.Meta.FromHistory = true
	return e
}

func liveMsgElem(role, content string) StreamElement {
	m := types.Message{Role: role, Content: content}
	return NewMessageElement(&m)
}

// TestMessageBroadcastStage_SkipsHistoryAndIndexesAbsolutely covers the two
// behaviours together: replayed history is counted for position but never
// re-published, and the published index continues from the persisted
// transcript rather than restarting each turn.
func TestMessageBroadcastStage_SkipsHistoryAndIndexesAbsolutely(t *testing.T) {
	got := runBroadcast(t, []StreamElement{
		historyMsgElem("user", "old q"),
		historyMsgElem("assistant", "old a"),
		liveMsgElem("user", "new q"),
		liveMsgElem("assistant", "new a"),
	}, 2)

	require.Len(t, got, 2, "replayed history must not be re-published")
	assert.Equal(t, "new q", got[0].Content)
	assert.Equal(t, 2, got[0].Index, "index continues from the replayed transcript")
	assert.Equal(t, "new a", got[1].Content)
	assert.Equal(t, 3, got[1].Index)
}

// TestMessageBroadcastStage_StripsBinary is the bus-route half of the one
// deliberate difference from the recording route.
func TestMessageBroadcastStage_StripsBinary(t *testing.T) {
	raw := "AAAABBBBCCCC"
	m := types.Message{
		Role: "user",
		Parts: []types.ContentPart{{
			Type:  "image",
			Media: &types.MediaContent{Data: &raw, MIMEType: "image/png"},
		}},
	}
	got := runBroadcast(t, []StreamElement{NewMessageElement(&m)}, 1)

	require.Len(t, got, 1)
	require.Len(t, got[0].Parts, 1)
	require.NotNil(t, got[0].Parts[0].Media)
	assert.Nil(t, got[0].Parts[0].Media.Data, "binary must not reach the bus")
	assert.Equal(t, "image/png", got[0].Parts[0].Media.MIMEType, "metadata is retained")

	require.NotNil(t, m.Parts[0].Media.Data, "the caller's message must be untouched")
}

// TestMessageBroadcastStage_PassesElementsThrough — it is a tap, not a filter.
func TestMessageBroadcastStage_PassesElementsThrough(t *testing.T) {
	s := NewMessageBroadcastStage(nil)

	in := make(chan StreamElement, 2)
	m := types.Message{Role: "user", Content: "hi"}
	in <- NewMessageElement(&m)
	close(in)

	out := make(chan StreamElement, 4)
	require.NoError(t, s.Process(context.Background(), in, out))

	var n int
	for range out {
		n++
	}
	assert.Equal(t, 1, n, "a nil emitter must not swallow elements")
}

// TestMessageBroadcastStage_ToolLoopRoundsArriveSeparately pins the liveness
// the stage exists for: a tool-calling round and the final answer are distinct
// events in order, not one batch at end of turn.
func TestMessageBroadcastStage_ToolLoopRoundsArriveSeparately(t *testing.T) {
	toolCallMsg := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "lookup", Args: []byte(`{"q":"x"}`)},
		},
	}
	toolResultMsg := types.Message{
		Role:       "tool",
		ToolResult: &types.MessageToolResult{ID: "c1", Name: "lookup"},
	}
	finalMsg := types.Message{Role: "assistant", Content: "the answer"}

	got := runBroadcast(t, []StreamElement{
		liveMsgElem("user", "ask"),
		NewMessageElement(&toolCallMsg),
		NewMessageElement(&toolResultMsg),
		NewMessageElement(&finalMsg),
	}, 4)

	require.Len(t, got, 4)
	for i, d := range got {
		assert.Equalf(t, i, d.Index, "event %d carries the wrong index", i)
	}
	require.Len(t, got[1].ToolCalls, 1, "the tool-calling round is its own event")
	assert.Equal(t, "lookup", got[1].ToolCalls[0].Name)
	require.NotNil(t, got[2].ToolResult, "the tool result is its own event")
	assert.Equal(t, "the answer", got[3].Content, "the final answer is a separate event")
}
