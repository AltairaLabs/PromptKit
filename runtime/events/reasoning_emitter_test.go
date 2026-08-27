package events

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// capture subscribes to one event type and returns a snapshot func. The bus
// dispatches through a worker pool, so callers must wait rather than read
// immediately, and must never assume publish order.
func capture(bus Bus, t EventType) func() []*Event {
	var mu sync.Mutex
	var got []*Event
	bus.Subscribe(t, func(e *Event) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	})
	return func() []*Event {
		mu.Lock()
		defer mu.Unlock()
		out := make([]*Event, len(got))
		copy(out, got)
		return out
	}
}

func waitFor(t *testing.T, snap func() []*Event, want int) []*Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(snap()) >= want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := snap()
	if len(got) < want {
		t.Fatalf("timed out waiting for %d events, got %d", want, len(got))
	}
	return got
}

// settle gives the bus time to deliver something it should NOT have sent.
func settle(snap func() []*Event) []*Event {
	time.Sleep(250 * time.Millisecond)
	return snap()
}

// TestReasoningCompletedCtx_EmitsTraceWithCorrelation covers the terminal
// reasoning event's payload: the assembled trace plus the join key that ties it
// to the round's provider call, and so to the tool calls that call requested.
func TestReasoningCompletedCtx_EmitsTraceWithCorrelation(t *testing.T) {
	bus := NewEventBus()
	e := NewEmitter(bus, "run", "sess", "conv")
	snap := capture(bus, EventReasoningCompleted)

	trace := &types.ReasoningTrace{
		Text:   "I need the forecast before answering.",
		Opaque: []types.OpaqueReasoning{{Provider: "claude", Kind: "thinking_signature", Data: "sig"}},
	}
	e.ReasoningCompletedCtx(context.Background(), trace, 2, "pc-42")

	got := waitFor(t, snap, 1)
	d, ok := got[0].Data.(*ReasoningCompletedData)
	if !ok {
		t.Fatalf("unexpected payload %T", got[0].Data)
	}
	if d.Trace == nil || d.Trace.Text != trace.Text {
		t.Fatalf("trace not carried: %#v", d.Trace)
	}
	if len(d.Trace.Opaque) != 1 || d.Trace.Opaque[0].Kind != "thinking_signature" {
		t.Fatalf("opaque reasoning not carried: %#v", d.Trace.Opaque)
	}
	if d.Round != 2 {
		t.Fatalf("Round = %d, want 2", d.Round)
	}
	if d.ProviderCallID != "pc-42" {
		t.Fatalf("ProviderCallID = %q, want pc-42", d.ProviderCallID)
	}
}

// TestReasoningCompletedCtx_SilentOnEmptyTrace keeps "this round did not
// reason" distinguishable from "the trace was dropped en route". Emitting an
// empty event would erase that distinction for every consumer.
func TestReasoningCompletedCtx_SilentOnEmptyTrace(t *testing.T) {
	bus := NewEventBus()
	e := NewEmitter(bus, "run", "sess", "conv")
	snap := capture(bus, EventReasoningCompleted)

	e.ReasoningCompletedCtx(context.Background(), nil, 1, "pc-1")
	e.ReasoningCompletedCtx(context.Background(), &types.ReasoningTrace{}, 1, "pc-1")
	e.ReasoningCompletedCtx(context.Background(), &types.ReasoningTrace{Text: ""}, 1, "pc-1")

	if got := settle(snap); len(got) != 0 {
		t.Fatalf("emitted %d events for empty reasoning; want none", len(got))
	}
}

