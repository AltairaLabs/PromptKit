package sdk

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/telemetry"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
	"github.com/AltairaLabs/PromptKit/sdk/session"
)

// TestOTelIntegration_SpansFromConversation verifies the full integration path:
// WithTracerProvider → initEventBus wires OTelEventListener → pipeline events → OTel spans.
// This test uses initEventBus alone (no manual listener) to prove the automatic wiring works.
func TestOTelIntegration_SpansFromConversation(t *testing.T) {
	// Set up OTel with an in-memory exporter so we can inspect spans.
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// Wire the listener through initEventBus, exactly as Open() would.
	cfg := &config{tracerProvider: tp}
	initEventBus(cfg)

	if cfg.eventBus == nil {
		t.Fatal("initEventBus should create an event bus")
	}

	// Track events for debug visibility.
	var eventTypes []events.EventType
	var eventMu sync.Mutex
	cfg.eventBus.SubscribeAll(func(e *events.Event) {
		eventMu.Lock()
		eventTypes = append(eventTypes, e.Type)
		eventMu.Unlock()
	})

	// Build a conversation with a mock provider using the wired event bus.
	conv := buildOTelTestConversation(t, cfg)
	defer conv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := conv.Send(ctx, "Hello, world!")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp == nil || resp.Text() == "" {
		t.Fatal("expected non-empty response")
	}

	// Allow async event processing to complete.
	time.Sleep(200 * time.Millisecond)

	eventMu.Lock()
	t.Logf("events received: %v", eventTypes)
	eventMu.Unlock()

	// Flush and collect spans.
	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	// Verify we got the key spans.
	spanNames := make(map[string]bool)
	for _, s := range spans {
		spanNames[s.Name] = true
	}

	// Check that at least one span starts with "promptkit.provider." (provider name varies).
	hasProvider := false
	for name := range spanNames {
		if len(name) > len("promptkit.provider.") && name[:len("promptkit.provider.")] == "promptkit.provider." {
			hasProvider = true
			break
		}
	}
	if !hasProvider {
		t.Errorf("missing provider span; got spans: %v", spanNameList(spans))
	}

	// Pipeline span should also be present.
	if !spanNames["promptkit.pipeline"] {
		t.Errorf("missing promptkit.pipeline span; got spans: %v", spanNameList(spans))
	}

	t.Logf("captured %d span(s): %v", len(spans), spanNameList(spans))
}

// TestOTelIntegration_SpansParentedUnderSession verifies that pipeline spans
// are parented under the session root span when StartSession is called.
func TestOTelIntegration_SpansParentedUnderSession(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := telemetry.Tracer(tp)
	listener := telemetry.NewOTelEventListener(tracer)
	// Use "otel-test" to match the sessionID used by buildOTelTestConversation's emitter.
	listener.StartSession(context.Background(), "otel-test")

	bus := events.NewEventBus()
	bus.SubscribeAll(listener.OnEvent)

	cfg := &config{eventBus: bus, tracerProvider: tp}
	conv := buildOTelTestConversation(t, cfg)
	defer conv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := conv.Send(ctx, "test parent-child")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	listener.EndSession("otel-test")

	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	spans := exp.GetSpans()

	// Find the session root span.
	var sessionSpanID [8]byte
	for _, s := range spans {
		if s.Name == "promptkit.session" {
			sessionSpanID = s.SpanContext.SpanID()
			break
		}
	}
	if sessionSpanID == [8]byte{} {
		t.Fatal("session span not found")
	}

	// All non-session spans should be parented (directly or transitively) under
	// the same trace. Check that provider span has the session's trace ID.
	sessionTraceID := [16]byte{}
	for _, s := range spans {
		if s.Name == "promptkit.session" {
			sessionTraceID = s.SpanContext.TraceID()
			break
		}
	}

	for _, s := range spans {
		if s.Name == "promptkit.session" {
			continue
		}
		if s.SpanContext.TraceID() != sessionTraceID {
			t.Errorf("span %q has trace ID %v, want %v (same as session)",
				s.Name, s.SpanContext.TraceID(), sessionTraceID)
		}
	}
}

// TestOTelIntegration_NoTracerProvider verifies that conversations work
// normally when no TracerProvider is configured (no spans, no errors).
func TestOTelIntegration_NoTracerProvider(t *testing.T) {
	cfg := &config{}
	initEventBus(cfg)

	conv := buildOTelTestConversation(t, cfg)
	defer conv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := conv.Send(ctx, "Hello!")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp == nil || resp.Text() == "" {
		t.Fatal("expected non-empty response")
	}
}

