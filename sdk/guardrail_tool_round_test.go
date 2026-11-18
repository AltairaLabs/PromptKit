package sdk

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// An output guardrail judges the message it is given. A round whose reply is
// only tool calls has no text to judge: a content check passes it, so its
// calls run, and does not judge earlier turns in its place.

// toolRoundConversation opens a conversation on a model that answers the
// first send with turn1, then calls send_message with no text, then says
// "Sent it.", under guard. It counts the send_message calls.
func toolRoundConversation(t *testing.T, turn1 string, guard Option) (*Conversation, *atomic.Int32) {
	t.Helper()
	repo := &sequenceRepo{turns: []mock.Turn{
		{Type: "text", Content: turn1},
		{Type: "tool_calls",
			ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "hi"}}}},
		{Type: "text", Content: "Sent it."},
	}}
	conv, err := Open(writeGatePack(t), "assist",
		WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)),
		WithSkipSchemaValidation(), guard)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	var calls atomic.Int32
	conv.OnTool("send_message", func(map[string]any) (any, error) {
		calls.Add(1)
		return map[string]any{"sent": true}, nil
	})
	return conv, &calls
}

// sequenceRepo answers the provider's calls with turns, in order.
type sequenceRepo struct {
	turns []mock.Turn
	next  atomic.Int32
}

func (r *sequenceRepo) GetTurn(context.Context, mock.ResponseParams) (*mock.Turn, error) {
	i := int(r.next.Add(1)) - 1
	if i >= len(r.turns) {
		return &mock.Turn{Type: "text"}, nil
	}
	return &r.turns[i], nil
}

func (r *sequenceRepo) GetResponse(ctx context.Context, p mock.ResponseParams) (string, error) {
	t, err := r.GetTurn(ctx, p)
	return t.Content, err
}

// blockedMessages lists the content of each message a guardrail failed.
func blockedMessages(msgs []types.Message) []string {
	var out []string
	for _, m := range msgs {
		for _, v := range m.Validations {
			if !v.Passed {
				out = append(out, m.Role+":"+m.Content)
				break
			}
		}
	}
	return out
}

// An output content_excludes guardrail does not block a tool-call round for
// what an earlier turn said, even when that turn is its own blocked message
// naming what it bans.
func TestGuardrail_ContentExcludesPassesAToolCallRound(t *testing.T) {
	conv, calls := toolRoundConversation(t, "Bruno is my uncle.", WithGuardrail(
		guardrails.Output("content_excludes", map[string]any{"patterns": []any{"Bruno"}},
			guardrails.WithMessage("We don't talk about Bruno."))))
	ctx := context.Background()

	first, err := conv.Send(ctx, "who is that?")
	require.NoError(t, err)
	require.Equal(t, "We don't talk about Bruno.", first.Text(), "the first reply is blocked")

	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)

	assert.Equal(t, int32(1), calls.Load(), "the tool-call round's call runs")
	assert.Equal(t, "Sent it.", resp.Text())
	assert.Equal(t, []string{"assistant:We don't talk about Bruno."}, blockedMessages(conv.Messages(ctx)),
		"only the first reply was blocked")
}

// An output contains_any guardrail does not block a tool-call round for
// lacking the pattern: the round has no text to judge. The reply after it is
// judged as usual.
func TestGuardrail_ContainsAnyPassesAToolCallRound(t *testing.T) {
	conv, calls := toolRoundConversation(t, "Hello.", WithGuardrail(
		guardrails.Output("contains_any", map[string]any{"patterns": []any{"Sent", "Hello"}})))
	ctx := context.Background()

	_, err := conv.Send(ctx, "hi")
	require.NoError(t, err)
	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)

	assert.Equal(t, int32(1), calls.Load(), "the tool-call round's call runs")
	assert.Equal(t, "Sent it.", resp.Text())
	assert.Empty(t, blockedMessages(conv.Messages(ctx)))
}
