package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const messageMetadataTestPack = `{
	"id": "message-metadata-test",
	"version": "1.0.0",
	"prompts": {
		"chat": {"id": "chat", "name": "Chat", "system_template": "You are helpful."}
	}
}`

// userMessageEvents collects the user-role message.created payloads off a bus.
type userMessageEvents struct {
	mu   sync.Mutex
	data []*events.MessageCreatedData
}

func (u *userMessageEvents) onEvent(e *events.Event) {
	d, ok := e.Data.(*events.MessageCreatedData)
	if !ok || d.Role != "user" {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.data = append(u.data, d)
}

func (u *userMessageEvents) first() *events.MessageCreatedData {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.data) == 0 {
		return nil
	}
	return u.data[0]
}

func openMessageMetadataConv(t *testing.T, provider any) (*Conversation, *userMessageEvents) {
	t.Helper()
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	seen := &userMessageEvents{}
	bus.Subscribe(events.EventMessageCreated, seen.onEvent)

	opts := []Option{WithSkipSchemaValidation(), WithEventBus(bus)}
	switch p := provider.(type) {
	case *mock.Provider:
		opts = append(opts, WithProvider(p))
	case *mock.StreamingProvider:
		opts = append(opts, WithProvider(p))
	}
	conv, err := Open(writeWorkflowTestPack(t, messageMetadataTestPack), "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, seen
}

func userMessage(t *testing.T, conv *Conversation) *types.Message {
	t.Helper()
	for _, m := range conv.Messages(context.Background()) {
		if m.Role == "user" {
			return &m
		}
	}
	t.Fatal("no user message in the conversation history")
	return nil
}

func TestWithMessageMetadata_SendStoresItWithTheUserMessage(t *testing.T) {
	conv, seen := openMessageMetadataConv(t, mock.NewProvider("mock", "m", false))
	// Stored in decoded JSON form: the []string reads back as []any.
	md := map[string]any{"selection": []any{"s1", "s2", "s3"}, "view": "canvas"}

	_, err := conv.Send(context.Background(), "align these", WithMessageMetadata(map[string]any{
		"selection": []string{"s1", "s2", "s3"}, "view": "canvas",
	}))
	require.NoError(t, err)

	assert.Equal(t, md, userMessage(t, conv).Metadata(), "message history")

	require.Eventually(t, func() bool { return seen.first() != nil }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, md, seen.first().Metadata, "message.created event")
}

func TestWithMessageMetadata_StreamStoresItWithTheUserMessage(t *testing.T) {
	conv, seen := openMessageMetadataConv(t, mock.NewStreamingProvider("mock", "m", false))
	md := map[string]any{"selection": "s1"}

	for chunk := range conv.Stream(context.Background(), "align this", WithMessageMetadata(md)) {
		require.NoError(t, chunk.Error)
	}

	assert.Equal(t, md, userMessage(t, conv).Metadata(), "message history")
	require.Eventually(t, func() bool { return seen.first() != nil }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, md, seen.first().Metadata, "message.created event")
}

func TestWithMessageMetadata_OnlyTheTurnItWasSentWith(t *testing.T) {
	conv, _ := openMessageMetadataConv(t, mock.NewProvider("mock", "m", false))

	_, err := conv.Send(context.Background(), "first", WithMessageMetadata(map[string]any{"turn": "one"}))
	require.NoError(t, err)
	_, err = conv.Send(context.Background(), "second")
	require.NoError(t, err)

	var users []*types.Message
	for _, m := range conv.Messages(context.Background()) {
		if m.Role == "user" {
			users = append(users, &m)
		}
	}
	require.Len(t, users, 2)
	assert.Equal(t, map[string]any{"turn": "one"}, users[0].Metadata())
	assert.Nil(t, users[1].Metadata())
}

// A *types.Message passed to Send is the caller's: the metadata is attached to
// a copy, so the caller's message and its Meta map are left as they were.
func TestWithMessageMetadata_LeavesTheCallersMessageAlone(t *testing.T) {
	conv, _ := openMessageMetadataConv(t, mock.NewProvider("mock", "m", false))
	shared := map[string]interface{}{"own": "kept"}
	msg := &types.Message{Role: "user", Meta: shared}
	msg.AddTextPart("hello")

	_, err := conv.Send(context.Background(), msg, WithMessageMetadata(map[string]any{"k": "v"}))
	require.NoError(t, err)

	assert.Equal(t, map[string]interface{}{"own": "kept"}, shared, "the caller's Meta map was written into")
	assert.Nil(t, msg.Metadata(), "the caller's message gained this turn's metadata")
	stored := userMessage(t, conv)
	assert.Equal(t, "kept", stored.Meta["own"])
	assert.Equal(t, map[string]any{"k": "v"}, stored.Metadata())
}

// A caller that reuses one *types.Message across turns must not have one
// turn's metadata attributed to the next.
func TestWithMessageMetadata_ReusedMessageDoesNotAccumulate(t *testing.T) {
	conv, _ := openMessageMetadataConv(t, mock.NewProvider("mock", "m", false))
	msg := &types.Message{Role: "user"}
	msg.AddTextPart("again")

	_, err := conv.Send(context.Background(), msg, WithMessageMetadata(map[string]any{"sel": "a"}))
	require.NoError(t, err)
	_, err = conv.Send(context.Background(), msg, WithMessageMetadata(map[string]any{"view": "x"}))
	require.NoError(t, err)

	var got []map[string]any
	for _, m := range conv.Messages(context.Background()) {
		if m.Role == "user" {
			got = append(got, m.Metadata())
		}
	}
	assert.Equal(t, []map[string]any{{"sel": "a"}, {"view": "x"}}, got)
}

// Metadata is kept in decoded JSON form, so it shares nothing with the
// caller's values: changing them after Send changes nothing stored.
func TestWithMessageMetadata_SharesNothingWithTheCaller(t *testing.T) {
	conv, _ := openMessageMetadataConv(t, mock.NewProvider("mock", "m", false))
	nested := map[string]string{"x": "1"}

	_, err := conv.Send(context.Background(), "hi", WithMessageMetadata(map[string]any{"pos": nested}))
	require.NoError(t, err)
	nested["x"] = "changed"

	assert.Equal(t, map[string]any{"pos": map[string]any{"x": "1"}}, userMessage(t, conv).Metadata())
}

func TestWithMessageMetadata_MergesRepeatedOptions(t *testing.T) {
	cfg, err := parseSendOptions([]SendOption{
		WithMessageMetadata(map[string]any{"a": 1, "b": 1}),
		WithMessageMetadata(map[string]any{"b": 2}),
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": float64(1), "b": float64(2)}, cfg.messageMetadata)
}

func TestWithMessageMetadata_CopiesTheCallersMap(t *testing.T) {
	md := map[string]any{"a": 1}
	cfg, err := parseSendOptions([]SendOption{WithMessageMetadata(md)})
	require.NoError(t, err)
	md["a"] = 2
	assert.Equal(t, float64(1), cfg.messageMetadata["a"])
}

// Empty metadata attaches nothing: the message keeps exactly the Meta it had,
// with no empty metadata key added.
func TestWithMessageMetadata_EmptyIsANoOp(t *testing.T) {
	cfg, err := parseSendOptions([]SendOption{
		WithMessageMetadata(nil), WithMessageMetadata(map[string]any{}),
	})
	require.NoError(t, err)

	own := map[string]interface{}{"own": "kept"}
	msg := withMessageMetadata(&types.Message{Role: "user", Meta: own}, cfg.messageMetadata)
	assert.Equal(t, map[string]interface{}{"own": "kept"}, msg.Meta)
	assert.NotContains(t, msg.Meta, types.MetaMessageMetadata)
}

func TestWithMessageMetadata_RejectsWhatCannotBeStored(t *testing.T) {
	_, err := parseSendOptions([]SendOption{WithMessageMetadata(map[string]any{"ch": make(chan int)})})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON-serializable")
}

func TestWithMessageMetadata_MergesOverWhatTheMessageCarries(t *testing.T) {
	msg := &types.Message{Meta: map[string]interface{}{
		types.MetaMessageMetadata: map[string]any{"a": 1, "b": 1},
	}}
	got := withMessageMetadata(msg, map[string]any{"b": 2})
	assert.Equal(t, map[string]any{"a": 1, "b": 2}, got.Metadata())
	assert.Equal(t, map[string]any{"a": 1, "b": 1}, msg.Metadata(), "the original was changed")
}