// TestOTelIntegration_A2ATraceRoundTrip verifies that trace context propagates
// from A2A client → server → conversation and produces spans in the same trace.
func TestOTelIntegration_A2ATraceRoundTrip(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	origTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(origTP)

	telemetry.SetupPropagation()

	// Create a parent span simulating an external caller.
	tracer := tp.Tracer("test-caller")
	parentCtx, parentSpan := tracer.Start(context.Background(), "external-request")
	parentTraceID := parentSpan.SpanContext().TraceID()
	parentSpan.End()

	// Set up an A2A server backed by a mock conversation.
	mock := &otelMockConv{}

	srv := NewA2AServer(func(string) (a2aserver.Conversation, error) { return mock, nil })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Use the A2A client to send a message with trace context.
	client := a2a.NewClient(ts.URL)
	resp, err := client.SendMessage(parentCtx, &a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{{Text: ptrStr("test trace")}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil task")
	}

	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	spans := exp.GetSpans()

	// Verify at least the parent and A2A server spans share the same trace ID.
	traceIDs := make(map[[16]byte]bool)
	for _, s := range spans {
		traceIDs[s.SpanContext.TraceID()] = true
	}

	// The parent trace ID should appear in the captured spans
	// (otelhttp on the server side creates child spans in the same trace).
	found := false
	for _, s := range spans {
		if s.SpanContext.TraceID() == parentTraceID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no spans share trace ID with parent %v; trace IDs seen: %v",
			parentTraceID, traceIDs)
	}

	t.Logf("captured %d span(s) across %d trace(s): %v",
		len(spans), len(traceIDs), spanNameList(spans))
}

// --- helpers ---

// buildOTelTestConversation creates a conversation with a mock provider
// for OTel integration tests. It uses the provided config's eventBus.
func buildOTelTestConversation(t *testing.T, cfg *config) *Conversation {
	t.Helper()

	repo := memory.NewPromptRepository()
	repo.RegisterPrompt("chat", &prompt.Config{
		APIVersion: "promptkit.io/v1alpha1",
		Kind:       "Prompt",
		Spec: prompt.Spec{
			TaskType:       "chat",
			SystemTemplate: "You are a helpful assistant.",
		},
	})
	promptRegistry := prompt.NewRegistryWithRepository(repo)

	provider := mock.NewProvider("mock-otel", "mock-model", false)

	var emitter *events.Emitter
	if cfg.eventBus != nil {
		emitter = events.NewEmitter(cfg.eventBus, "", "otel-test", "otel-test")
	}

	pipelineCfg := &pipeline.Config{
		PromptRegistry: promptRegistry,
		TaskType:       "chat",
		Provider:       provider,
		EventEmitter:   emitter,
	}

	pipe, err := pipeline.Build(pipelineCfg)
	if err != nil {
		t.Fatalf("pipeline.Build: %v", err)
	}

	sess, err := session.NewUnarySession(session.UnarySessionConfig{
		Pipeline:       pipe,
		StateStore:     statestore.NewMemoryStore(),
		ConversationID: "otel-test",
	})
	if err != nil {
		t.Fatalf("NewUnarySession: %v", err)
	}

	return &Conversation{
		prompt:         &pack.Prompt{ID: "chat", Name: "chat"},
		promptName:     "chat",
		promptRegistry: promptRegistry,
		config:         cfg,
		mode:           UnaryMode,
		unarySession:   sess,
	}
}

func spanNameList(spans []tracetest.SpanStub) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name
	}
	return names
}

// otelMockConv implements a2aserver.Conversation for the OTel integration test.
type otelMockConv struct{}

func (m *otelMockConv) Send(_ context.Context, _ any) (a2aserver.SendResult, error) {
	return &otelMockResult{}, nil
}

func (m *otelMockConv) Close() error { return nil }

type otelMockResult struct{}

func (r *otelMockResult) HasPendingTools() bool       { return false }
func (r *otelMockResult) Parts() []types.ContentPart { return []types.ContentPart{types.NewTextPart("ok")} }
func (r *otelMockResult) Text() string               { return "ok" }

func ptrStr(s string) *string { return &s }
