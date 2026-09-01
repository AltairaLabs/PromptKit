//go:build integration

package stage

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// This file proves #1839 and #1840 against a REAL reasoning model, over a REAL
// multi-round tool loop. Mock providers cannot prove either: a mock returns
// whatever reasoning the test tells it to, so a passing mock test only shows
// the test's own arithmetic. What has to hold is that a live thinking model
// emits reasoning on tool-calling rounds — not just on the final answer — and
// that each round's trace and round number survive to a consumer.
//
// Run:
//
//	ANTHROPIC_API_KEY=... go test -tags integration ./runtime/pipeline/stage/ \
//	    -run TestLive_ReasoningAndRounds -v
//
// Override the model with CLAUDE_THINKING_MODEL.

// liveThinkingProvider builds a real Claude provider with extended thinking on,
// through CreateProviderFromSpec so additional_config.thinking_budget is
// actually applied — the constructor path leaves it unset.
func liveThinkingProvider(t *testing.T) providers.Provider {
	t.Helper()

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

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID:      "claude-live-reasoning",
		Type:    "claude",
		Model:   model,
		BaseURL: "https://api.anthropic.com/v1",
		Defaults: providers.ProviderDefaults{
			MaxTokens: 4096,
		},
		AdditionalConfig: map[string]interface{}{
			"thinking_budget": 2048,
		},
	})
	require.NoError(t, err, "CreateProviderFromSpec")
	return p
}

// weatherExecutor answers the live model's tool calls with fixed readings, so
// the loop is driven by the model's own decisions rather than by a script.
type weatherExecutor struct{}

func (weatherExecutor) Name() string { return "local" }
func (weatherExecutor) Execute(
	_ context.Context, d *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	switch d.Name {
	case "get_temperature":
		return json.RawMessage(`{"celsius": 21}`), nil
	case "get_humidity":
		return json.RawMessage(`{"percent": 64}`), nil
	default:
		return json.RawMessage(`{}`), nil
	}
}

func liveToolRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	r := tools.NewRegistry()
	for _, n := range []string{"get_temperature", "get_humidity"} {
		require.NoError(t, r.Register(&tools.ToolDescriptor{
			Name:        n,
			Description: "Return the current " + n + " for a city.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
			Mode: "local",
		}))
	}
	r.RegisterExecutor(weatherExecutor{})
	return r
}

// liveCapture holds everything a recorder would see from one live turn.
type liveCapture struct {
	mu             sync.Mutex
	messageCreated []*events.MessageCreatedData
	toolStarted    []*events.ToolCallEventData
	providerStart  []*events.ProviderCallStartedData
	providerDone   []*events.ProviderCallCompletedData
}

func (c *liveCapture) subscribe(bus events.Bus) {
	bus.Subscribe(events.EventToolCallStarted, func(e *events.Event) {
		if d, ok := e.Data.(*events.ToolCallEventData); ok {
			c.mu.Lock()
			c.toolStarted = append(c.toolStarted, d)
			c.mu.Unlock()
		}
	})
	bus.Subscribe(events.EventProviderCallStarted, func(e *events.Event) {
		if d, ok := e.Data.(*events.ProviderCallStartedData); ok {
			c.mu.Lock()
			c.providerStart = append(c.providerStart, d)
			c.mu.Unlock()
		}
	})
	bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
		if d, ok := e.Data.(*events.ProviderCallCompletedData); ok {
			c.mu.Lock()
			c.providerDone = append(c.providerDone, d)
			c.mu.Unlock()
		}
	})
}

// liveRecordingStore captures message.created the way a real recorder does —
// as an EventStore fed by the RecordingStage.
type liveRecordingStore struct {
	mu   sync.Mutex
	data []*events.MessageCreatedData
}

func (s *liveRecordingStore) Append(_ context.Context, e *events.Event) error {
	if d, ok := e.Data.(*events.MessageCreatedData); ok {
		s.mu.Lock()
		s.data = append(s.data, d)
		s.mu.Unlock()
	}
	return nil
}
func (s *liveRecordingStore) OnEvent(*events.Event) {}
func (s *liveRecordingStore) Query(_ context.Context, _ *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}
func (s *liveRecordingStore) QueryRaw(
	_ context.Context, _ *events.EventFilter,
) ([]*events.StoredEvent, error) {
	return nil, nil
}
func (s *liveRecordingStore) Stream(_ context.Context, _ string) (<-chan *events.Event, error) {
	return nil, nil
}
func (s *liveRecordingStore) Close() error { return nil }

func (s *liveRecordingStore) snapshot() []*events.MessageCreatedData {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*events.MessageCreatedData, len(s.data))
	copy(out, s.data)
	return out
}