// TestReasoningCompletedCtx_TraceSerializes pins the deliberate difference from
// MessageCreatedData.Reasoning, which is json:"-".
//
// ReasoningDeltaData.Text already serializes, so the same content leaves the
// process as fragments; withholding the assembled form would hand a serializing
// consumer the pieces but not the whole. The json:"-" on the message record
// addresses a different concern — keeping reasoning out of conversational
// history — and is asserted separately.
func TestReasoningCompletedCtx_TraceSerializes(t *testing.T) {
	out, err := json.Marshal(&ReasoningCompletedData{
		Trace:          &types.ReasoningTrace{Text: "VISIBLE_REASONING"},
		Round:          1,
		ProviderCallID: "pc-1",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "VISIBLE_REASONING") {
		t.Fatalf("reasoning.completed must serialize its trace, matching reasoning.delta: %s", out)
	}
}

// TestReasoningDeltaCtx_CarriesRoundAttribution covers the streaming fragment's
// attribution, so deltas can be reconciled with the trace that follows them.
func TestReasoningDeltaCtx_CarriesRoundAttribution(t *testing.T) {
	bus := NewEventBus()
	e := NewEmitter(bus, "run", "sess", "conv")
	snap := capture(bus, EventReasoningDelta)

	e.ReasoningDeltaCtx(context.Background(), "weighing options", 3, "pc-9")

	got := waitFor(t, snap, 1)
	d, ok := got[0].Data.(*ReasoningDeltaData)
	if !ok {
		t.Fatalf("unexpected payload %T", got[0].Data)
	}
	if d.Text != "weighing options" || d.Round != 3 || d.ProviderCallID != "pc-9" {
		t.Fatalf("delta attribution wrong: %#v", d)
	}
}

// TestToolCallEventCtx_CarriesRoundAndStripsBinary covers the struct-taking
// tool emitter: it must carry Round/ProviderCallID (which the positional
// helpers cannot) while keeping the binary-stripping the bus route requires.
func TestToolCallEventCtx_CarriesRoundAndStripsBinary(t *testing.T) {
	bus := NewEventBus()
	e := NewEmitter(bus, "run", "sess", "conv")
	snap := capture(bus, EventToolCallCompleted)

	big := "AAAABBBBCCCC"
	e.ToolCallEventCtx(context.Background(), EventToolCallCompleted, &ToolCallEventData{
		ToolName:       "probe",
		CallID:         "call_1",
		Round:          4,
		ProviderCallID: "pc-4",
		Parts:          []types.ContentPart{types.NewImagePartFromData(big, "image/png", nil)},
	})

	got := waitFor(t, snap, 1)
	d, ok := got[0].Data.(*ToolCallEventData)
	if !ok {
		t.Fatalf("unexpected payload %T", got[0].Data)
	}
	if d.Round != 4 || d.ProviderCallID != "pc-4" {
		t.Fatalf("round attribution lost: %#v", d)
	}
	raw, err := json.Marshal(d.Parts)
	if err != nil {
		t.Fatalf("marshal parts: %v", err)
	}
	if strings.Contains(string(raw), big) {
		t.Fatalf("binary payload was not stripped for the observability route: %s", raw)
	}
}

// TestToolCallEventCtx_NilDataEmitsNothing guards against publishing an event
// with no payload, which would panic every type-asserting subscriber.
func TestToolCallEventCtx_NilDataEmitsNothing(t *testing.T) {
	bus := NewEventBus()
	e := NewEmitter(bus, "run", "sess", "conv")
	snap := capture(bus, EventToolCallStarted)

	e.ToolCallEventCtx(context.Background(), EventToolCallStarted, nil)

	if got := settle(snap); len(got) != 0 {
		t.Fatalf("emitted %d events for nil data; want none", len(got))
	}
}

// TestProviderCallStartedCtx_CarriesRoundAndDefaultsSource covers the additive
// struct-taking provider emitter, including the Source default the positional
// form applies.
func TestProviderCallStartedCtx_CarriesRoundAndDefaultsSource(t *testing.T) {
	bus := NewEventBus()
	e := NewEmitter(bus, "run", "sess", "conv")
	snap := capture(bus, EventProviderCallStarted)

	e.ProviderCallStartedCtx(context.Background(), &ProviderCallStartedData{
		Provider: "claude", Model: "m", MessageCount: 2, ToolCount: 1,
		Round: 5, CallID: "pc-5",
	})

	got := waitFor(t, snap, 1)
	d, ok := got[0].Data.(*ProviderCallStartedData)
	if !ok {
		t.Fatalf("unexpected payload %T", got[0].Data)
	}
	if d.Round != 5 || d.CallID != "pc-5" {
		t.Fatalf("round/callID lost: %#v", d)
	}
	if d.Source != SourceAgent {
		t.Fatalf("Source = %q, want %q", d.Source, SourceAgent)
	}
}

// TestProviderCallStartedCtx_NilDataEmitsNothing mirrors the tool-event guard.
func TestProviderCallStartedCtx_NilDataEmitsNothing(t *testing.T) {
	bus := NewEventBus()
	e := NewEmitter(bus, "run", "sess", "conv")
	snap := capture(bus, EventProviderCallStarted)

	e.ProviderCallStartedCtx(context.Background(), nil)

	if got := settle(snap); len(got) != 0 {
		t.Fatalf("emitted %d events for nil data; want none", len(got))
	}
}
