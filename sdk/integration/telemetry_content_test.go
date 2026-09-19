package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// A credential-shaped value passed as a tool argument, so a leak is obvious in
// the failure output. Under on-behalf-of token exchange this is what tool
// arguments actually carry.
const spanSecret = "ya29.a0AfB_bYc-LIVE-DELEGATED-ACCESS-TOKEN"

// spanAttributeValues runs one tool-calling turn end to end through sdk.Open
// and returns every attribute value recorded on any span or span event.
//
// End to end on purpose. A unit test asserting that options land on the config
// passes even if initEventBus stops passing them to the listener — verified by
// deleting that argument, which still compiles and still passes. Only driving a
// real turn and reading the exporter proves the option is connected.
func spanAttributeValues(t *testing.T, extra ...sdk.Option) []string {
	t.Helper()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Looking that up",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_weather",
			Arguments: map[string]interface{}{"city": "London", "access_token": spanSecret},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{Type: "text", Content: "Sunny."})

	opts := append([]sdk.Option{sdk.WithEventBus(bus), sdk.WithTracerProvider(tp)}, extra...)
	conv := openToolConv(t, repo, map[string]func(args map[string]any) (any, error){
		"get_weather": func(map[string]any) (any, error) {
			return map[string]any{"temperature": 22}, nil
		},
	}, opts...)

	_, err := conv.Send(context.Background(), "What is the weather?")
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond) // async bus dispatch

	require.NoError(t, tp.ForceFlush(context.Background()))
	var values []string
	for _, s := range exp.GetSpans() {
		for _, a := range s.Attributes {
			values = append(values, a.Value.Emit())
		}
		for _, e := range s.Events {
			for _, a := range e.Attributes {
				values = append(values, a.Value.Emit())
			}
		}
	}
	require.NotEmpty(t, values, "no span attributes recorded; the turn produced no trace")
	return values
}

// TestTelemetry_ContentOffByDefault_EndToEnd is the guarantee, asserted through
// the surface a caller actually uses.
//
// Configuring tracing must not export customer data by accident. Tool arguments
// are composed by the model and carry whatever the tool takes — and none of
// this is reachable from a tool handler, since the handler's return value is
// what the MODEL consumes too.
func TestTelemetry_ContentOffByDefault_EndToEnd(t *testing.T) {
	for _, v := range spanAttributeValues(t) {
		assert.NotContainsf(t, v, spanSecret,
			"a tool argument reached a span with no content-capture option set: %q", v)
	}
}

// TestTelemetry_ContentCaptureOptIn_EndToEnd proves the option is connected.
//
// This is the assertion a config-level unit test cannot make: it fails if
// initEventBus stops forwarding the options to the listener, which compiles
// perfectly well.
func TestTelemetry_ContentCaptureOptIn_EndToEnd(t *testing.T) {
	joined := strings.Join(
		spanAttributeValues(t, sdk.WithTelemetryContentCapture(true)), "|")
	assert.Contains(t, joined, spanSecret,
		"content capture was enabled but no payload reached the trace; the option is "+
			"not reaching the listener")
}

// TestTelemetry_RedactorAppliesEndToEnd covers the split this design rests on:
// the caller owns the policy, the runtime owns the enforcement point.
//
// The redactor is applied at the BUS, not inside the listener, so the same
// policy covers a configured event store and the metrics collector too. This
// asserts it through traces because that is the consumer with an exporter to
// read; events.Redacting has the per-field coverage.
func TestTelemetry_RedactorAppliesEndToEnd(t *testing.T) {
	joined := strings.Join(spanAttributeValues(t,
		sdk.WithTelemetryContentCapture(true),
		sdk.WithEventRedactor(func(_, value string) string {
			return strings.ReplaceAll(value, spanSecret, "[REDACTED]")
		}),
	), "|")

	assert.NotContains(t, joined, spanSecret, "the redactor did not reach the listener")
	assert.Contains(t, joined, "[REDACTED]", "the redacted form must still be recorded")
	assert.Contains(t, joined, "London",
		"the redactor rewrote only what it matched; other captured content stays")
}

// capturingStore records every event it is handed, standing in for a
// consumer-supplied EventStore shipping somewhere.
type capturingStore struct {
	mu  sync.Mutex
	got []*events.Event
}

func (s *capturingStore) OnEvent(e *events.Event) {
	s.mu.Lock()
	s.got = append(s.got, e)
	s.mu.Unlock()
}

func (s *capturingStore) Append(_ context.Context, e *events.Event) error {
	s.OnEvent(e)
	return nil
}
func (s *capturingStore) Query(context.Context, *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}
func (s *capturingStore) QueryRaw(context.Context, *events.EventFilter) ([]*events.StoredEvent, error) {
	return nil, nil
}
func (s *capturingStore) Stream(context.Context, string) (<-chan *events.Event, error) {
	return nil, nil
}
func (s *capturingStore) Close() error { return nil }

func (s *capturingStore) dump() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for _, e := range s.got {
		if d, ok := e.Data.(*events.ToolCallEventData); ok {
			b.WriteString(fmt.Sprint(d.Args))
		}
		if d, ok := e.Data.(*events.MessageCreatedData); ok {
			b.WriteString(d.Content)
			for _, c := range d.ToolCalls {
				b.WriteString(c.Args)
			}
		}
	}
	return b.String()
}

// TestEventRedactor_AppliesToTheEventStoreToo is the reason redaction lives at
// the bus rather than inside the OTel listener.
//
// A consumer-supplied EventStore is just another subscriber, and it may ship
// anywhere. A policy that only covered traces would leave it untouched — which
// is exactly what happened before this moved out of the telemetry package.
//
// Note this is the BUS store subscription, not WithRecording: RecordingStage
// appends directly and deliberately keeps full fidelity.
func TestEventRedactor_AppliesToTheEventStoreToo(t *testing.T) {
	store := &capturingStore{}

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type: "tool_calls", Content: "Looking that up",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_weather",
			Arguments: map[string]interface{}{"city": "London", "access_token": spanSecret},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{Type: "text", Content: "Sunny."})

	conv := openToolConv(t, repo, map[string]func(args map[string]any) (any, error){
		"get_weather": func(map[string]any) (any, error) {
			return map[string]any{"temperature": 22}, nil
		},
	},
		sdk.WithEventBus(bus),
		sdk.WithTracerProvider(tp),
		sdk.WithEventStore(store),
		sdk.WithEventRedactor(func(_, value string) string {
			return strings.ReplaceAll(value, spanSecret, "[REDACTED]")
		}),
	)

	_, err := conv.Send(context.Background(), "What is the weather?")
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)

	dumped := store.dump()
	require.NotEmpty(t, dumped, "the store received no content-bearing events")
	assert.NotContains(t, dumped, spanSecret,
		"a credential reached the event store unredacted; the policy is only wired to "+
			"the trace listener, not to every bus consumer")
	assert.Contains(t, dumped, "[REDACTED]", "the redacted form must reach the store")
}
