package stage

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
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

// realMessage builds a message the way the system actually produces one.
//
// Observed on a live tool-calling turn (see
// sdk/message_created_live_integration_test.go): a USER message carries its
// text in Parts with Content EMPTY, while an assistant reply carries it in
// Content with no Parts. Fixtures that set Content on both — as these once did
// — agree with the code under test and with nothing else, which is how a live
// route shipped with a blank user turn.
func realMessage(role, text string) types.Message {
	if role == roleUser {
		t := text
		return types.Message{
			Role:  role,
			Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: &t}},
		}
	}
	return types.Message{Role: role, Content: text}
}

func historyMsgElem(role, content string) StreamElement {
	m := realMessage(role, content)
	m.Source = "statestore"
	e := NewMessageElement(&m)
	e.Meta.FromHistory = true
	return e
}

func liveMsgElem(role, content string) StreamElement {
	m := realMessage(role, content)
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
	assert.Equal(t, "new q", got[0].GetContent())
	assert.Equal(t, 2, got[0].Index, "index continues from the replayed transcript")
	assert.Equal(t, "new a", got[1].GetContent())
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
	assert.Equal(t, "the answer", got[3].GetContent(), "the final answer is a separate event")
}

// TestMessageBroadcastStage_IndexIsArrivalPosition pins the precondition the
// stage depends on, by showing what happens when it is violated.
//
// Index is literally the element's position in the stream this Process call
// saw. That is transcript-absolute ONLY because the provider re-emits the
// accumulated transcript in order down a linear chain. Feed the same messages
// in a different order — as any path that reorders would — and Index follows
// arrival, not the transcript.
//
// This is not a bug to fix here; it is the contract to place the stage by.
// See the precondition on MessageBroadcastStage.
func TestMessageBroadcastStage_IndexIsArrivalPosition(t *testing.T) {
	// Transcript order would be first, second, third. Deliver them scrambled.
	got := runBroadcast(t, []StreamElement{
		liveMsgElem("user", "third"),
		liveMsgElem("user", "first"),
		liveMsgElem("user", "second"),
	}, 3)

	require.Len(t, got, 3)
	// runBroadcast sorts by Index, so got[0] is whatever arrived first.
	assert.Equal(t, "third", got[0].GetContent(),
		"Index tracks arrival position; a reordering path yields a wrong transcript index")
	assert.Equal(t, 0, got[0].Index)
	assert.Equal(t, "first", got[1].GetContent())
	assert.Equal(t, "second", got[2].GetContent())
}

