package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// These tests check the conversation a realtime session stores for the tool
// calls the pipeline runs, through the state store, and that a text provider
// resuming it with Resume is sent every call followed by its result.

// toolExchangeCall is an open realtime conversation over a scripted session,
// saving to a state store, whose lookup_order tool runs until release closes
// or the call is canceled.
type toolExchangeCall struct {
	t       *testing.T
	conv    *sdk.Conversation
	sess    *heardReplySession
	store   *statestore.MemoryStore
	pack    string
	running chan struct{}
	release chan struct{}
}

func openToolExchangeCall(t *testing.T) *toolExchangeCall {
	t.Helper()
	sess := &heardReplySession{MockStreamSession: mock.NewMockStreamSession(),
		toolResponses: make(chan []providers.ToolResponse, 4)}
	prov := &heardReplyProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false), sess: sess}
	store := statestore.NewMemoryStore()
	conv, _ := openRecordedDuplex(t, lateTranscriptPackJSON, prov, sdk.WithStateStore(store))
	c := &toolExchangeCall{t: t, conv: conv, sess: sess, store: store, pack: writePackFile(t, lateTranscriptPackJSON),
		running: make(chan struct{}, 4), release: make(chan struct{})}
	conv.OnToolCtx("lookup_order", func(ctx context.Context, _ map[string]any) (any, error) {
		c.running <- struct{}{}
		select {
		case <-c.release:
			return map[string]any{"status": "shipped"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	ch, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for range ch { //nolint:revive // drained so the session never blocks
		}
	}()
	return c
}

// ask sends the caller's typed turn and waits for the session to get it.
func (c *toolExchangeCall) ask(text string) {
	c.t.Helper()
	n := len(c.sess.GetTexts())
	require.NoError(c.t, c.conv.SendText(context.Background(), text))
	require.Eventually(c.t, func() bool { return len(c.sess.GetTexts()) > n }, 2*time.Second, 2*time.Millisecond)
}

// callTool ends the reply with a call to lookup_order and waits for the tool
// to start running.
func (c *toolExchangeCall) callTool(id string) {
	c.t.Helper()
	reason := "tool_calls"
	c.sess.EmitChunk(&providers.StreamChunk{FinishReason: &reason, ToolCalls: []types.MessageToolCall{
		{ID: id, Name: "lookup_order", Args: json.RawMessage(`{"order":"7"}`)}}})
	select {
	case <-c.running:
	case <-time.After(3 * time.Second):
		c.t.Fatal("the tool never ran")
	}
}

// saved waits until the store holds n messages and returns them.
func (c *toolExchangeCall) saved(n int) []types.Message {
	c.t.Helper()
	var msgs []types.Message
	require.Eventually(c.t, func() bool {
		state, err := c.store.Load(context.Background(), c.conv.ID())
		if err != nil || state == nil {
			return false
		}
		msgs = state.Messages
		return len(msgs) >= n
	}, 5*time.Second, 5*time.Millisecond, "the store never held %d messages", n)
	return msgs
}

// resume resumes the stored conversation on prov, a text provider.
func (c *toolExchangeCall) resume(prov providers.Provider) *sdk.Conversation {
	c.t.Helper()
	resumed, err := sdk.Resume(c.conv.ID(), c.pack, "chat", sdk.WithProvider(prov), sdk.WithSkipSchemaValidation(),
		sdk.WithStateStore(c.store))
	require.NoError(c.t, err)
	c.t.Cleanup(func() { _ = resumed.Close() })
	return resumed
}

// resumeRequest resumes the stored conversation on a text provider, sends
// "thanks", and returns the messages of the provider request, rendered as
// role and content.
func (c *toolExchangeCall) resumeRequest() []string {
	c.t.Helper()
	prov := &requestRecordingProvider{Provider: mock.NewProvider("mock", "mock-model", false)}
	_, err := c.resume(prov).Send(context.Background(), "thanks")
	require.NoError(c.t, err)
	return renderRequest(prov.firstRequest())
}

// renderRequest renders messages as role and content, a call as its ID and a
// result as its call's ID and text.
func renderRequest(msgs []types.Message) []string {
	var out []string
	for _, m := range msgs {
		switch {
		case len(m.ToolCalls) > 0:
			out = append(out, m.Role+":calls "+m.ToolCalls[0].ID)
		case m.ToolResult != nil:
			text := m.ToolResult.Error
			if text == "" {
				text = m.ToolResult.GetTextContent()
			}
			out = append(out, m.Role+":result "+m.ToolResult.ID+" "+text)
		default:
			out = append(out, m.Role+":"+m.GetContent())
		}
	}
	return out
}

// Closing the conversation while a tool runs stores the call with a result:
// the tool is canceled, and the cancellation is the call's result. Resume
// sends the call and its result together.
func TestDuplexToolExchange_CloseDuringRoundIsResumable(t *testing.T) {
	c := openToolExchangeCall(t)
	c.ask("where is order 7?")
	c.callTool("call_1")
	require.NoError(t, c.conv.Close())

	stored := renderRequest(c.saved(3))
	assert.Equal(t, []string{"user:where is order 7?", "assistant:calls call_1",
		"tool:result call_1 context canceled"}, stored)
	assert.Equal(t, append(stored, "user:thanks"), c.resumeRequest())
}

// A typed turn sent while a tool runs is stored after the tool's result, so
// Resume sends the call followed by its result.
func TestDuplexToolExchange_TurnDuringRoundIsStoredAfterTheResult(t *testing.T) {
	c := openToolExchangeCall(t)
	c.ask("where is order 7?")
	c.callTool("call_1")
	c.ask("and order 8?")
	close(c.release)
	select {
	case <-c.sess.toolResponses:
	case <-time.After(3 * time.Second):
		t.Fatal("the tool's result never reached the session")
	}
	reason := "stop"
	c.sess.EmitChunk(&providers.StreamChunk{Content: "Both have shipped.", FinishReason: &reason})
	c.saved(5)
	require.NoError(t, c.conv.Close())

	assert.Equal(t, []string{
		"user:where is order 7?", "assistant:calls call_1", "tool:result call_1 {\n  \"status\": \"shipped\"\n}",
		"user:and order 8?", "assistant:Both have shipped.", "user:thanks",
	}, c.resumeRequest())
}
