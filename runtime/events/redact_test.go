package events

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const redactSecret = "ya29.LIVE-DELEGATED-TOKEN"

func scrub(_ string, v string) string {
	return strings.ReplaceAll(v, redactSecret, "[REDACTED]")
}

func textPart(s string) types.ContentPart {
	return types.ContentPart{Type: types.ContentTypeText, Text: &s}
}

func messageEvent() *Event {
	return &Event{
		Type: EventMessageCreated,
		Data: &MessageCreatedData{
			Role:      "assistant",
			Content:   "token is " + redactSecret,
			Parts:     []types.ContentPart{textPart("part holds " + redactSecret)},
			ToolCalls: []MessageToolCall{{ID: "c1", Name: "fetch", Args: `{"t":"` + redactSecret + `"}`}},
			ToolResult: &MessageToolResult{
				ID: "c1", Name: "fetch",
				Parts: []types.ContentPart{textPart("result " + redactSecret)},
				Error: "failed for " + redactSecret,
			},
		},
	}
}

// TestRedacting_DoesNotMutateTheSharedEvent is the correctness property the
// whole design rests on.
//
// A bus fans ONE event out to every subscriber. If redaction rewrote in place,
// wrapping a trace listener would also strip the recording store that is
// supposed to keep the original — silently, and only for whichever subscriber
// happened to run second.
func TestRedacting_DoesNotMutateTheSharedEvent(t *testing.T) {
	evt := messageEvent()

	var redacted *Event
	Redacting(func(e *Event) { redacted = e }, scrub)(evt)

	original, ok := evt.Data.(*MessageCreatedData)
	require.True(t, ok)
	assert.Contains(t, original.Content, redactSecret,
		"the shared event was rewritten in place; every other subscriber now sees redacted data")
	assert.Contains(t, original.ToolCalls[0].Args, redactSecret)
	assert.Contains(t, *original.Parts[0].Text, redactSecret)
	assert.Contains(t, original.ToolResult.Error, redactSecret)

	got, ok := redacted.Data.(*MessageCreatedData)
	require.True(t, ok)
	assert.NotContains(t, got.Content, redactSecret, "the wrapped subscriber got unredacted content")
}

// TestRedacting_CoversEveryContentFieldOnAMessage guards against a field being
// added to the payload and quietly escaping redaction.
func TestRedacting_CoversEveryContentFieldOnAMessage(t *testing.T) {
	var redacted *Event
	Redacting(func(e *Event) { redacted = e }, scrub)(messageEvent())

	d, ok := redacted.Data.(*MessageCreatedData)
	require.True(t, ok)

	assert.NotContains(t, d.Content, redactSecret, "Content")
	assert.NotContains(t, *d.Parts[0].Text, redactSecret, "Parts")
	assert.NotContains(t, d.ToolCalls[0].Args, redactSecret, "ToolCalls.Args")
	assert.NotContains(t, *d.ToolResult.Parts[0].Text, redactSecret, "ToolResult.Parts")
	assert.NotContains(t, d.ToolResult.Error, redactSecret, "ToolResult.Error")

	// Identity survives: redaction scrubs payload, not routing.
	assert.Equal(t, "assistant", d.Role)
	assert.Equal(t, "fetch", d.ToolCalls[0].Name)
	assert.Equal(t, "c1", d.ToolResult.ID)
}

// TestRedacting_ToolCallArgs covers the payload that carries credentials under
// on-behalf-of token exchange.
func TestRedacting_ToolCallArgs(t *testing.T) {
	evt := &Event{
		Type: EventToolCallStarted,
		Data: &ToolCallEventData{
			ToolName: "fetch_orders", CallID: "c1",
			Args: map[string]interface{}{"token": redactSecret, "limit": 10},
		},
	}

	var redacted *Event
	Redacting(func(e *Event) { redacted = e }, scrub)(evt)

	d, ok := redacted.Data.(*ToolCallEventData)
	require.True(t, ok)
	assert.Equal(t, "[REDACTED]", d.Args["token"])
	assert.Equal(t, 10, d.Args["limit"], "non-string values pass through untouched")
	assert.Equal(t, "fetch_orders", d.ToolName, "the tool NAME is not payload")

	orig, ok := evt.Data.(*ToolCallEventData)
	require.True(t, ok)
	assert.Equal(t, redactSecret, orig.Args["token"], "the source event's args map was mutated")
}

// TestRedacting_FieldNamesLetAPolicyDiscriminate proves the field argument is
// usable, not decorative — a policy can scrub arguments while keeping prose.
func TestRedacting_FieldNamesLetAPolicyDiscriminate(t *testing.T) {
	argsOnly := func(field, v string) string {
		if field == FieldToolCallArgs {
			return "[ARGS REDACTED]"
		}
		return v
	}

	var redacted *Event
	Redacting(func(e *Event) { redacted = e }, argsOnly)(messageEvent())

	d, ok := redacted.Data.(*MessageCreatedData)
	require.True(t, ok)
	assert.Equal(t, "[ARGS REDACTED]", d.ToolCalls[0].Args)
	assert.Contains(t, d.Content, redactSecret,
		"a policy that only targets args must leave message content alone")
}

// TestRedacting_NilPolicyIsPassThrough keeps the wrapper free when unused: no
// copy, and the same subscriber back.
func TestRedacting_NilPolicyIsPassThrough(t *testing.T) {
	evt := messageEvent()
	var got *Event
	sub := func(e *Event) { got = e }

	Redacting(sub, nil)(evt)
	assert.Same(t, evt, got, "a nil policy must deliver the original event, uncopied")
}

// TestRedacting_LeavesNonContentEventsUncopied: most events on a busy pipeline
// are provider and tool timing. Copying them to rewrite nothing is pure cost.
func TestRedacting_LeavesNonContentEventsUncopied(t *testing.T) {
	evt := &Event{Type: EventProviderCallStarted, Data: &ProviderCallStartedData{Model: "gpt-4"}}
	var got *Event
	Redacting(func(e *Event) { got = e }, scrub)(evt)
	assert.Same(t, evt, got, "an event with no content should pass through without a copy")
}
