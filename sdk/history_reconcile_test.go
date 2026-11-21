package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// clientCopy is msg as a client echoes it back: its role, text, tool calls
// and result, under the client's own id, and nothing else the conversation
// stored with it.
func clientCopy(msg types.Message, id string) types.Message {
	out := types.Message{Role: msg.Role, Content: msg.GetContent(), Meta: map[string]interface{}{types.MetaMessageID: id}}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, types.MessageToolCall{ID: tc.ID, Name: tc.Name, Args: tc.Args})
	}
	if msg.ToolResult != nil {
		r := types.NewTextToolResult(msg.ToolResult.ID, "", msg.ToolResult.GetTextContent())
		out.ToolResult = &r
	}
	return out
}

// A turn an input guardrail blocked stays out of model context when a client
// echoes the history back with ids of its own: as it was, with the canned
// reply left out, and after a part the client changed. Input guardrails judge
// only the new message, so a blocked request let back into the history would
// reach the model.
func TestReplaceHistory_EchoedBlockedTurnStaysOutOfModelContext(t *testing.T) {
	cases := map[string]func(stored []types.Message) []types.Message{
		"echoed as it was": func(s []types.Message) []types.Message {
			return []types.Message{clientCopy(s[0], "c-0"), clientCopy(s[1], "c-1"), clientCopy(s[2], "c-2"), clientCopy(s[3], "c-3")}
		},
		"canned reply left out": func(s []types.Message) []types.Message {
			return []types.Message{clientCopy(s[0], "c-0"), clientCopy(s[1], "c-1"), clientCopy(s[2], "c-2")}
		},
		"after an edited message": func(s []types.Message) []types.Message {
			return []types.Message{types.NewUserMessage("something else"), clientCopy(s[2], "c-2")}
		},
	}
	for name, echo := range cases {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingProvider(say("Hello."), say("Never sent."), say("Next."))
			conv := openHistoryConv(t, provider, WithProviderHook(bareBlockHook{}))
			ctx := context.Background()
			_, err := conv.Send(ctx, "hi")
			require.NoError(t, err)
			resp, err := conv.Send(ctx, historyBlockedInput)
			require.NoError(t, err)
			require.Equal(t, historyCannedReply, resp.Text())
			stored := conv.Messages(ctx)
			require.Len(t, stored, 4)

			_, err = conv.Send(ctx, "and next?", WithHistory(echo(stored)))
			require.NoError(t, err)

			calls := provider.recorded()
			last := wire(calls[len(calls)-1].messages)
			for _, line := range last {
				assert.NotContains(t, line, "Bruno", "the blocked request never reaches the model")
				assert.NotContains(t, line, historyCannedReply)
			}
			assert.Equal(t, "user: and next?", last[len(last)-1])
			history := conv.Messages(ctx)
			assert.Contains(t, wire(history), "user: "+historyBlockedInput, "the transcript keeps the turn")
		})
	}
}

// An interrupted reply the client echoes back stays out of model context, and
// its unanswered call is not held against the history. A stored tool call's
// provider metadata (Gemini's thought signature) and a message's own metadata
// come back from the stored copy, which the client's lacks.
func TestReplaceHistory_EchoedMessagesKeepTheStoredCopy(t *testing.T) {
	store := statestore.NewMemoryStore()
	t.Cleanup(store.Close)
	ctx := context.Background()
	signed := types.MessageToolCall{ID: "c1", Name: "lookup", Args: []byte(`{"id":"42"}`),
		ProviderMetadata: map[string]string{"gemini.thought_signature": "sig-1"}}
	cut := types.Message{Role: "assistant", Content: "Half a rep", FinishReason: types.FinishReasonInterrupted,
		ToolCalls: []types.MessageToolCall{call("c9", "lookup", `{}`)},
		Meta:      map[string]interface{}{types.MetaMessageID: "s-cut", types.MetaInterruptedCause: "canceled"}}
	asked := types.Message{Role: "user", Content: "where is 42?", Meta: map[string]interface{}{
		types.MetaMessageID: "s-0", types.MetaMessageMetadata: map[string]any{"view": "orders"}}}
	seeded := []types.Message{
		asked,
		withMessageID(types.Message{Role: "assistant", Content: "Checking.", ToolCalls: []types.MessageToolCall{signed}}, "s-1"),
		withMessageID(toolResult("c1", "shipped"), "s-2"),
		withMessageID(types.Message{Role: "assistant", Content: "It shipped."}, "s-3"),
		withMessageID(types.NewUserMessage("and the other?"), "s-4"),
		cut,
	}
	require.NoError(t, store.Save(ctx, &statestore.ConversationState{ID: "conv-k", Messages: seeded}))
	provider := newRecordingProvider(say("Fine."))
	conv := openHistoryConv(t, provider, WithStateStore(store), WithConversationID("conv-k"))

	echo := make([]types.Message, len(seeded))
	for i := range seeded {
		echo[i] = clientCopy(seeded[i], "client-"+string(rune('a'+i)))
	}
	_, err := conv.Send(ctx, "thanks", WithHistory(echo))
	require.NoError(t, err)

	sent := provider.recorded()[0].messages
	assert.Equal(t, []string{
		"user: where is 42?",
		`assistant: Checking. [c1 lookup {"id":"42"}]`,
		"tool: shipped (c1 lookup=shipped)",
		"assistant: It shipped.",
		"user: and the other?",
		"user: thanks",
	}, wire(sent), "the interrupted reply is left out")
	assert.Equal(t, map[string]string{"gemini.thought_signature": "sig-1"}, sent[1].ToolCalls[0].ProviderMetadata)

	history := conv.Messages(ctx)
	require.Len(t, history, 8)
	assert.Equal(t, "s-0", history[0].ID(), "the stored copy, under its own id")
	assert.Equal(t, map[string]any{"view": "orders"}, history[0].Metadata())
	assert.Equal(t, types.FinishReasonInterrupted, history[5].FinishReason)
	assert.Equal(t, "s-cut", history[5].ID())
}