// TestMessageBroadcastStage_DownstreamOfMergeStillPublishesEveryMessage places
// the stage after a fan-in, the topology its precondition warns about.
//
// What survives: completeness. Every message is still published exactly once.
// What does NOT: order, and therefore Index. MergeStage spawns a goroutine per
// input (stages_advanced.go), so the interleaving is nondeterministic — which
// is why this test asserts counts and content, never positions. Asserting an
// order here would be asserting a race.
func TestMessageBroadcastStage_DownstreamOfMergeStillPublishesEveryMessage(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	var mu sync.Mutex
	seen := map[string]int{}
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		if d, ok := e.Data.(*events.MessageCreatedData); ok {
			mu.Lock()
			seen[d.Content]++
			mu.Unlock()
		}
	})

	emitMsgs := func(name string, contents ...string) *StageFunc {
		return NewStageFunc(name, StageTypeGenerate,
			func(ctx context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				for _, c := range contents {
					m := types.Message{Role: "assistant", Content: c}
					select {
					case out <- NewMessageElement(&m):
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			})
	}

	a := emitMsgs("a", "a1", "a2")
	b := emitMsgs("b", "b1")
	merge := NewMergeStage("merge", 2)
	bcast := NewMessageBroadcastStage(events.NewEmitter(bus, "run", "sess", "conv"))

	p, err := NewPipelineBuilder().
		AddStage(a).AddStage(b).AddStage(merge).AddStage(bcast).
		Merge("merge", "a", "b").
		Connect("merge", bcast.Name()).
		Build()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	in := make(chan StreamElement)
	close(in)
	out, err := p.Execute(ctx, in)
	require.NoError(t, err)
	for range out { //nolint:revive // draining
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, map[string]int{"a1": 1, "a2": 1, "b1": 1}, seen,
		"every message from both branches is published exactly once")
}

// binaryBearingMessage carries inline bytes in every place a message can hold
// them: content parts, and a tool result's parts.
func binaryBearingMessage() types.Message {
	img := "AAAABBBBCCCCDDDDEEEEFFFF"
	aud := "ZZZZYYYYXXXXWWWWVVVVUUUU"
	return types.Message{
		Role:    "assistant",
		Content: "here is the image you asked for",
		Parts: []types.ContentPart{{
			Type:  "image",
			Media: &types.MediaContent{Data: &img, MIMEType: "image/png"},
		}},
		ToolResult: &types.MessageToolResult{
			ID:   "call_1",
			Name: "record_audio",
			Parts: []types.ContentPart{{
				Type:  "audio",
				Media: &types.MediaContent{Data: &aud, MIMEType: "audio/pcm"},
			}},
		},
	}
}

// inlineBinaryIn reports every place the payload still holds raw bytes.
func inlineBinaryIn(d *events.MessageCreatedData) []string {
	var found []string
	check := func(where string, parts []types.ContentPart) {
		for i, p := range parts {
			if p.Media != nil && p.Media.Data != nil && *p.Media.Data != "" {
				found = append(found, where+"["+string(rune('0'+i))+"]")
			}
			if p.Media != nil && p.Media.FilePath != nil && *p.Media.FilePath != "" {
				found = append(found, where+"["+string(rune('0'+i))+"].FilePath")
			}
		}
	}
	check("Parts", d.Parts)
	if d.ToolResult != nil {
		check("ToolResult.Parts", d.ToolResult.Parts)
	}
	return found
}

// TestMessageBroadcastStage_NeverPutsBinaryOnTheBus pins the invariant the whole
// two-route split exists for: the bus is for observability and must not carry
// payloads, whether or not a recording stage is configured.
//
// RecordingStage is the opt-in route that deals with binary, and it writes
// straight to an EventStore — it never publishes. So a bus subscriber must see
// metadata only, ALWAYS: MIME type, dimensions, size, URL references, never the
// bytes. Sibling of the #853 audio guard above.
func TestMessageBroadcastStage_NeverPutsBinaryOnTheBus(t *testing.T) {
	msg := binaryBearingMessage()
	got := runBroadcast(t, []StreamElement{NewMessageElement(&msg)}, 1)

	require.Len(t, got, 1)
	assert.Emptyf(t, inlineBinaryIn(got[0]),
		"inline binary reached a bus subscriber at %v — the bus must carry metadata only",
		inlineBinaryIn(got[0]))

	// Metadata survives, or the event is useless to a live consumer.
	require.Len(t, got[0].Parts, 1)
	require.NotNil(t, got[0].Parts[0].Media)
	assert.Equal(t, "image/png", got[0].Parts[0].Media.MIMEType)
	require.NotNil(t, got[0].ToolResult)
	require.Len(t, got[0].ToolResult.Parts, 1)
	assert.Equal(t, "audio/pcm", got[0].ToolResult.Parts[0].Media.MIMEType)

	// And the caller's own message is untouched — the bus copy is a copy.
	require.NotNil(t, msg.Parts[0].Media.Data)
}

// TestRecordingStage_KeepsBinaryOffTheBusButInTheStore is the other half of the
// same invariant, from the recording side.
//
// With a recording stage wired, the binary goes to the STORE and still not to
// the bus — the two routes are not two copies of the same thing.
func TestRecordingStage_KeepsBinaryOffTheBusButInTheStore(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	var busEvents int32
	var busBinary []string
	var mu sync.Mutex
	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		defer mu.Unlock()
		atomic.AddInt32(&busEvents, 1)
		if d, ok := e.Data.(*events.MessageCreatedData); ok {
			busBinary = append(busBinary, inlineBinaryIn(d)...)
		}
	})

	store := &fakeEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionOutput})

	msg := binaryBearingMessage()
	in := make(chan StreamElement, 2)
	in <- NewMessageElement(&msg)
	close(in)
	out := make(chan StreamElement, 4)
	require.NoError(t, rs.Process(context.Background(), in, out))
	for range out { //nolint:revive // draining
	}

	time.Sleep(150 * time.Millisecond) // let any stray publish land

	mu.Lock()
	defer mu.Unlock()
	assert.Zero(t, atomic.LoadInt32(&busEvents),
		"RecordingStage must never publish — it writes straight to the EventStore")
	assert.Empty(t, busBinary)

	recorded := store.filterByType(events.EventMessageCreated)
	require.Len(t, recorded, 1)
	data, ok := recorded[0].Data.(*events.MessageCreatedData)
	require.True(t, ok)
	require.NotEmpty(t, inlineBinaryIn(data),
		"the STORE is where binary belongs — recording is lossless by design")
}
