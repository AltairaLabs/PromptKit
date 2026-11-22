package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callEndProvider is the mock tool provider whose failAt'th streamed call
// (0-based; -1 for none) fails after keepDeltas of its text deltas. With
// keepCalls the failing call first sends the reply's tool calls, complete,
// as a chunk that does not end the reply.
type callEndProvider struct {
	*mock.ToolProvider
	failAt     int32
	keepDeltas int
	keepCalls  bool
	calls      atomic.Int32
	requests   []providers.PredictionRequest
	mu         sync.Mutex
}

func (p *callEndProvider) record(req providers.PredictionRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
}

// requestTexts lists the text of every message of every request the model
// was sent.
func (p *callEndProvider) requestTexts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, req := range p.requests {
		for i := range req.Messages {
			out = append(out, req.Messages[i].GetContent())
		}
	}
	return out
}

func (p *callEndProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	return p.ToolProvider.PredictStream(ctx, req)
}

func (p *callEndProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, tools interface{}, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.record(req)
	return p.ToolProvider.PredictWithTools(ctx, req, tools, choice)
}

var errCallEndBroke = errors.New("stream broke")

func (p *callEndProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools interface{}, choice string,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	in, err := p.ToolProvider.PredictStreamWithTools(ctx, req, tools, choice)
	if err != nil || p.calls.Add(1)-1 != p.failAt {
		return in, err
	}
	out := make(chan providers.StreamChunk, providers.DefaultStreamBufferSize)
	go func() {
		defer close(out)
		sent := 0
		for chunk := range in {
			if chunk.FinishReason != nil {
				if p.keepCalls && len(chunk.ToolCalls) > 0 {
					out <- providers.StreamChunk{Content: chunk.Content, ToolCalls: chunk.ToolCalls}
				}
				continue
			}
			if sent == p.keepDeltas {
				continue
			}
			out <- chunk
			sent++
		}
		out <- providers.StreamChunk{Error: errCallEndBroke}
	}()
	return out, nil
}

func callEndStage(t *testing.T, provider providers.Provider, cfg *ProviderConfig) *ProviderStage {
	t.Helper()
	turnState := NewTurnState()
	turnState.AllowedTools = []string{"lookup"}
	return NewProviderStageWithTurnState(provider, mockToolRegistry(t, "lookup"), &pipeline.ToolPolicy{},
		cfg, nil, nil, turnState)
}

func callEndTurns() []mock.Turn {
	return []mock.Turn{
		{Type: "tool_calls", StreamChunks: []string{"Let me ", "look."},
			ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "42"}}}},
		{Type: "text", StreamChunks: []string{"Found ", "it."}},
	}
}

// describeCallEnds renders the text deltas, the provider call ends and the
// assistant transcript messages of a run, naming message ids m0, m1, ... in
// the order they first appear.
func describeCallEnds(elems []StreamElement) []string {
	labels := map[string]string{}
	label := func(id string) string {
		if id == "" {
			return "-"
		}
		if l, ok := labels[id]; ok {
			return l
		}
		l := fmt.Sprintf("m%d", len(labels))
		labels[id] = l
		return l
	}
	var out []string
	for i := range elems {
		e := &elems[i]
		switch {
		case e.Text != nil && *e.Text != "":
			out = append(out, "text "+label(e.Meta.MessageID)+" "+*e.Text)
		case e.Meta.ProviderCallEnd != nil:
			end := e.Meta.ProviderCallEnd
			ids := make([]string, 0, len(end.ToolCalls))
			for _, tc := range end.ToolCalls {
				ids = append(ids, tc.ID)
			}
			out = append(out, fmt.Sprintf("end %s %s [%s]", label(end.MessageID), end.FinishReason, strings.Join(ids, " ")))
		case e.Message != nil && e.Message.Role == roleAssistant:
			out = append(out, "stored "+label(e.Message.ID())+" "+e.Message.FinishReason)
		case e.Error != nil:
			out = append(out, "error")
		}
	}
	return out
}

// Each provider call of a per-turn streamed execution ends with its
// ProviderCallEnd, after its text and before its calls run, the last call
// included, naming the id its message is stored under.
func TestProviderCallEnd_EndsEveryCallOfAStreamedTurn(t *testing.T) {
	stg := callEndStage(t, mock.NewToolProviderWithRepository("mock", "mock-model", false,
		&turnScript{turns: callEndTurns()}), &ProviderConfig{})

	elems, err := runProviderStage(t, stg, "where is 42?")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text m0 Let me ", "text m0 look.",
		"end m0 tool_use [call_0_lookup]",
		"text m1 Found ", "text m1 it.",
		"end m1 stop []",
		"stored m0 tool_use", "stored m1 stop",
	}, describeCallEnds(elems))
}

