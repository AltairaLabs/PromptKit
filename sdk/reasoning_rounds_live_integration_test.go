//go:build integration

package sdk_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// This test answers the question the stage-level live test does not: does any
// of this reach a consumer wired the way a real one is — sdk.Open with
// WithEventStore and WithRecording — rather than a stage driven directly?
//
// That distinction matters because the two signals travel different routes.
// RecordingStage writes message.created STRAIGHT to the EventStore, bypassing
// the bus. Emitter events (tool.call.*, provider.call.*) go to the BUS, and
// reach a store only because initEventBus subscribes store.OnEvent to it. A
// consumer attached to one route does not automatically see the other, and
// FileEventStore.OnEvent silently drops anything with an empty SessionID.
//
// Run:
//
//	ANTHROPIC_API_KEY=... go test -tags integration ./sdk/ \
//	    -run TestLive_SDKConsumer_ReceivesReasoningAndRounds -v

const livePackJSON = `{
	"id": "live-reasoning",
	"version": "1.0.0",
	"description": "Live reasoning + round attribution",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You have tools for temperature and humidity. Use them, then summarize.",
			"tools": ["get_temperature", "get_humidity"]
		}
	},
	"tools": {
		"get_temperature": {
			"name": "get_temperature",
			"description": "Get the current temperature for a city",
			"parameters": {
				"type": "object",
				"properties": {"city": {"type": "string"}},
				"required": ["city"]
			}
		},
		"get_humidity": {
			"name": "get_humidity",
			"description": "Get the current humidity for a city",
			"parameters": {
				"type": "object",
				"properties": {"city": {"type": "string"}},
				"required": ["city"]
			}
		}
	}
}`

// consumerStore stands in for a real consumer's session store. It implements
// EventStore, so it receives BOTH the recording stage's direct Appends and the
// bus events initEventBus subscribes it to — exactly the surface a recorder has.
type consumerStore struct {
	mu  sync.Mutex
	evs []*events.Event
}

func (s *consumerStore) Append(_ context.Context, e *events.Event) error {
	s.mu.Lock()
	s.evs = append(s.evs, e)
	s.mu.Unlock()
	return nil
}

// OnEvent is the bus-subscriber entry point. Deliberately NOT filtering on
// SessionID here — the point is to observe what actually arrives.
func (s *consumerStore) OnEvent(e *events.Event) { _ = s.Append(context.Background(), e) }

func (s *consumerStore) Query(_ context.Context, _ *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}
func (s *consumerStore) QueryRaw(_ context.Context, _ *events.EventFilter) ([]*events.StoredEvent, error) {
	return nil, nil
}
func (s *consumerStore) Stream(_ context.Context, _ string) (<-chan *events.Event, error) {
	return nil, nil
}
func (s *consumerStore) Close() error { return nil }

