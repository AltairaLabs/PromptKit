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
	md := map[string]any{"selection": []string{"s1", "s2", "s3"}, "view": "canvas"}

	_, err := conv.Send(context.Background(), "align these", WithMessageMetadata(md))
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

// A caller may share one Meta map across messages; this turn's metadata must
// not leak into the others through it.
func TestWithMessageMetadata_DoesNotWriteIntoTheCallersMetaMap(t *testing.T) {
	conv, _ := openMessageMetadataConv(t, mock.NewProvider("mock", "m", false))
	shared := map[string]interface{}{"own": "kept"}
	msg := &types.Message{Role: "user", Meta: shared}
	msg.AddTextPart("hello")

	_, err := conv.Send(context.Background(), msg, WithMessageMetadata(map[string]any{"k": "v"}))
	require.NoError(t, err)

	assert.Equal(t, map[string]interface{}{"own": "kept"}, shared, "the caller's Meta map was written into")
	stored := userMessage(t, conv)
	assert.Equal(t, "kept", stored.Meta["own"])
	assert.Equal(t, map[string]any{"k": "v"}, stored.Metadata())
}

func TestWithMessageMetadata_MergesRepeatedOptions(t *testing.T) {
	cfg, err := parseSendOptions([]SendOption{
		WithMessageMetadata(map[string]any{"a": 1, "b": 1}),
		WithMessageMetadata(map[string]any{"b": 2}),
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": 1, "b": 2}, cfg.messageMetadata)
}

func TestWithMessageMetadata_CopiesTheCallersMap(t *testing.T) {
	md := map[string]any{"a": 1}
	cfg, err := parseSendOptions([]SendOption{WithMessageMetadata(md)})
	require.NoError(t, err)
	md["a"] = 2
	assert.Equal(t, 1, cfg.messageMetadata["a"])
}

// Empty metadata attaches nothing: the message keeps exactly the Meta it had,
// with no empty metadata key added.
func TestWithMessageMetadata_EmptyIsANoOp(t *testing.T) {
	cfg, err := parseSendOptions([]SendOption{
		WithMessageMetadata(nil), WithMessageMetadata(map[string]any{}),
	})
	require.NoError(t, err)

	own := map[string]interface{}{"own": "kept"}
	msg := &types.Message{Role: "user", Meta: own}
	applyMessageMetadata(msg, cfg.messageMetadata)
	assert.Equal(t, map[string]interface{}{"own": "kept"}, msg.Meta)
	assert.NotContains(t, msg.Meta, types.MetaMessageMetadata)
}

func TestWithMessageMetadata_RejectsWhatCannotBeStored(t *testing.T) {
	_, err := parseSendOptions([]SendOption{WithMessageMetadata(map[string]any{"ch": make(chan int)})})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON-serializable")
}

func TestApplyMessageMetadata_MergesOverExisting(t *testing.T) {
	msg := &types.Message{Meta: map[string]interface{}{
		types.MetaMessageMetadata: map[string]any{"a": 1, "b": 1},
	}}
	applyMessageMetadata(msg, map[string]any{"b": 2})
	assert.Equal(t, map[string]any{"a": 1, "b": 2}, msg.Metadata())
}

// The pack decides how metadata reaches the model: it references the keys as
// template variables, with defaults for turns that send none.
const messageMetadataTemplatePack = `{
	"id": "message-metadata-template-test",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "Selected: {{selection}}. View: {{view}}.",
			"variables": [
				{"name": "selection", "type": "string", "required": false, "default": "none"},
				{"name": "view", "type": "string", "required": false, "default": "list"}
			]
		}
	}
}`

func openMessageMetadataTemplateConv(t *testing.T) (*Conversation, *turnRecordingProvider) {
	t.Helper()
	provider := &turnRecordingProvider{}
	conv, err := Open(writeWorkflowTestPack(t, messageMetadataTemplatePack), "chat",
		WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, provider
}

func TestWithMessageMetadata_TemplateRendersItForThatTurnOnly(t *testing.T) {
	conv, provider := openMessageMetadataTemplateConv(t)

	_, err := conv.Send(context.Background(), "align these", WithMessageMetadata(map[string]any{
		"selection": []string{"s1", "s2"},
		"view":      "canvas",
	}))
	require.NoError(t, err)
	_, err = conv.Send(context.Background(), "and now?")
	require.NoError(t, err)

	require.Equal(t, 2, provider.turnCount())
	assert.Equal(t, `Selected: ["s1","s2"]. View: canvas.`, provider.systemAt(0),
		"a string binds as is; anything else as its JSON")
	assert.Equal(t, "Selected: none. View: list.", provider.systemAt(1),
		"the next turn sent no metadata, so the pack's defaults render")
}

func TestWithMessageMetadata_StreamRendersItInTheTemplate(t *testing.T) {
	conv, provider := openMessageMetadataTemplateConv(t)

	for chunk := range conv.Stream(context.Background(), "align",
		WithMessageMetadata(map[string]any{"view": "canvas"})) {
		require.NoError(t, chunk.Error)
	}

	assert.Equal(t, "Selected: none. View: canvas.", provider.systemAt(0))
}

func TestWithMessageMetadata_JSONInputWinsANameClash(t *testing.T) {
	conv, provider := openMessageMetadataTemplateConv(t)

	_, err := conv.Send(context.Background(), "go",
		WithMessageMetadata(map[string]any{"view": "from-metadata", "selection": "s1"}),
		WithJSONInput(map[string]any{"view": "from-json-input"}))
	require.NoError(t, err)

	assert.Equal(t, "Selected: s1. View: from-json-input.", provider.systemAt(0))
}
