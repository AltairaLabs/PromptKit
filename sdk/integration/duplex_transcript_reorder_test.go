package integration

import (
	"context"
	"encoding/json"
	"sync"
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

// These tests script a realtime session the way OpenAI Realtime orders its
// events: the user's transcript is a single final event that can land after
// the reply to it has started, or finished. The conversation reorders it
// (input_transcript_ordering: reorder) and the stored history must read as
// the call went: the user turn, then everything the model did in answer.

// lateTranscriptPackJSON offers one tool.
const lateTranscriptPackJSON = `{
	"id": "late-transcript", "version": "1.0.0", "description": "late transcript reordering",
	"prompts": {"chat": {"id": "chat", "name": "Chat", "system_template": "Be helpful.",
		"tools": ["lookup_order"]}},
	"tools": {"lookup_order": {"name": "lookup_order", "description": "Look up an order",
		"parameters": {"type": "object", "properties": {"order": {"type": "string"}}}}}
}`

// lateTranscriptSession is a mock realtime session the test drives event by
// event. It answers tool calls the stage runs on a channel.
type lateTranscriptSession struct {
	*mock.MockStreamSession
	toolResponses chan []providers.ToolResponse
}

func (s *lateTranscriptSession) SendToolResponse(ctx context.Context, id, result string) error {
	return s.SendToolResponses(ctx, []providers.ToolResponse{{ToolCallID: id, Result: result}})
}

func (s *lateTranscriptSession) SendToolResponses(_ context.Context, rs []providers.ToolResponse) error {
	s.toolResponses <- rs
	return nil
}

// lateTranscriptProvider hands out its one session.
type lateTranscriptProvider struct {
	*mock.StreamingProvider
	sess *lateTranscriptSession
}

func (p *lateTranscriptProvider) CreateStreamSession(
	context.Context, *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return p.sess, nil
}

// lateTranscriptCall is an open conversation over a scripted session.
type lateTranscriptCall struct {
	t     *testing.T
	conv  *sdk.Conversation
	sess  *lateTranscriptSession
	store *statestore.MemoryStore
}

func openLateTranscriptCall(t *testing.T) *lateTranscriptCall {
	t.Helper()
	sess := &lateTranscriptSession{
		MockStreamSession: mock.NewMockStreamSession(),
		toolResponses:     make(chan []providers.ToolResponse, 4),
	}
	prov := &lateTranscriptProvider{
		StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false),
		sess:              sess,
	}
	store := statestore.NewMemoryStore()
	conv, err := sdk.OpenDuplex(writePackFile(t, lateTranscriptPackJSON), "chat",
		sdk.WithProvider(prov),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStateStore(store),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config:   types.StreamingMediaConfig{Type: types.ContentTypeAudio},
			Metadata: map[string]any{"input_transcript_ordering": "reorder"},
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup_order", func(map[string]any) (any, error) {
		return map[string]any{"status": "shipped"}, nil
	})
	drainDuplexResponse(t, conv)
	return &lateTranscriptCall{t: t, conv: conv, sess: sess, store: store}
}

// speak sends one chunk of caller audio and waits for the session to get it,
// so everything the session emits afterwards follows it.
func (c *lateTranscriptCall) speak() {
	c.t.Helper()
	before := len(c.sess.GetChunks())
	require.NoError(c.t, c.conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{Data: make([]byte, 320), MIMEType: "audio/pcm"},
	}))
	require.Eventually(c.t, func() bool { return len(c.sess.GetChunks()) > before },
		2*time.Second, 2*time.Millisecond, "the session never got the caller's audio")
}

// say emits the assistant's spoken words as an output transcript delta.
func (c *lateTranscriptCall) say(text string) {
	c.sess.EmitChunk(&providers.StreamChunk{Delta: text,
		Metadata: map[string]any{"type": "output_transcription"}})
}

// finish ends the assistant's response.
func (c *lateTranscriptCall) finish(reason string, calls ...types.MessageToolCall) {
	c.sess.EmitChunk(&providers.StreamChunk{FinishReason: &reason, ToolCalls: calls})
}

// transcribe emits the user's final transcript, as OpenAI Realtime's
// conversation.item.input_audio_transcription.completed does.
func (c *lateTranscriptCall) transcribe(text string) {
	c.sess.EmitChunk(&providers.StreamChunk{Metadata: map[string]any{
		"type": "input_transcription", "transcription": text, "transcription_final": true,
	}})
}

// awaitToolResponse waits for the stage to answer the model's tool call.
func (c *lateTranscriptCall) awaitToolResponse() []providers.ToolResponse {
	c.t.Helper()
	select {
	case rs := <-c.sess.toolResponses:
		return rs
	case <-time.After(3 * time.Second):
		c.t.Fatal("the session was never given the tool's result")
		return nil
	}
}

// savedEntry is a stored message reduced to what a transcript shows.
type savedEntry struct {
	Role, Content, ToolCallID string
}

// saved waits until the store holds n messages and returns them.
func (c *lateTranscriptCall) saved(n int) []savedEntry {
	c.t.Helper()
	var got []savedEntry
	var mu sync.Mutex
	load := func() bool {
		state, err := c.store.Load(context.Background(), c.conv.ID())
		if err != nil || state == nil {
			return false
		}
		var out []savedEntry
		for i := range state.Messages {
			m := &state.Messages[i]
			e := savedEntry{Role: m.Role, Content: m.GetContent()}
			switch {
			case len(m.ToolCalls) > 0:
				e.ToolCallID = m.ToolCalls[0].ID
			case m.ToolResult != nil:
				e.ToolCallID = m.ToolResult.ID
				e.Content = m.ToolResult.GetTextContent()
			}
			out = append(out, e)
		}
		mu.Lock()
		got = out
		mu.Unlock()
		return len(out) >= n
	}
	assert.Eventually(c.t, load, 5*time.Second, 10*time.Millisecond, "the store never held %d messages", n)
	mu.Lock()
	defer mu.Unlock()
	return got
}

func orderLookupCall(id string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: "lookup_order", Args: json.RawMessage(`{"order":"7"}`)}
}

// A tool round finishes before the late transcript of the user turn that
// asked for it: the tool call and its result are stored after the transcript,
// and the result after the call it answers, then the reply.
func TestDuplexLateTranscript_ToolResultFollowsItsCall(t *testing.T) {
	c := openLateTranscriptCall(t)

	c.speak()
	c.say("Let me check.")
	c.finish("tool_calls", orderLookupCall("call_1"))
	assert.Equal(t, "call_1", c.awaitToolResponse()[0].ToolCallID)
	c.speak() // the caller's microphone keeps streaming
	c.transcribe("Where is order 7?")
	c.say("It has shipped.")
	c.finish("stop")

	assert.Equal(t, []savedEntry{
		{Role: "user", Content: "Where is order 7?"},
		{Role: "assistant", Content: "Let me check.", ToolCallID: "call_1"},
		{Role: "tool", Content: "{\n  \"status\": \"shipped\"\n}", ToolCallID: "call_1"},
		{Role: "assistant", Content: "It has shipped."},
	}, c.saved(4))
}