func (s *consumerStore) byType(t events.EventType) []*events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*events.Event
	for _, e := range s.evs {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func TestLive_SDKConsumer_ReceivesReasoningAndRounds(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		key = os.Getenv("CLAUDE_API_KEY")
	}
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	model := os.Getenv("CLAUDE_THINKING_MODEL")
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	// Build the provider through the spec path so thinking_budget is applied.
	provider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID:               "claude-live",
		Type:             "claude",
		Model:            model,
		BaseURL:          "https://api.anthropic.com/v1",
		Defaults:         providers.ProviderDefaults{MaxTokens: 4096},
		AdditionalConfig: map[string]interface{}{"thinking_budget": 2048},
	})
	require.NoError(t, err)

	dir := t.TempDir()
	packPath := dir + "/live.pack.json"
	require.NoError(t, os.WriteFile(packPath, []byte(livePackJSON), 0o644))

	store := &consumerStore{}
	bus := events.NewEventBus()

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithEventBus(bus),
		sdk.WithEventStore(store),
		sdk.WithRecording(nil),
	)
	require.NoError(t, err)
	defer func() { _ = conv.Close() }()

	conv.OnTool("get_temperature", func(map[string]any) (any, error) {
		return map[string]any{"celsius": 21}, nil
	})
	conv.OnTool("get_humidity", func(map[string]any) (any, error) {
		return map[string]any{"percent": 64}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	resp, err := conv.Send(ctx, "What's the weather in Bristol? Check temperature and humidity first.")
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Let the bus worker pool drain.
	time.Sleep(750 * time.Millisecond)

	// A model with no tools on the wire role-plays calling them in prose
	// instead of emitting tool_use blocks, which would silently reduce this to
	// a single-round turn and stop it discriminating anything. Assert the loop
	// really was multi-round below rather than trusting the setup.

	created := store.byType(events.EventMessageCreated)
	toolStarted := store.byType(events.EventToolCallStarted)
	provStarted := store.byType(events.EventProviderCallStarted)
	provDone := store.byType(events.EventProviderCallCompleted)

	t.Logf("consumer received: %d message.created, %d tool.call.started, "+
		"%d provider.call.started, %d provider.call.completed",
		len(created), len(toolStarted), len(provStarted), len(provDone))

	// --- The signals must arrive at all. This is the part the stage-level test
	// cannot prove, because it subscribes to the bus directly.
	require.NotEmpty(t, created, "consumer received no message.created")
	require.NotEmpty(t, provStarted,
		"consumer received NO provider.call.started — the runtime emits it, so a store "+
			"seeing none is a delivery problem between bus and store")
	require.NotEmpty(t, toolStarted, "consumer received no tool.call.started")

	// --- #1840: round + correlation ID, as seen by the consumer.
	startByID := map[string]int{}
	for _, e := range provStarted {
		d, ok := e.Data.(*events.ProviderCallStartedData)
		require.True(t, ok, "unexpected payload %T", e.Data)
		assert.NotZero(t, d.Round, "provider.call.started stamped round 0")
		require.NotEmpty(t, d.CallID, "provider.call.started carries no call ID")
		startByID[d.CallID] = d.Round
	}
	for _, e := range toolStarted {
		d, ok := e.Data.(*events.ToolCallEventData)
		require.True(t, ok, "unexpected payload %T", e.Data)
		assert.NotZerof(t, d.Round, "tool %q stamped round 0 at the consumer", d.ToolName)
		require.NotEmptyf(t, d.ProviderCallID, "tool %q carries no provider call ID", d.ToolName)
		round, found := startByID[d.ProviderCallID]
		assert.Truef(t, found, "tool %q references an unknown provider call", d.ToolName)
		assert.Equalf(t, round, d.Round, "tool %q disagrees with its provider call's round", d.ToolName)
	}

	// --- #1839: reasoning, as seen by the consumer.
	var traces, lastAssistantIdx int
	for i, e := range created {
		d, ok := e.Data.(*events.MessageCreatedData)
		if !ok {
			continue
		}
		if d.Role == "assistant" {
			lastAssistantIdx = i
		}
		if d.Reasoning != nil && d.Reasoning.Text != "" {
			traces++
			t.Logf("message[%d] role=%s tool_calls=%d reasoning=%d chars",
				i, d.Role, len(d.ToolCalls), len(d.Reasoning.Text))
			assert.NotContains(t, d.Content, d.Reasoning.Text,
				"reasoning leaked into conversational content")
		}
	}

	require.Positive(t, traces,
		"consumer received NO reasoning on any message.created — the trace is not "+
			"surviving the route to the store")

	// The terminal SDK response is the pre-existing path. Report what it offers
	// against what the event route delivered, so the gap is visible either way.
	terminal := resp.Message() != nil && resp.Message().Reasoning != nil
	t.Logf("reasoning traces at consumer: %d | terminal response carries reasoning: %v (last assistant idx %d)",
		traces, terminal, lastAssistantIdx)
}

// TestLive_NonRecordingConsumer_GetsCorrelatedReasoning is the test that
// matters for a consumer that does NOT enable recording — the normal wiring.
//
// RecordingStage is opt-in (WithRecording plus an EventStore). Without it there
// is no message.created at all, so the reasoning trace carried there reaches
// nobody, and the consumer is left re-accumulating reasoning.delta fragments
// and inventing a turn boundary — the exact problem #1839 was filed about.
//
// reasoning.completed closes that gap, and carries the same ProviderCallID as
// the round's tool calls so the two can be joined.
//
// Run:
//
//	ANTHROPIC_API_KEY=... go test -tags integration ./sdk/ \
//	    -run TestLive_NonRecordingConsumer_GetsCorrelatedReasoning -v
func TestLive_NonRecordingConsumer_GetsCorrelatedReasoning(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		key = os.Getenv("CLAUDE_API_KEY")
	}
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	model := os.Getenv("CLAUDE_THINKING_MODEL")
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	provider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID:               "claude-live",
		Type:             "claude",
		Model:            model,
		BaseURL:          "https://api.anthropic.com/v1",
		Defaults:         providers.ProviderDefaults{MaxTokens: 4096},
		AdditionalConfig: map[string]interface{}{"thinking_budget": 2048},
	})
	require.NoError(t, err)

	dir := t.TempDir()
	packPath := dir + "/live.pack.json"
	require.NoError(t, os.WriteFile(packPath, []byte(livePackJSON), 0o644))

	store := &consumerStore{}
	bus := events.NewEventBus()

	// Deliberately NO WithRecording — this is the wiring that previously
	// received no reasoning at all.
	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithEventBus(bus),
		sdk.WithEventStore(store),
	)
	require.NoError(t, err)
	defer func() { _ = conv.Close() }()

	conv.OnTool("get_temperature", func(map[string]any) (any, error) {
		return map[string]any{"celsius": 21}, nil
	})
	conv.OnTool("get_humidity", func(map[string]any) (any, error) {
		return map[string]any{"percent": 64}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	_, err = conv.Send(ctx, "What's the weather in Bristol? Check temperature and humidity first.")
	require.NoError(t, err)
	time.Sleep(750 * time.Millisecond)

	created := store.byType(events.EventMessageCreated)
	completed := store.byType(events.EventReasoningCompleted)
	deltas := store.byType(events.EventReasoningDelta)
	toolStarted := store.byType(events.EventToolCallStarted)

	t.Logf("non-recording consumer: %d message.created, %d reasoning.completed, "+
		"%d reasoning.delta, %d tool.call.started",
		len(created), len(completed), len(deltas), len(toolStarted))

	// Precondition: this really is the no-recording case. If message.created
	// showed up, recording got wired somehow and the test proves nothing.
	require.Empty(t, created,
		"expected NO message.created without WithRecording; recording appears to be enabled")

	require.NotEmpty(t, completed,
		"a non-recording consumer received NO reasoning.completed — the assembled trace "+
			"still is not reaching consumers that do not enable recording")
	require.NotEmpty(t, toolStarted, "model made no tool calls; nothing to correlate")

	// Correlation: reasoning must be reachable by the same key as the tool calls.
	reasoningByCall := map[string]string{}
	for _, e := range completed {
		d, ok := e.Data.(*events.ReasoningCompletedData)
		require.True(t, ok, "unexpected payload %T", e.Data)
		require.NotNil(t, d.Trace)
		assert.NotEmpty(t, d.Trace.Text, "emitted an empty reasoning trace")
		assert.NotZero(t, d.Round, "reasoning.completed stamped round 0")
		require.NotEmpty(t, d.ProviderCallID, "reasoning.completed carries no provider call ID")
		reasoningByCall[d.ProviderCallID] = d.Trace.Text
		t.Logf("round %d reasoning (%d chars) call=%s", d.Round, len(d.Trace.Text), d.ProviderCallID)
	}

	joined := 0
	for _, e := range toolStarted {
		d, ok := e.Data.(*events.ToolCallEventData)
		require.True(t, ok, "unexpected payload %T", e.Data)
		if r, found := reasoningByCall[d.ProviderCallID]; found {
			joined++
			assert.NotEmptyf(t, r, "tool %q joined to an empty trace", d.ToolName)
			t.Logf("tool %q (round %d) joins reasoning via %s", d.ToolName, d.Round, d.ProviderCallID)
		}
	}
	assert.Positive(t, joined,
		"no tool call could be joined to the reasoning that produced it — correlation is broken")

	// Deltas must agree with the assembled trace on which round they describe.
	for _, e := range deltas {
		d, ok := e.Data.(*events.ReasoningDeltaData)
		require.True(t, ok, "unexpected payload %T", e.Data)
		assert.NotZero(t, d.Round, "reasoning.delta stamped round 0")
		assert.NotEmpty(t, d.ProviderCallID, "reasoning.delta carries no provider call ID")
	}
}