// The continuous (duplex) mode sends no ProviderCallEnd, a structured-output
// re-ask's included.
func TestProviderCallEnd_ContinuousModeSendsNone(t *testing.T) {
	turns := append(callEndTurns(), mock.Turn{Type: "text", StreamChunks: []string{`{"found":true}`}})
	stg := callEndStage(t, mock.NewToolProviderWithRepository("mock", "mock-model", false,
		&turnScript{turns: turns}), &ProviderConfig{Streaming: true, ResponseFormat: jsonSchemaFormat()})

	elems, err := runProviderStage(t, stg, "where is 42?")
	require.NoError(t, err)

	// The tool round's text is withheld with the schema; the re-ask's answer
	// streams under the id it is stored under.
	assert.Equal(t, []string{
		`text m0 {"found":true}`,
		"stored m1 tool_use", "stored m0 stop",
	}, describeCallEnds(elems))
}

// A call that fails mid-stream ends with its ProviderCallEnd before the
// error: interrupted when its partial reply is kept, error when nothing is.
func TestProviderCallEnd_FailedCall(t *testing.T) {
	t.Run("partial reply kept", func(t *testing.T) {
		stg := callEndStage(t, &callEndProvider{ToolProvider: mock.NewToolProviderWithRepository(
			"mock", "mock-model", false, &turnScript{turns: callEndTurns()}), failAt: 1, keepDeltas: 1},
			&ProviderConfig{})

		elems, err := runProviderStage(t, stg, "where is 42?")
		require.ErrorIs(t, err, errCallEndBroke)

		assert.Equal(t, []string{
			"text m0 Let me ", "text m0 look.",
			"end m0 tool_use [call_0_lookup]",
			"text m1 Found ",
			"end m1 interrupted []",
			"stored m0 tool_use", "stored m1 interrupted",
			"error",
		}, describeCallEnds(elems))
	})
	t.Run("nothing kept", func(t *testing.T) {
		stg := callEndStage(t, &callEndProvider{ToolProvider: mock.NewToolProviderWithRepository(
			"mock", "mock-model", false, &turnScript{turns: callEndTurns()}), failAt: 1, keepDeltas: 0},
			&ProviderConfig{})

		elems, err := runProviderStage(t, stg, "where is 42?")
		require.ErrorIs(t, err, errCallEndBroke)

		assert.Equal(t, []string{
			"text m0 Let me ", "text m0 look.",
			"end m0 tool_use [call_0_lookup]",
			"end m1 error []",
			"error",
		}, describeCallEnds(elems))
	})
}

// A structured-output re-ask is a provider call of its own: its answer is
// stored under the id of the reply it replaces, which its text and its end
// name. A re-ask that fails ends with error and keeps the reply it would
// have replaced.
func TestProviderCallEnd_StructuredOutputReask(t *testing.T) {
	turns := append(callEndTurns(), mock.Turn{Type: "text", StreamChunks: []string{`{"found":`, `true}`}})
	t.Run("answered", func(t *testing.T) {
		stg := callEndStage(t, mock.NewToolProviderWithRepository("mock", "mock-model", false,
			&turnScript{turns: turns}), &ProviderConfig{ResponseFormat: jsonSchemaFormat()})

		elems, err := runProviderStage(t, stg, "where is 42?")
		require.NoError(t, err)

		assert.Equal(t, []string{
			"end m0 tool_use [call_0_lookup]",
			"end m1 stop []",
			`text m1 {"found":`, "text m1 true}",
			"end m1 stop []",
			"stored m0 tool_use", "stored m1 stop",
		}, describeCallEnds(elems))
	})
	// A failed re-ask keeps the unconstrained answer under the reply's id: the
	// answer streams under it, as the stored message holds it, and nothing of
	// the re-ask does, however much of it the model sent. The re-ask's end
	// names an id of its own, under which nothing is stored.
	for _, keep := range []int{0, 1} {
		t.Run(fmt.Sprintf("re-ask fails after %d deltas", keep), func(t *testing.T) {
			stg := callEndStage(t, &callEndProvider{ToolProvider: mock.NewToolProviderWithRepository(
				"mock", "mock-model", false, &turnScript{turns: turns}), failAt: 2, keepDeltas: keep},
				&ProviderConfig{ResponseFormat: jsonSchemaFormat()})

			elems, err := runProviderStage(t, stg, "where is 42?")
			require.NoError(t, err, "a failed re-ask keeps the unconstrained answer")

			assert.Equal(t, []string{
				"end m0 tool_use [call_0_lookup]",
				"end m1 stop []",
				"text m1 Found it.",
				"end m2 error []",
				"stored m0 tool_use", "stored m1 stop",
			}, describeCallEnds(elems))
		})
	}
}