// TestLive_ReasoningAndRounds drives a real thinking model through a real tool
// loop and asserts what a consumer actually receives.
func TestLive_ReasoningAndRounds(t *testing.T) {
	provider := liveThinkingProvider(t)
	defer func() { _ = provider.Close() }()

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "live-run", "live-sess", "live-conv")
	cap := &liveCapture{}
	cap.subscribe(bus)

	store := &liveRecordingStore{}

	turnState := NewTurnState()
	turnState.SystemPrompt = "You have tools for temperature and humidity. " +
		"Use them one at a time, then summarize the weather."
	turnState.AllowedTools = []string{"get_temperature", "get_humidity"}

	providerStage := NewProviderStageWithTurnState(
		provider, liveToolRegistry(t), nil,
		&ProviderConfig{MaxTokens: 4096}, emitter, nil, turnState,
	)
	recStage := NewRecordingStage(store, RecordingStageConfig{
		Position:       RecordingPositionOutput,
		SessionID:      "live-sess",
		ConversationID: "live-conv",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// provider stage → recording stage, mirroring the real pipeline order.
	in := make(chan StreamElement, 1)
	mid := make(chan StreamElement, 128)
	out := make(chan StreamElement, 128)

	userMsg := types.Message{
		Role:    "user",
		Content: "What's the weather in Bristol? Check temperature and humidity before answering.",
	}
	in <- NewMessageElement(&userMsg)
	close(in)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := recStage.Process(ctx, mid, out); err != nil {
			t.Errorf("recording stage: %v", err)
		}
	}()

	require.NoError(t, providerStage.Process(ctx, in, mid))
	for range out { //nolint:revive // draining
	}
	wg.Wait()

	// Let the bus worker pool drain.
	time.Sleep(500 * time.Millisecond)

	cap.mu.Lock()
	toolStarted := append([]*events.ToolCallEventData(nil), cap.toolStarted...)
	providerStart := append([]*events.ProviderCallStartedData(nil), cap.providerStart...)
	providerDone := append([]*events.ProviderCallCompletedData(nil), cap.providerDone...)
	cap.mu.Unlock()

	recorded := store.snapshot()

	t.Logf("live turn: %d provider.started, %d provider.completed, %d tool.started, %d message.created",
		len(providerStart), len(providerDone), len(toolStarted), len(recorded))

	// --- The loop must actually be multi-round, or nothing below discriminates.
	require.GreaterOrEqual(t, len(providerStart), 2,
		"model did not run a multi-round tool loop; the single-round case cannot "+
			"distinguish this change from the pre-existing last-turn-only behavior")
	require.NotEmpty(t, toolStarted, "model made no tool calls")

	// --- #1840: provider.call.started IS emitted, contrary to the issue's premise.
	assert.NotEmpty(t, providerStart, "provider.call.started was not emitted")

	// --- #1840: round + correlation ID on every event.
	startByID := make(map[string]int)
	for _, d := range providerStart {
		assert.NotZerof(t, d.Round, "provider.call.started stamped round 0")
		require.NotEmpty(t, d.CallID, "provider.call.started carries no call ID")
		startByID[d.CallID] = d.Round
	}
	assert.Lenf(t, startByID, len(providerStart),
		"provider call IDs are not unique per round (%d IDs for %d calls)",
		len(startByID), len(providerStart))

	for _, d := range providerDone {
		assert.NotZero(t, d.Round, "provider.call.completed stamped round 0")
		round, ok := startByID[d.CallID]
		if assert.Truef(t, ok, "completed call %q has no matching started call", d.CallID) {
			assert.Equal(t, round, d.Round, "round disagrees between started and completed")
		}
	}

	for _, d := range toolStarted {
		assert.NotZerof(t, d.Round, "tool %q stamped round 0 — the silent-zero failure mode", d.ToolName)
		require.NotEmptyf(t, d.ProviderCallID, "tool %q carries no provider call ID", d.ToolName)
		round, ok := startByID[d.ProviderCallID]
		assert.Truef(t, ok, "tool %q points at an unknown provider call %q", d.ToolName, d.ProviderCallID)
		assert.Equalf(t, round, d.Round,
			"tool %q says round %d but its provider call says round %d", d.ToolName, d.Round, round)
	}

	// --- #1839: reasoning reaches a recorder, per round, from a REAL model.
	var withReasoning []*events.MessageCreatedData
	for _, d := range recorded {
		if d.Reasoning != nil && d.Reasoning.Text != "" {
			withReasoning = append(withReasoning, d)
		}
	}

	for i, d := range recorded {
		note := "no reasoning"
		if d.Reasoning != nil && d.Reasoning.Text != "" {
			note = "reasoning: " + d.Reasoning.Text[:min(90, len(d.Reasoning.Text))] + "…"
		}
		t.Logf("message[%d] role=%s tool_calls=%d content=%.40q %s",
			i, d.Role, len(d.ToolCalls), d.Content, note)
	}

	require.NotEmpty(t, withReasoning,
		"no message.created carried a reasoning trace — either the model returned no "+
			"thinking, or the trace is being dropped at the recording boundary")

	for i, d := range withReasoning {
		// Reasoning must never contaminate conversational content.
		assert.NotContainsf(t, d.Content, d.Reasoning.Text,
			"reasoning leaked into message content on message %d", i)
	}

	// The claim that makes #1839 worth shipping: at least one trace belongs to a
	// message the TERMINAL RESPONSE cannot deliver.
	//
	// Response.Message().Reasoning reports the LAST assistant message only. So
	// this change earns its keep exactly when reasoning lands somewhere else —
	// on a tool-calling round, explaining why the model chose the calls it did.
	// Observed live: Claude thinks before deciding to call tools, then answers
	// from the results without further thinking, so the final assistant message
	// carries no trace at all and the terminal-response workaround captures
	// nothing.
	lastAssistantIdx := -1
	for i, d := range recorded {
		if d.Role == roleAssistant {
			lastAssistantIdx = i
		}
	}
	require.GreaterOrEqual(t, lastAssistantIdx, 0, "no assistant message was recorded")

	reachableFromTerminalResponse := 0
	unreachable := 0
	for i, d := range recorded {
		if d.Reasoning == nil || d.Reasoning.Text == "" {
			continue
		}
		if i == lastAssistantIdx {
			reachableFromTerminalResponse++
		} else {
			unreachable++
		}
	}

	t.Logf("reasoning traces: %d reachable from the terminal response, %d reachable ONLY via message.created",
		reachableFromTerminalResponse, unreachable)

	assert.Positivef(t, unreachable,
		"every reasoning trace sat on the final assistant message, so the pre-existing "+
			"terminal-response path would have sufficed; this run does not demonstrate "+
			"the gap #1839 exists to close (traces=%d)", len(withReasoning))
}
