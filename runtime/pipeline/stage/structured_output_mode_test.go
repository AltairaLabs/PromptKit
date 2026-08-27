package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// schemaSpyProvider records the ResponseFormat and tool presence of every call
// it receives, across both the tool path and the plain Predict path.
//
// Recording per call is the point: the defect this guards against is a schema
// reaching the WRONG calls, so a spy that only reports "a schema was sent
// somewhere" would pass against the bug.
type schemaSpyProvider struct {
	providers.Provider
	toolRounds int
	failReask  bool
	reaskCost  float64

	mu           sync.Mutex
	calls        []spiedCall
	plainPredict bool
}

// sawPlainPredict reports whether any re-ask took the non-tool request path.
func (p *schemaSpyProvider) sawPlainPredict() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.plainPredict
}

type spiedCall struct {
	withTools bool
	schema    bool
}

func (p *schemaSpyProvider) ID() string              { return "spy" }
func (p *schemaSpyProvider) Name() string            { return "spy" }
func (p *schemaSpyProvider) Model() string           { return "spy-model" }
func (p *schemaSpyProvider) Type() base.ProviderType { return base.ProviderTypeInference }
func (p *schemaSpyProvider) Close() error            { return nil }
func (p *schemaSpyProvider) SupportsStreaming() bool { return false }

func (p *schemaSpyProvider) BuildTooling(d []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	return d, nil
}

func (p *schemaSpyProvider) record(withTools bool, rf *providers.ResponseFormat) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, spiedCall{withTools: withTools, schema: rf != nil})
}

func (p *schemaSpyProvider) snapshot() []spiedCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]spiedCall, len(p.calls))
	copy(out, p.calls)
	return out
}

func (p *schemaSpyProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(false, req.ResponseFormat)
	p.mu.Lock()
	p.plainPredict = true
	p.mu.Unlock()
	// Reject a transcript carrying tool-result roles, exactly as a real
	// provider does. Claude answers
	//   400 messages: Unexpected role "tool"
	// because its plain-Predict serializer is tool-blind — a live-only failure
	// until this fake reproduced it. Without this the re-ask could regress to
	// plain Predict and every canned test here would still pass.
	for i := range req.Messages {
		if req.Messages[i].Role == "tool" {
			return providers.PredictionResponse{}, fmt.Errorf(
				`provider: 400 messages: Unexpected role "tool"`)
		}
	}
	return p.finalAnswer()
}

func (p *schemaSpyProvider) PredictStreamWithTools(
	_ context.Context, _ providers.PredictionRequest,
	_ providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

// PredictWithTools serves both roles the stage puts through this method: a
// tool-loop round (tools offered) and the final re-ask (nil tools, taken via
// the tool path only because the transcript carries tool-result roles a plain
// Predict serializer would reject).
//
// withTools therefore records whether tools were OFFERED, not which method was
// called — the re-ask's defining property is that the model has nothing to
// choose from, and recording the method name would report the opposite.
func (p *schemaSpyProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest,
	providerTools providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	offered := providerTools != nil
	p.record(offered, req.ResponseFormat)
	if !offered {
		resp, err := p.finalAnswer()
		return resp, nil, err
	}

	p.mu.Lock()
	n := 0
	for _, c := range p.calls {
		if c.withTools {
			n++
		}
	}
	p.mu.Unlock()

	if n > p.toolRounds {
		return providers.PredictionResponse{Content: "unconstrained prose"}, nil, nil
	}
	return providers.PredictionResponse{}, []types.MessageToolCall{{
		ID:   "call_" + string(rune('A'+n-1)),
		Name: "probe",
		Args: json.RawMessage(`{"q":"` + string(rune('a'+n-1)) + `"}`),
	}}, nil
}

// finalAnswer is what a re-ask returns, on either request path.
func (p *schemaSpyProvider) finalAnswer() (providers.PredictionResponse, error) {
	if p.failReask {
		return providers.PredictionResponse{}, assert.AnError
	}
	resp := providers.PredictionResponse{Content: `{"answer":"constrained"}`}
	if p.reaskCost > 0 {
		resp.CostInfo = &types.CostInfo{TotalCost: p.reaskCost}
	}
	return resp, nil
}

// runSchemaLoop drives a full turn and returns the spy plus the final messages.
func runSchemaLoop(
	t *testing.T, p *schemaSpyProvider, cfg *ProviderConfig, allowedTools []string,
) []types.Message {
	t.Helper()

	reg := registryWithTools(t, "probe")
	reg.RegisterExecutor(staticExecutor{})

	turnState := NewTurnState()
	turnState.SystemPrompt = "sys"
	turnState.AllowedTools = allowedTools

	stage := NewProviderStageWithTurnState(p, reg, nil, cfg, nil, nil, turnState)

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "go"}
	input <- NewMessageElement(&userMsg)
	close(input)

	output := make(chan StreamElement, 64)
	require.NoError(t, stage.Process(context.Background(), input, output))

	var msgs []types.Message
	for elem := range output {
		if elem.Message != nil {
			msgs = append(msgs, *elem.Message)
		}
	}
	return msgs
}

