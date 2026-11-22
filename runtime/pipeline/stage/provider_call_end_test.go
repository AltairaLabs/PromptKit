package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// callEndProvider is the mock tool provider whose failAt'th streamed call
// (0-based; -1 for none) fails after keepDeltas of its text deltas.
type callEndProvider struct {
	*mock.ToolProvider
	failAt     int32
	keepDeltas int
	calls      atomic.Int32
}

var errCallEndBroke = errors.New("stream broke")

func (p *callEndProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools interface{}, choice string,
) (<-chan providers.StreamChunk, error) {
	in, err := p.ToolProvider.PredictStreamWithTools(ctx, req, tools, choice)
	if err != nil || p.calls.Add(1)-1 != p.failAt {
		return in, err
	}
	out := make(chan providers.StreamChunk, providers.DefaultStreamBufferSize)
	go func() {
		defer close(out)
		sent := 0
		for chunk := range in {
			if chunk.FinishReason != nil || sent == p.keepDeltas {
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
	t.Run("re-ask fails", func(t *testing.T) {
		stg := callEndStage(t, &callEndProvider{ToolProvider: mock.NewToolProviderWithRepository(
			"mock", "mock-model", false, &turnScript{turns: turns}), failAt: 2, keepDeltas: 0},
			&ProviderConfig{ResponseFormat: jsonSchemaFormat()})

		elems, err := runProviderStage(t, stg, "where is 42?")
		require.NoError(t, err, "a failed re-ask keeps the unconstrained answer")

		assert.Equal(t, []string{
			"end m0 tool_use [call_0_lookup]",
			"end m1 stop []",
			"end m1 error []",
			"stored m0 tool_use", "stored m1 stop",
		}, describeCallEnds(elems))
	})
}
