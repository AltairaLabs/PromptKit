// Package main shows what conversation content reaches observability
// consumers, and the two controls over it.
//
// Both matter because tool arguments are composed by the MODEL and carry
// whatever your tools take — order ids, email addresses, and under
// on-behalf-of token exchange, live delegated OAuth credentials. Anything
// subscribed to the event bus sees them, and a trace exported to a third-party
// APM takes them across a trust boundary.
//
// The two controls are different things and it is worth knowing which you want:
//
//	WithTelemetryContentCapture(bool)  the GATE.      Off by default: content
//	                                                  never reaches spans at all.
//	WithEventRedactor(policy)          the POLICY.    Rewrites content for every
//	                                                  bus consumer that does see it.
//
// You cannot do either from a tool handler. Three of the four content
// attributes are produced by the model rather than your code, and the fourth —
// the tool result — is the same value the MODEL consumes, so redacting it in a
// handler withholds it from the model rather than from the trace.
//
// Run it:
//
//	go run .
//
// It runs the same turn three ways and prints what an observability consumer
// received each time. Everything is mocked: no API keys, no network.
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// A credential-shaped value the model passes as a tool argument. Under
// on-behalf-of token exchange this is exactly what tool arguments carry.
const accessToken = "ya29.a0AfB_bYc-LIVE-DELEGATED-ACCESS-TOKEN"

const customerEmail = "ada@example.com"

// scriptedModel drives the turn without needing a scenario id.
//
// The file-backed mock repository keys its turns by scenario, which arena
// supplies and the SDK does not, so a YAML config here would silently fall
// through to its default response and the example would demonstrate nothing.
// Implementing the two-method interface directly keeps the script visible in
// one place.
type scriptedModel struct{ turn int }

func (m *scriptedModel) GetResponse(context.Context, mock.ResponseParams) (string, error) {
	return "Order A-1042 has shipped.", nil
}

func (m *scriptedModel) GetTurn(context.Context, mock.ResponseParams) (*mock.Turn, error) {
	m.turn++
	if m.turn == 1 {
		return &mock.Turn{
			Type:    "tool_calls",
			Content: "Let me look that up.",
			ToolCalls: []mock.ToolCall{{
				Name: "fetch_order",
				Arguments: map[string]interface{}{
					"order_id":       "A-1042",
					"customer_email": customerEmail,
					"access_token":   accessToken,
				},
			}},
		}, nil
	}
	return &mock.Turn{Type: "text", Content: "Order A-1042 has shipped."}, nil
}

// auditStore stands in for a consumer-supplied EventStore — an audit log, a
// SIEM feed, anything subscribed to the bus that is not the tracer.
//
// It is here to make a point the trace alone would hide: the redactor is
// applied per SUBSCRIBER at the bus, so it covers this too. A control that only
// scrubbed spans would leave this store holding the raw token.
type auditStore struct {
	mu   sync.Mutex
	seen []string
}

func (s *auditStore) OnEvent(e *events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch d := e.Data.(type) {
	case *events.ToolCallEventData:
		if len(d.Args) > 0 {
			s.seen = append(s.seen, fmt.Sprint(d.Args))
		}
	case *events.MessageCreatedData:
		for _, c := range d.ToolCalls {
			s.seen = append(s.seen, c.Args)
		}
	}
}

func (s *auditStore) Append(_ context.Context, e *events.Event) error { s.OnEvent(e); return nil }
func (s *auditStore) Query(context.Context, *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}
func (s *auditStore) QueryRaw(context.Context, *events.EventFilter) ([]*events.StoredEvent, error) {
	return nil, nil
}
func (s *auditStore) Stream(context.Context, string) (<-chan *events.Event, error) {
	return nil, nil
}
func (s *auditStore) Close() error { return nil }

func (s *auditStore) contents() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.seen, " ")
}