func jsonSchemaFormat() *providers.ResponseFormat {
	return &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object"}`),
	}
}

// TestFinalTurn_SchemaOffToolRoundsAndOnTheReask is the core guarantee.
//
// Asserted per call rather than in aggregate: the bug being prevented is a
// schema attached to tool-calling rounds, which an aggregate check ("a schema
// was sent at least once") cannot see.
func TestFinalTurn_SchemaOffToolRoundsAndOnTheReask(t *testing.T) {
	p := &schemaSpyProvider{toolRounds: 2}
	msgs := runSchemaLoop(t, p, &ProviderConfig{
		MaxTokens:      100,
		ResponseFormat: jsonSchemaFormat(),
	}, []string{"probe"})

	calls := p.snapshot()
	require.Len(t, calls, 4, "2 tool rounds + the round that ends the loop + the re-ask")

	for i, c := range calls[:3] {
		assert.True(t, c.withTools, "call %d should be a tool-path round", i+1)
		assert.False(t, c.schema,
			"call %d carried the schema; a schema on tool-calling rounds is the defect (#1853)", i+1)
	}

	assert.False(t, calls[3].withTools, "the re-ask must offer no tools")
	assert.True(t, calls[3].schema, "the re-ask must carry the schema")

	require.NotEmpty(t, msgs)
	last := msgs[len(msgs)-1]
	assert.JSONEq(t, `{"answer":"constrained"}`, last.Content,
		"the turn must return the constrained answer, not the prose that ended the loop")
	assert.NotContains(t, last.Content, "unconstrained",
		"the discarded answer must not survive into the result")
}

// TestFinalTurn_NoToolsMeansNoExtraCall pins the cost boundary: a turn with no
// tools has no loop to protect, so it keeps a single constrained call.
func TestFinalTurn_NoToolsMeansNoExtraCall(t *testing.T) {
	p := &schemaSpyProvider{toolRounds: 0}
	runSchemaLoop(t, p, &ProviderConfig{
		MaxTokens:      100,
		ResponseFormat: jsonSchemaFormat(),
	}, nil)

	calls := p.snapshot()
	require.Len(t, calls, 1, "a tool-free turn must not pay for a re-ask")
	assert.True(t, calls[0].schema, "and its single call must still carry the schema")
}

// TestEveryRoundMode_RestoresTheOldBehavior proves the escape hatch actually
// escapes — including that it costs no re-ask.
func TestEveryRoundMode_RestoresTheOldBehavior(t *testing.T) {
	p := &schemaSpyProvider{toolRounds: 2}
	runSchemaLoop(t, p, &ProviderConfig{
		MaxTokens:            100,
		ResponseFormat:       jsonSchemaFormat(),
		StructuredOutputMode: StructuredOutputEveryRound,
	}, []string{"probe"})

	calls := p.snapshot()
	require.Len(t, calls, 3, "every_round must not add a re-ask")
	for i, c := range calls {
		assert.True(t, c.schema, "call %d must carry the schema under every_round", i+1)
	}
}

// TestFinalTurn_NoResponseFormatChangesNothing guards the default path: with no
// schema configured there is nothing to withhold and nothing to re-ask.
func TestFinalTurn_NoResponseFormatChangesNothing(t *testing.T) {
	p := &schemaSpyProvider{toolRounds: 1}
	runSchemaLoop(t, p, &ProviderConfig{MaxTokens: 100}, []string{"probe"})

	calls := p.snapshot()
	require.Len(t, calls, 2, "1 tool round + the round that ends it; no re-ask")
	for i, c := range calls {
		assert.False(t, c.schema, "call %d invented a schema the caller never set", i+1)
	}
}

