package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// realtimeToolPack is a pack whose prompt can call a lookup tool.
const realtimeToolPack = `{
	"id": "realtime-tool",
	"version": "1.0.0",
	"prompts": {
		"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
			"system_template": "You help.", "tools": ["lookup"]}
	},
	"tools": {
		"lookup": {"name": "lookup", "description": "Look something up",
			"parameters": {"type": "object", "properties": {"q": {"type": "string"}}}}
	}
}`

// createdMessageText renders a message as role:text, a tool call as role:call name.
func createdMessageText(m *types.Message) string {
	if m.ToolResult != nil {
		return m.Role + ":" + m.ToolResult.GetTextContent()
	}
	if len(m.ToolCalls) > 0 {
		return m.Role + ":call " + m.ToolCalls[0].Name
	}
	return m.Role + ":" + m.GetContent()
}

// realtimeToolScript is a realtime model that answers the first user turn
// with a tool call, then the tool result with "the answer", then the second
// user turn with "second reply".
func realtimeToolScript() *mock.StreamingProvider {
	repo := newGateTurnRepo()
	repo.add(1, mock.Turn{Type: "tool_calls", Content: "checking",
		ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"q": "x"}}}})
	repo.add(2, mock.Turn{Type: "text", Content: "the answer"})
	repo.add(3, mock.Turn{Type: "text", Content: "second reply"})
	return mock.NewStreamingProvider("rt", "rt-model", false).
		WithAutoRespond("unused").
		WithMockResponses(repo, "default", "")
}

// realtimeTurn sends text and reads the responses until the chunk that
// completes the assistant turn.
func realtimeTurn(t *testing.T, conv *Conversation, ch <-chan providers.StreamChunk, text string) {
	t.Helper()
	require.NoError(t, conv.SendText(context.Background(), text))
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case c, ok := <-ch:
			require.True(t, ok, "response channel closed during the turn for %q", text)
			require.NoError(t, c.Error)
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				return
			}
		case <-timer.C:
			t.Fatalf("no completed assistant turn for %q", text)
		}
	}
}

// A realtime conversation's assistant replies reach the event bus as
// message.created, as a text conversation's do: the user turn, the tool-call
// turn, the tool result and the reply, each once, at the index each holds in
// the persisted transcript.
func TestDuplexRealtime_BroadcastsEveryMessageAtItsTranscriptIndex(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	var mu sync.Mutex
	published := map[int]string{}
	var dup []int
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		d, ok := e.Data.(*events.MessageCreatedData)
		if !ok {
			return
		}
		text := d.Role + ":" + d.GetContent()
		if len(d.ToolCalls) > 0 {
			text = d.Role + ":call " + d.ToolCalls[0].Name
		}
		mu.Lock()
		defer mu.Unlock()
		if _, seen := published[d.Index]; seen {
			dup = append(dup, d.Index)
		}
		published[d.Index] = text
	})
	store := statestore.NewMemoryStore()
	conv, err := OpenDuplex(createTestPackFile(t, realtimeToolPack), "chat",
		WithProvider(realtimeToolScript()), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithEventBus(bus), WithStateStore(store), WithConversationID("rt-bus"))
	require.NoError(t, err)
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "found", nil })
	ch, err := conv.Response()
	require.NoError(t, err)

	realtimeTurn(t, conv, ch, "first")
	realtimeTurn(t, conv, ch, "second")

	want := map[int]string{
		0: "user:first",
		1: "assistant:call lookup",
		2: "tool:found",
		3: "assistant:the answer",
		4: "user:second",
		5: "assistant:second reply",
	}
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(published) >= len(want)
	}, 5*time.Second, 5*time.Millisecond)
	require.NoError(t, conv.Close())
	bus.Close() // delivers any stray publish, so an extra event is caught

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, want, published)
	assert.Empty(t, dup, "a message was published more than once")

	state, err := store.Load(context.Background(), "rt-bus")
	require.NoError(t, err)
	persisted := map[int]string{}
	for i := range state.Messages {
		persisted[i] = createdMessageText(&state.Messages[i])
	}
	assert.Equal(t, want, persisted, "the bus indices are the persisted transcript's")
}