// runTurn drives one tool-calling turn and reports what each consumer received.
func runTurn(label string, opts ...sdk.Option) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	store := &auditStore{}

	// A scripted model: round 1 calls the tool with a credential and an email in
	// its arguments, round 2 answers. This is the shape a real model produces,
	// and the reason the example exists.
	provider := mock.NewToolProviderWithRepository(
		"mock", "mock-model", false, &scriptedModel{})

	base := []sdk.Option{
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithTracerProvider(tp),
		sdk.WithEventStore(store),
	}

	conv, err := sdk.Open("./telemetry.pack.json", "chat", append(base, opts...)...)
	if err != nil {
		fmt.Printf("%-28s open failed: %v\n", label, err)
		return
	}
	defer func() { _ = conv.Close() }()

	conv.OnTool("fetch_order", func(map[string]any) (any, error) {
		return map[string]any{"status": "shipped"}, nil
	})

	if _, err := conv.Send(context.Background(), "Where is my order?"); err != nil {
		fmt.Printf("%-28s send failed: %v\n", label, err)
		return
	}
	time.Sleep(300 * time.Millisecond) // the bus dispatches asynchronously

	_ = tp.ForceFlush(context.Background())
	report(label, spanText(exp), store.contents())
}

// spanText flattens every attribute on every span and span event.
func spanText(exp *tracetest.InMemoryExporter) string {
	var b strings.Builder
	for _, s := range exp.GetSpans() {
		for _, a := range s.Attributes {
			b.WriteString(a.Value.Emit())
			b.WriteString(" ")
		}
		for _, e := range s.Events {
			for _, a := range e.Attributes {
				b.WriteString(a.Value.Emit())
				b.WriteString(" ")
			}
		}
	}
	return b.String()
}

func report(label, traced, stored string) {
	verdict := func(haystack string) string {
		switch {
		case strings.Contains(haystack, accessToken):
			return "LEAKED the access token"
		case strings.Contains(haystack, "[REDACTED]"):
			return "redacted"
		case strings.Contains(haystack, customerEmail):
			return "no token, but the email is present"
		default:
			return "no content"
		}
	}
	fmt.Printf("%-28s trace: %-32s audit store: %s\n", label, verdict(traced), verdict(stored))
}

func main() {
	fmt.Println("One tool-calling turn, three configurations.")
	fmt.Println("The model passes an access token and an email as tool arguments.")
	fmt.Println()

	// 1. The default. Nothing configured beyond tracing itself.
	runTurn("default")

	// 2. Content capture on, no policy. The deliberate choice to record
	//    payloads — appropriate when the backend is a fitting place for them.
	runTurn("capture on", sdk.WithTelemetryContentCapture(true))

	// 3. Capture on WITH a policy. The redactor runs at the bus, so it covers
	//    the audit store as well as the tracer.
	runTurn("capture on + redactor",
		sdk.WithTelemetryContentCapture(true),
		sdk.WithEventRedactor(func(field, value string) string {
			if field == events.FieldToolCallArgs {
				return strings.ReplaceAll(value, accessToken, "[REDACTED]")
			}
			return value
		}),
	)

	fmt.Println()
	fmt.Println("Read row 1 carefully. The content gate is a TRACING control: it")
	fmt.Println("keeps payloads off spans and does nothing for any other consumer.")
	fmt.Println("The audit store — an EventStore this example wires deliberately —")
	fmt.Println("still receives the raw token. That is correct: a store is a sink")
	fmt.Println("you chose, and redacting it by default would break audit and")
	fmt.Println("replay. It is also the reason the gate alone is not a data-")
	fmt.Println("protection story.")
	fmt.Println()
	fmt.Println("Row 3 is: WithEventRedactor runs at the BUS, so one policy covers")
	fmt.Println("every subscriber — tracer, store and metrics alike. WithRecording")
	fmt.Println("stays unaffected by design: RecordingStage appends straight to its")
	fmt.Println("store with no bus hop, so recordings keep full fidelity.")
}