// TestFinalTurn_ReaskFailureKeepsTheLoopsWork pins the degradation choice: a
// failed re-ask loses the schema, not the completed tool work.
func TestFinalTurn_ReaskFailureKeepsTheLoopsWork(t *testing.T) {
	p := &schemaSpyProvider{toolRounds: 1, failReask: true}
	msgs := runSchemaLoop(t, p, &ProviderConfig{
		MaxTokens:      100,
		ResponseFormat: jsonSchemaFormat(),
	}, []string{"probe"})

	require.NotEmpty(t, msgs, "a failed re-ask must not fail the turn")
	last := msgs[len(msgs)-1]
	assert.Equal(t, "unconstrained prose", last.Content,
		"the loop's own answer must survive when the re-ask cannot replace it")

	var toolResults int
	for _, m := range msgs {
		if m.Role == "tool" || len(m.ToolCalls) > 0 {
			toolResults++
		}
	}
	assert.Positive(t, toolResults, "the completed tool work must still be returned")

	// The degradation must be detectable. Handing back prose with nothing to
	// distinguish it from a model that just answered that way is the same
	// unobservable success this mode exists to remove — and a live 400 produced
	// exactly that before this marker existed.
	require.NotNil(t, last.Meta, "a failed re-ask left no trace on the message")
	cause, ok := last.Meta[ReaskFailedMetaKey].(string)
	require.True(t, ok, "expected %s on the un-replaced answer", ReaskFailedMetaKey)
	assert.NotEmpty(t, cause, "the marker must carry the provider error, not just a flag")
}

func TestParseStructuredOutputMode(t *testing.T) {
	assert.Equal(t, StructuredOutputFinalTurn, ParseStructuredOutputMode("final_turn"))
	assert.Equal(t, StructuredOutputFinalTurn, ParseStructuredOutputMode("  FINAL_TURN "))
	assert.Equal(t, StructuredOutputEveryRound, ParseStructuredOutputMode("every_round"))
	assert.Equal(t, StructuredOutputEveryRound, ParseStructuredOutputMode("legacy"))

	// An unrecognized value must fall through to the default rather than being
	// forwarded — guessing here silently changes whether a caller's schema
	// constrains tool-calling rounds.
	assert.Equal(t, StructuredOutputFinalTurn, ParseStructuredOutputMode("nonsense").resolve())
	assert.Equal(t, StructuredOutputFinalTurn, ParseStructuredOutputMode("").resolve())
}

// streamingSpyProvider streams a reasoning delta and a text delta per round,
// then ends the loop. Streaming is what most providers do — BaseProvider
// reports SupportsStreaming true — so this, not the unary path, is where a
// leaked partial would actually reach a client.
type streamingSpyProvider struct {
	schemaSpyProvider
}

func (p *streamingSpyProvider) SupportsStreaming() bool { return true }

func (p *streamingSpyProvider) PredictStreamWithTools(
	_ context.Context, req providers.PredictionRequest,
	_ providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	p.record(true, req.ResponseFormat)

	p.mu.Lock()
	n := 0
	for _, c := range p.calls {
		if c.withTools {
			n++
		}
	}
	p.mu.Unlock()

	ch := make(chan providers.StreamChunk, 4)
	go func() {
		defer close(ch)
		ch <- providers.StreamChunk{Reasoning: "deliberating"}
		ch <- providers.StreamChunk{Delta: "leaked partial"}
		if n <= p.toolRounds {
			ch <- providers.StreamChunk{ToolCalls: []types.MessageToolCall{{
				ID:   "call_" + string(rune('A'+n-1)),
				Name: "probe",
				Args: json.RawMessage(`{"q":"` + string(rune('a'+n-1)) + `"}`),
			}}}
		}
	}()
	return ch, nil
}

// TestFinalTurn_StreamingPartialsNeverReachTheClient guards the leak path.
//
// Suppression is per LOOP, not per round: a round is only known to be the last
// one once it has finished, far too late to have withheld its deltas. So every
// round's text is dropped, and the consumer's only text is the constrained
// answer. Reasoning deltas are a separate non-content channel and must survive.
func TestFinalTurn_StreamingPartialsNeverReachTheClient(t *testing.T) {
	p := &streamingSpyProvider{schemaSpyProvider{toolRounds: 2}}

	reg := registryWithTools(t, "probe")
	reg.RegisterExecutor(staticExecutor{})
	turnState := NewTurnState()
	turnState.SystemPrompt = "sys"
	turnState.AllowedTools = []string{"probe"}

	stage := NewProviderStageWithTurnState(p, reg, nil, &ProviderConfig{
		MaxTokens:      100,
		ResponseFormat: jsonSchemaFormat(),
	}, nil, nil, turnState)

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "go"}
	input <- NewMessageElement(&userMsg)
	close(input)

	output := make(chan StreamElement, 128)
	require.NoError(t, stage.Process(context.Background(), input, output))

	var textDeltas, reasoningDeltas int
	var msgs []types.Message
	for elem := range output {
		switch {
		case elem.Reasoning != nil:
			reasoningDeltas++
		case elem.Message != nil:
			msgs = append(msgs, *elem.Message)
		case elem.Meta.StreamingDelta:
			textDeltas++
		}
	}

	assert.Zero(t, textDeltas,
		"a schema-withheld loop leaked %d text delta(s); those partials are prose "+
			"the caller never asked for and are discarded by the re-ask", textDeltas)
	assert.Positive(t, reasoningDeltas,
		"reasoning deltas are a separate non-content channel and must keep flowing")

	require.NotEmpty(t, msgs)
	assert.JSONEq(t, `{"answer":"constrained"}`, msgs[len(msgs)-1].Content)
}

