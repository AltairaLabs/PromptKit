//go:build integration

package sdk_test

import (
	"context"
	"os"
	"strings"
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

// liveReasoningProvider names one real reasoning-capable provider and how to
// turn its thinking on. Each vendor exposes reasoning differently on the wire —
// Claude streams thinking blocks, Gemini marks parts with thought:true, OpenAI
// returns a reasoning summary via the Responses API — so a provider-agnostic
// claim is only as good as the providers actually exercised.
type liveReasoningProvider struct {
	name    string
	envKeys []string
	spec    providers.ProviderSpec

	// reasonsOnToolRounds records whether the vendor RELIABLY supplies reasoning
	// text on rounds that call tools. Claude and Gemini do, on every observed
	// run.
	//
	// OpenAI o-series does not: observed live returning a reasoning item with an
	// EMPTY summary on one run and a full summary (plus 83 summary deltas) on
	// the next, for the same request. Requiring presence there would be a flaky
	// test, and asserting absence would fail the moment it does supply one — so
	// for these providers presence is optional and only the SHAPE is asserted:
	// whatever reasoning arrives must still be well-formed and correlated.
	reasonsOnToolRounds bool
}

func liveReasoningProviders() []liveReasoningProvider {
	claudeModel := os.Getenv("CLAUDE_THINKING_MODEL")
	if claudeModel == "" {
		claudeModel = "claude-sonnet-4-6"
	}
	geminiModel := os.Getenv("GEMINI_THINKING_MODEL")
	if geminiModel == "" {
		// DELIBERATELY 2.5, and not swept along with the deprecation
		// migration (#1844).
		//
		// This test exists to prove reasoning survives the pipeline on a
		// TOOL-CALLING turn. Gemini 3 does not produce thought summaries on
		// streaming tool rounds at all — a vendor limitation recorded by
		// TestGemini3_StreamingToolRound_NoReasoning_Live, and unaffected by
		// the thinkingLevel fix in #1846, which addressed the non-tool
		// streaming path. Measured here: 3.7-flash gives 0 reasoning.completed
		// and 0 deltas over a two-round tool loop, with thinking_level "high"
		// and include_thoughts set.
		//
		// So moving this default to 3.x would leave the test green while it
		// asserted nothing about reasoning — the precise failure #1844 warned
		// the migration must avoid. 2.5 is the only Gemini generation that
		// exercises this path, so it stays until a 3.x model supplies thoughts
		// on tool rounds.
		geminiModel = "gemini-2.5-flash"
	}
	openaiModel := os.Getenv("OPENAI_REASONING_MODEL")
	if openaiModel == "" {
		openaiModel = "o4-mini"
	}

	return []liveReasoningProvider{
		{
			name:                "claude",
			envKeys:             []string{"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"},
			reasonsOnToolRounds: true,
			spec: providers.ProviderSpec{
				ID: "claude-live", Type: "claude", Model: claudeModel,
				BaseURL:          "https://api.anthropic.com/v1",
				Defaults:         providers.ProviderDefaults{MaxTokens: 4096},
				AdditionalConfig: map[string]interface{}{"thinking_budget": 2048},
			},
		},
		{
			name:                "gemini",
			envKeys:             []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
			reasonsOnToolRounds: true,
			spec: providers.ProviderSpec{
				ID: "gemini-live", Type: "gemini", Model: geminiModel,
				BaseURL:  "https://generativelanguage.googleapis.com/v1beta",
				Defaults: providers.ProviderDefaults{MaxTokens: 4096},
				// include_thoughts is what makes the thought parts visible;
				// without it the model still thinks but returns no summary.
				//
				// The thinking knob is GENERATION-SPECIFIC and must move with
				// the model: Gemini 3 wants thinking_level and largely ignores
				// a budget for summaries, while 2.5 rejects a level with HTTP
				// 400 (see gemini_thinking.go). Sending the 2.5 knob to 3.7
				// yields zero thought parts — the test still runs, still calls
				// its tools, and asserts nothing about reasoning.
				AdditionalConfig: geminiThinkingConfig(geminiModel),
			},
		},
		{
			name:    "openai",
			envKeys: []string{"OPENAI_API_KEY"},
			// See reasonsOnToolRounds: empty summaries on tool-calling rounds.
			reasonsOnToolRounds: false,
			spec: providers.ProviderSpec{
				ID: "openai-live", Type: "openai", Model: openaiModel,
				BaseURL:  "https://api.openai.com/v1",
				Defaults: providers.ProviderDefaults{MaxTokens: 4096},
				// Reasoning text is only available through the Responses API,
				// and only when a summary is requested.
				AdditionalConfig: map[string]interface{}{
					"api_mode":          "responses",
					"reasoning_effort":  "medium",
					"reasoning_summary": "auto",
				},
			},
		},
	}
}

// TestLive_AllReasoningProviders_CorrelatedReasoning runs the same
// non-recording consumer assertions against every real reasoning provider a
// key is available for.
//
// The conformance table in runtime/providers proves each vendor's PARSE against
// a canned wire response. It cannot prove the canned shape matches what the
// vendor actually sends — only a live call does that. This is that check, and
// it runs end to end: real API, real tool loop, events observed at a consumer.
func TestLive_AllReasoningProviders_CorrelatedReasoning(t *testing.T) {
	ran := 0
	for _, lp := range liveReasoningProviders() {
		t.Run(lp.name, func(t *testing.T) {
			var key string
			for _, k := range lp.envKeys {
				if v := os.Getenv(k); v != "" {
					key = v
					break
				}
			}
			if key == "" {
				t.Skipf("none of %v set", lp.envKeys)
			}
			ran++
			res := runLiveReasoningConsumer(t, lp)

			// The invariant every provider must satisfy, reasoning or not: a
			// consumer receives the round's provider events, attributed. This
			// is asserted here rather than in the gathering helper so the
			// subtest itself can fail.
			require.NotEmptyf(t, res.providerStarted,
				"%s: no provider.call.started reached the consumer", lp.name)
			for _, d := range res.providerStarted {
				assert.NotZerof(t, d.Round, "%s: provider.call.started stamped round 0", lp.name)
				assert.NotEmptyf(t, d.CallID, "%s: provider.call.started carries no call ID", lp.name)
			}
		})
	}
	if ran == 0 {
		t.Skip("no provider keys available")
	}
}

// liveConsumerResult is what one live provider run delivered to the consumer.
type liveConsumerResult struct {
	providerStarted []*events.ProviderCallStartedData
	reasoningTraces int
}

// runLiveReasoningConsumer opens a non-recording consumer against one live
// provider, asserts the reasoning-specific expectations, and returns what
// arrived so the caller can assert the provider-agnostic invariants.
func runLiveReasoningConsumer(t *testing.T, lp liveReasoningProvider) liveConsumerResult {
	t.Helper()

	provider, err := providers.CreateProviderFromSpec(lp.spec)
	require.NoErrorf(t, err, "%s: CreateProviderFromSpec", lp.name)

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
	require.NoErrorf(t, err, "%s: Send", lp.name)
	time.Sleep(1 * time.Second)

	completed := store.byType(events.EventReasoningCompleted)
	deltas := store.byType(events.EventReasoningDelta)
	toolStarted := store.byType(events.EventToolCallStarted)
	provStarted := store.byType(events.EventProviderCallStarted)

	t.Logf("%s: %d reasoning.completed, %d reasoning.delta, %d tool.call.started, %d provider.call.started",
		lp.name, len(completed), len(deltas), len(toolStarted), len(provStarted))

	res := liveConsumerResult{}
	for _, e := range provStarted {
		if d, ok := e.Data.(*events.ProviderCallStartedData); ok {
			res.providerStarted = append(res.providerStarted, d)
		}
	}

	switch {
	case lp.reasonsOnToolRounds:
		require.NotEmptyf(t, completed,
			"%s: NO reasoning.completed from a live reasoning model that reliably supplies "+
				"it. Either the vendor changed, or the wire field this repo parses does not "+
				"match what it actually sends. (%d reasoning.delta seen)", lp.name, len(deltas))
	case len(completed) == 0:
		// Allowed for this vendor — see reasonsOnToolRounds. Nothing to
		// correlate, so stop here rather than assert on an empty set.
		t.Logf("  %s: no reasoning on this tool-calling run (vendor supplies it "+
			"only sometimes; %d deltas seen)", lp.name, len(deltas))
		return res
	default:
		t.Logf("  %s: vendor supplied reasoning on this run; asserting shape and correlation",
			lp.name)
	}

	reasoningByCall := map[string]string{}
	for _, e := range completed {
		d, ok := e.Data.(*events.ReasoningCompletedData)
		require.Truef(t, ok, "%s: unexpected payload %T", lp.name, e.Data)
		require.NotNilf(t, d.Trace, "%s: emitted a nil trace", lp.name)
		assert.NotEmptyf(t, d.Trace.Text, "%s: emitted an empty trace", lp.name)
		assert.NotZerof(t, d.Round, "%s: reasoning.completed stamped round 0", lp.name)
		require.NotEmptyf(t, d.ProviderCallID, "%s: reasoning.completed carries no call ID", lp.name)
		reasoningByCall[d.ProviderCallID] = d.Trace.Text
		res.reasoningTraces++
		t.Logf("  %s round %d: %d chars — %.90s…", lp.name, d.Round, len(d.Trace.Text), d.Trace.Text)
	}

	// Where the model used tools, the reasoning behind them must be joinable.
	joined := 0
	for _, e := range toolStarted {
		d := e.Data.(*events.ToolCallEventData)
		assert.NotZerof(t, d.Round, "%s: tool %q stamped round 0", lp.name, d.ToolName)
		if r, found := reasoningByCall[d.ProviderCallID]; found {
			joined++
			assert.NotEmptyf(t, r,
				"%s: tool %q joined to an empty reasoning trace", lp.name, d.ToolName)
		}
	}
	// A provider that reliably reasons on tool rounds must produce at least one
	// joinable pair, or correlation is broken. For a best-effort provider the
	// reasoning may land on a round that called no tools (observed: OpenAI
	// reasoning on round 2 only), so requiring a join there would fail on
	// vendor behaviour rather than on a defect. What still must hold in both
	// cases — asserted in the loop above — is that any tool call sharing a
	// provider call with reasoning maps to a non-empty trace.
	if len(toolStarted) > 0 && lp.reasonsOnToolRounds {
		assert.Positivef(t, joined,
			"%s: %d tool calls, none joinable to the reasoning that produced them",
			lp.name, len(toolStarted))
	}
	t.Logf("  %s: %d/%d tool calls joined to their reasoning", lp.name, joined, len(toolStarted))

	return res
}

// geminiThinkingConfig picks the thinking knob matching a model's generation.
//
// Gemini 3 replaced thinking_budget with thinking_level. Each generation
// rejects or ignores the other's, so this cannot be one static map — and the
// failure is silent in the direction that matters: a 2.5 budget sent to a 3.x
// model returns no thought summaries rather than an error, so a test
// configured that way passes while proving nothing.
func geminiThinkingConfig(model string) map[string]interface{} {
	cfg := map[string]interface{}{"include_thoughts": true}
	if strings.HasPrefix(model, "gemini-2.") || strings.HasPrefix(model, "gemini-1.") {
		cfg["thinking_budget"] = 2048
		return cfg
	}
	cfg["thinking_level"] = "high"
	return cfg
}