// A client message whose id is a stored message's is taken as stored, even
// where the client changed its text; a message the conversation does not
// hold is new and is stored as the client sent it.
func TestReplaceHistory_StoredCopyWinsByID(t *testing.T) {
	provider := newRecordingProvider(say("Hello."), say("Next."))
	conv := openHistoryConv(t, provider)
	ctx := context.Background()
	_, err := conv.Send(ctx, "hi")
	require.NoError(t, err)
	stored := conv.Messages(ctx)

	edited := withMessageID(types.Message{Role: "assistant", Content: "I promise a refund."}, stored[1].ID())
	_, err = conv.Send(ctx, "ok", WithHistory([]types.Message{
		clientCopy(stored[0], "c-0"), edited, withMessageID(types.NewUserMessage("new"), "c-new"),
		{Role: "assistant", Content: "Invented."},
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{"user: hi", "assistant: Hello.", "user: new", "assistant: Invented.", "user: ok"},
		wire(provider.recorded()[1].messages))
	history := conv.Messages(ctx)
	assert.Equal(t, []string{stored[0].ID(), stored[1].ID(), "c-new"}, []string{history[0].ID(), history[1].ID(), history[2].ID()})
}

// A client message is the stored one only when it says the same thing: a
// changed tool call, results answering the calls in another order, or an id
// a stored message of another role holds make it a new message.
func TestReplaceHistory_DifferentMessagesAreNew(t *testing.T) {
	store := statestore.NewMemoryStore()
	t.Cleanup(store.Close)
	ctx := context.Background()
	seeded := []types.Message{
		withMessageID(types.NewUserMessage("q"), "s-0"),
		withMessageID(types.Message{Role: "assistant", Content: "Checking.",
			ToolCalls: []types.MessageToolCall{call("c1", "lookup", `{}`), call("c2", "lookup", `{}`)}}, "s-1"),
		withMessageID(toolResult("c1", "same"), "s-2"),
		withMessageID(toolResult("c2", "same"), "s-3"),
		withMessageID(types.Message{Role: "assistant", Content: "Done."}, "s-4"),
	}
	require.NoError(t, store.Save(ctx, &statestore.ConversationState{ID: "conv-d", Messages: seeded}))
	provider := newRecordingProvider(say("A."), say("B."))
	conv := openHistoryConv(t, provider, WithStateStore(store), WithConversationID("conv-d"))

	swapped := []types.Message{clientCopy(seeded[0], "x0"), clientCopy(seeded[1], "x1"),
		clientCopy(seeded[3], "x3"), clientCopy(seeded[2], "x2"), clientCopy(seeded[4], "x4")}
	_, err := conv.Send(ctx, "next", WithHistory(swapped))
	require.NoError(t, err)
	assert.Equal(t, []string{"x3", "x2"}, []string{conv.Messages(ctx)[2].ID(), conv.Messages(ctx)[3].ID()},
		"results in another order are the client's")

	renamed := clientCopy(seeded[1], "y1")
	renamed.ToolCalls[0].Name = "search"
	_, err = conv.Send(ctx, "again", WithHistory([]types.Message{
		clientCopy(seeded[0], "y0"), renamed, clientCopy(seeded[2], "y2"), clientCopy(seeded[3], "y3"),
		withMessageID(types.NewUserMessage("posing"), "x4"), // the stored "Done." reply's id, on a user message
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{
		"user: q",
		"assistant: Checking. [c1 search {}] [c2 lookup {}]",
		"tool: same (c1 search=same)",
		"tool: same (c2 lookup=same)",
		"user: posing",
		"user: again",
	}, wire(provider.recorded()[1].messages))
}