// TestFinalTurn_ReaskCarriesWorkflowAttribution pins the stamp hand-off.
//
// The re-ask replaces the message a round produced, so it must inherit that
// round's workflow attribution. Re-deriving it instead would read whatever
// state a mid-loop handoff has since moved on to, silently misattributing the
// answer to the wrong state.
func TestFinalTurn_ReaskCarriesWorkflowAttribution(t *testing.T) {
	p := &schemaSpyProvider{toolRounds: 1}

	reg := registryWithTools(t, "probe")
	reg.RegisterExecutor(staticExecutor{})
	turnState := NewTurnState()
	turnState.SystemPrompt = "sys"
	turnState.AllowedTools = []string{"probe"}

	st := NewProviderStageWithTurnState(p, reg, nil, &ProviderConfig{
		MaxTokens:      100,
		ResponseFormat: jsonSchemaFormat(),
	}, nil, nil, turnState)
	st.SetWorkflowStateResolver(&fakeResolver{meta: map[string]any{"state": "underwriting"}})

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "go"}
	input <- NewMessageElement(&userMsg)
	close(input)

	output := make(chan StreamElement, 64)
	require.NoError(t, st.Process(context.Background(), input, output))

	var last types.Message
	for elem := range output {
		if elem.Message != nil && elem.Message.Role == roleAssistant {
			last = *elem.Message
		}
	}

	assert.JSONEq(t, `{"answer":"constrained"}`, last.Content, "expected the re-asked answer")
	require.NotNil(t, last.Meta, "the constrained answer lost its workflow metadata")
	assert.Equal(t, map[string]any{"state": "underwriting"}, last.Meta[workflowStateMetaKey],
		"the re-ask must inherit the attribution of the answer it replaced")
}

// TestFinalTurn_ReaskCostIsCounted pins that the extra call is billed.
//
// The re-ask is a real provider call and its spend must reach the loop's
// running total, or a MaxCostUSD budget silently under-counts by one call per
// structured-output turn.
func TestFinalTurn_ReaskCostIsCounted(t *testing.T) {
	p := &schemaSpyProvider{toolRounds: 1, reaskCost: 0.25}
	msgs := runSchemaLoop(t, p, &ProviderConfig{
		MaxTokens:      100,
		ResponseFormat: jsonSchemaFormat(),
	}, []string{"probe"})

	var last types.Message
	for _, m := range msgs {
		if m.Role == roleAssistant && m.Content != "" {
			last = m
		}
	}
	require.NotNil(t, last.CostInfo, "the re-ask must carry its cost onto the message")
	assert.InDelta(t, 0.25, last.CostInfo.TotalCost, 1e-9)
	assert.Equal(t, "spy", last.CostInfo.ProviderName,
		"the re-ask must be stamped with provider identity like any other round")
}

// TestFinalTurn_ToolFreeTranscriptUsesPlainPredict covers the other half of the
// re-ask's path choice.
//
// A loop that ends on its first round never produced a tool message, so there
// is no tool linkage and the plain Predict path is correct — the tool-aware
// path is a workaround for serialization, not the preferred route.
func TestFinalTurn_ToolFreeTranscriptUsesPlainPredict(t *testing.T) {
	p := &schemaSpyProvider{toolRounds: 0}
	msgs := runSchemaLoop(t, p, &ProviderConfig{
		MaxTokens:      100,
		ResponseFormat: jsonSchemaFormat(),
	}, []string{"probe"})

	calls := p.snapshot()
	require.Len(t, calls, 2, "one round that called nothing, plus the re-ask")
	assert.True(t, calls[0].withTools, "the round offered tools")
	assert.False(t, calls[0].schema, "and carried no schema")
	assert.False(t, calls[1].withTools, "the re-ask offered none")
	assert.True(t, calls[1].schema)
	assert.True(t, p.sawPlainPredict(), "a tool-free transcript should not need the tool path")

	var last types.Message
	for _, m := range msgs {
		if m.Role == roleAssistant && m.Content != "" {
			last = m
		}
	}
	assert.JSONEq(t, `{"answer":"constrained"}`, last.Content)
}
