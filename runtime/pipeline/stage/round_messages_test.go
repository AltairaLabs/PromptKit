package stage

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// turnScript answers each model call with the next scripted mock turn, and
// with the last one once the script runs out.
type turnScript struct {
	mu    sync.Mutex
	turns []mock.Turn
	next  int
}

func (s *turnScript) GetTurn(context.Context, mock.ResponseParams) (*mock.Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.turns[min(s.next, len(s.turns)-1)]
	s.next++
	return &t, nil
}

func (s *turnScript) GetResponse(ctx context.Context, p mock.ResponseParams) (string, error) {
	t, err := s.GetTurn(ctx, p)
	return t.Content, err
}

// describeElements renders what a caller rendering the turn as it runs sees
// in each element: a text delta, a reported round message, or a transcript
// message.
func describeElements(elems []StreamElement) []string {
	var out []string
	for i := range elems {
		e := &elems[i]
		switch {
		case e.Text != nil && *e.Text != "":
			out = append(out, "text "+*e.Text)
		case len(e.Meta.RoundMessages) > 0:
			for j := range e.Meta.RoundMessages {
				out = append(out, "round "+describeMessage(&e.Meta.RoundMessages[j]))
			}
		case e.Message != nil:
			out = append(out, "message "+describeMessage(e.Message))
		}
	}
	return out
}

func describeMessage(m *types.Message) string {
	s := m.Role
	if m.Content != "" && m.ToolResult == nil {
		s += " " + m.Content
	}
	for _, tc := range m.ToolCalls {
		s += " call " + tc.ID + " " + tc.Name + " " + string(tc.Args)
	}
	if m.ToolResult != nil {
		s += " result " + m.ToolResult.ID + " " + m.ToolResult.GetTextContent()
	}
	return s
}

// A streamed tool loop reports each tool round as it happens: the call once
// the round's reply has streamed, its result once it ran, then the next
// round's text. The transcript still follows as messages at the end.
func TestStreamingToolLoop_ReportsEachRoundAsItHappens(t *testing.T) {
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, &turnScript{turns: []mock.Turn{
		{Type: "tool_calls", StreamChunks: []string{"Let me ", "look."},
			ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "42"}}}},
		{Type: "text", StreamChunks: []string{"Found ", "it."}},
	}})
	reg := mockToolRegistry(t, "lookup")
	turnState := NewTurnState()
	turnState.AllowedTools = []string{"lookup"}
	stg := NewProviderStageWithTurnState(provider, reg, &pipeline.ToolPolicy{}, &ProviderConfig{}, nil, nil, turnState)

	elems, err := runProviderStage(t, stg, "where is 42?")
	require.NoError(t, err)

	const result = "{\n  \"result\": \"mock response\"\n}"
	assert.Equal(t, []string{
		"text Let me ",
		"text look.",
		`round assistant Let me look. call call_0_lookup lookup {"id":"42"}`,
		"round tool result call_0_lookup " + result,
		"text Found ",
		"text it.",
		"message user where is 42?",
		`message assistant Let me look. call call_0_lookup lookup {"id":"42"}`,
		"message tool result call_0_lookup " + result,
		"message assistant Found it.",
	}, describeElements(elems))
}

// A round whose calls end the turn reports its calls, and leaves its results
// to the transcript.
func TestStreamingToolLoop_RoundThatEndsTheTurnReportsOnlyItsCalls(t *testing.T) {
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, &turnScript{turns: []mock.Turn{
		{Type: "tool_calls", StreamChunks: []string{"Checking."},
			ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "1"}}}},
	}})
	reg := mockToolRegistry(t, "lookup")
	turnState := NewTurnState()
	turnState.AllowedTools = []string{"lookup"}
	stg := NewProviderStageWithTurnState(provider, reg, &pipeline.ToolPolicy{MaxRounds: 1},
		&ProviderConfig{}, nil, nil, turnState)

	elems, err := runProviderStage(t, stg, "check")
	var maxRounds *MaxRoundsError
	require.ErrorAs(t, err, &maxRounds, "the round limit ends the turn")

	var rounds []string
	for _, d := range describeElements(elems) {
		if len(d) > 6 && d[:6] == "round " {
			rounds = append(rounds, d)
		}
	}
	assert.Equal(t, []string{`round assistant Checking. call call_0_lookup lookup {"id":"1"}`}, rounds)
}

// The continuous (duplex) mode delimits its turns with its own elements and
// does not report rounds.
func TestStreamingToolLoop_ContinuousModeDoesNotReportRounds(t *testing.T) {
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, &turnScript{turns: []mock.Turn{
		{Type: "tool_calls", StreamChunks: []string{"Let me look."},
			ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "42"}}}},
		{Type: "text", StreamChunks: []string{"Found it."}},
	}})
	turnState := NewTurnState()
	turnState.AllowedTools = []string{"lookup"}
	stg := NewProviderStageWithTurnState(provider, mockToolRegistry(t, "lookup"), &pipeline.ToolPolicy{},
		&ProviderConfig{Streaming: true}, nil, nil, turnState)

	elems, err := runProviderStage(t, stg, "where is 42?")
	require.NoError(t, err)

	described := describeElements(elems)
	assert.Contains(t, described, "message assistant Found it.", "the turn ran")
	for _, d := range described {
		assert.NotContains(t, d, "round ")
	}
}
