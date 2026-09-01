package telemetry

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// A token shaped like the real thing, so a leak is greppable in the failure
// output rather than needing a second look to recognise.
const secretToken = "ya29.a0AfB_bYc-LIVE-DELEGATED-ACCESS-TOKEN"

// driveToolTurn emits a tool call carrying a credential and a message carrying
// content, then returns every attribute value recorded on any span or span
// event.
//
// Flattened deliberately: the guarantee is "this value does not appear
// anywhere in the trace", not "it is absent from the attribute I remembered to
// check". A new content attribute added on a path that skips the gate is
// caught by this rather than needing its own assertion.
func driveToolTurn(t *testing.T, opts ...OTelOption) []string {
	t.Helper()
	listener, exp, tp := newTestListenerWith(t, opts...)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")
	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallStarted, Timestamp: now,
		SessionID: "sess-1", ExecutionID: "run-1",
		Data: &events.ProviderCallStartedData{Provider: "openai", Model: "gpt-4"},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventToolCallStarted, Timestamp: now.Add(10 * time.Millisecond),
		SessionID: "sess-1", ExecutionID: "run-1",
		Data: &events.ToolCallEventData{
			ToolName: "fetch_orders", CallID: "call-1",
			Args: map[string]any{"access_token": secretToken, "email": "user@example.com"},
		},
	})
	// The tool span is only exported once it ENDS, so the completion is required
	// — without it the arguments never reach the exporter and a leak test would
	// pass by measuring nothing.
	listener.OnEvent(&events.Event{
		Type: events.EventToolCallCompleted, Timestamp: now.Add(15 * time.Millisecond),
		SessionID: "sess-1", ExecutionID: "run-1",
		Data: &events.ToolCallEventData{
			ToolName: "fetch_orders", CallID: "call-1",
			Duration: 5 * time.Millisecond, Status: "success",
		},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventMessageCreated, Timestamp: now.Add(20 * time.Millisecond),
		SessionID: "sess-1", ExecutionID: "run-1",
		Data: &events.MessageCreatedData{
			Role:    "assistant",
			Content: "Your order for " + secretToken + " is ready",
			ToolCalls: []events.MessageToolCall{
				{ID: "call-1", Name: "fetch_orders", Args: `{"access_token":"` + secretToken + `"}`},
			},
		},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallCompleted, Timestamp: now.Add(500 * time.Millisecond),
		SessionID: "sess-1", ExecutionID: "run-1",
		Data: &events.ProviderCallCompletedData{Provider: "openai", Model: "gpt-4"},
	})
	listener.EndSession("sess-1")

	var values []string
	for _, sp := range flushAndGetSpans(t, tp, exp) {
		for _, a := range sp.Attributes {
			values = append(values, a.Value.Emit())
		}
		for _, e := range sp.Events {
			for _, a := range e.Attributes {
				values = append(values, a.Value.Emit())
			}
		}
	}
	require.NotEmpty(t, values, "no attributes recorded at all; the turn did not produce spans")
	_ = tracetest.SpanStubs{}
	return values
}

// TestContentIsOffByDefault is the guarantee the gate exists for.
//
// Tool arguments are composed by the model and carry whatever the tool takes.
// Under on-behalf-of token exchange that includes live delegated credentials,
// which would otherwise be exported verbatim to whatever APM the operator has
// configured — customer data across a trust boundary with no control surface.
func TestContentIsOffByDefault(t *testing.T) {
	for _, v := range driveToolTurn(t) {
		assert.NotContainsf(t, v, secretToken,
			"a credential reached a span with content capture OFF: %q", v)
		assert.NotContains(t, v, "user@example.com",
			"caller data reached a span with content capture off")
	}
}

// TestOperationalAttributesSurviveTheGate pins the other half: turning content
// off must not blind the trace.
//
// Span structure, tool NAMES, model and provider are what make a trace useful
// operationally, and none of them are payload. A gate that took those with it
// would just push operators to re-enable capture, defeating itself.
func TestOperationalAttributesSurviveTheGate(t *testing.T) {
	values := driveToolTurn(t)
	joined := strings.Join(values, "|")

	assert.Contains(t, joined, "fetch_orders", "the tool NAME is not payload and must survive")
	assert.Contains(t, joined, "gpt-4", "the model is not payload and must survive")
	assert.Contains(t, joined, "openai", "the provider is not payload and must survive")
}

// TestContentCaptureOptIn proves the gate is a gate and not a deletion.
func TestContentCaptureOptIn(t *testing.T) {
	joined := strings.Join(driveToolTurn(t, WithContentCapture(true)), "|")
	assert.Contains(t, joined, secretToken,
		"with capture explicitly enabled the content must be recorded; otherwise the "+
			"option does nothing and callers cannot get the payloads they asked for")
}

// Redaction is deliberately NOT tested here: it is no longer this package's
// job. events.Redacting wraps any subscriber and is covered in that package,
// so one policy serves this listener, a caller's own subscriber and any
// third-party store. What this package owns, and what the tests above pin, is
// the default-off GATE — the part a caller cannot supply from outside.