// A call that fails after its tool calls arrived keeps them on the
// interrupted reply, where they never run; its end reports no calls, since
// none of them is reported or run.
func TestProviderCallEnd_InterruptedCallReportsNoToolCalls(t *testing.T) {
	stg := callEndStage(t, &callEndProvider{ToolProvider: mock.NewToolProviderWithRepository(
		"mock", "mock-model", false, &turnScript{turns: callEndTurns()}), failAt: 0, keepDeltas: 2, keepCalls: true},
		&ProviderConfig{})

	elems, err := runProviderStage(t, stg, "where is 42?")
	require.ErrorIs(t, err, errCallEndBroke)

	assert.Equal(t, []string{
		"text m0 Let me ", "text m0 look.",
		"end m0 interrupted []",
		"stored m0 interrupted",
		"error",
	}, describeCallEnds(elems))
	var kept *types.Message
	for i := range elems {
		if m := elems[i].Message; m != nil && m.Role == roleAssistant {
			kept = m
		}
	}
	require.NotNil(t, kept)
	require.Len(t, kept.ToolCalls, 1, "the interrupted reply keeps its complete call")
	assert.Equal(t, "call_0_lookup", kept.ToolCalls[0].ID)
}

// blockRound is a provider hook whose BeforeCall blocks round round of a turn
// with an enforced decision, and allows every other.
type blockRound struct{ round int }

func (h *blockRound) Name() string { return "block_round" }

func (h *blockRound) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	if req.Round == h.round {
		return hooks.Enforced("not allowed", nil)
	}
	return hooks.Allow
}

func (h *blockRound) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

// A structured-output turn a BeforeCall guardrail blocks is not re-asked:
// the blocked message stays the reply, nothing blocked reaches the model,
// and the blocked round, which made no provider call, sends no end.
func TestProviderCallEnd_BlockedTurnIsNotReasked(t *testing.T) {
	const secret = "the blocked words"
	for _, round := range []int{1, 2} {
		t.Run(fmt.Sprintf("blocked at round %d", round), func(t *testing.T) {
			turns := append(callEndTurns(), mock.Turn{Type: "text", StreamChunks: []string{`{"found":true}`}})
			provider := &callEndProvider{ToolProvider: mock.NewToolProviderWithRepository(
				"mock", "mock-model", false, &turnScript{turns: turns}), failAt: -1}
			turnState := NewTurnState()
			turnState.AllowedTools = []string{"lookup"}
			stg := NewProviderStageWithTurnState(provider, mockToolRegistry(t, "lookup"), &pipeline.ToolPolicy{},
				&ProviderConfig{ResponseFormat: jsonSchemaFormat()}, nil,
				hooks.NewRegistry(hooks.WithProviderHook(&blockRound{round: round})), turnState)

			elems, err := runProviderStage(t, stg, secret)
			require.NoError(t, err)

			var reply *types.Message
			for i := range elems {
				if m := elems[i].Message; m != nil && m.Role == roleAssistant {
					reply = m
				}
			}
			require.NotNil(t, reply)
			assert.True(t, isMarkedBlockedInput(reply), "the blocked message is the turn's reply")
			assert.NotContains(t, reply.Content, "found", "the re-ask did not replace it")
			if round == 1 {
				assert.NotContains(t, provider.requestTexts(), secret, "the blocked input never reached the model")
				assert.Empty(t, provider.requestTexts())
			} else {
				assert.Len(t, provider.requests, 1, "only the allowed round called the model")
			}
			ends := 0
			for i := range elems {
				if elems[i].Meta.ProviderCallEnd != nil {
					ends++
				}
			}
			assert.Equal(t, round-1, ends, "one end per provider call made")
		})
	}
}
