package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// convDuplexAudio is the streaming config the resume tests open sessions with.
var convDuplexAudio = &providers.StreamingInputConfig{Config: types.StreamingMediaConfig{
	Type: types.ContentTypeAudio, ChunkSize: 3200, SampleRate: 16000, Channels: 1, BitDepth: 16,
	Encoding: "pcm_linear16"}}

// convPersisted saves a conversation the way a previous process left it: a
// user turn with its text in Parts, the reply, and a tool exchange.
func convPersisted(t *testing.T, store statestore.Store, id string) {
	t.Helper()
	user := types.Message{Role: "user"}
	user.AddTextPart("my name is Ada")
	toolCall := types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "lookup"}}}
	toolResult := types.NewToolResultMessage(types.NewTextToolResult("c1", "lookup", "found"))
	msgs := []types.Message{user, {Role: "assistant", Content: "hi Ada"}, toolCall, toolResult,
		{Role: "assistant", Content: "anything else?"}}
	require.NoError(t, store.(statestore.BulkWriter).Save(context.Background(),
		&statestore.ConversationState{ID: id, Messages: msgs}))
}

// ResumeDuplex gives the new realtime session the persisted conversation
// before the caller's first input: the user and assistant turns with text, in
// order, and nothing else. The caller's input then goes to the session as
// usual.
func TestConv_ResumeDuplexSeedsTheRealtimeSession(t *testing.T) {
	store := statestore.NewMemoryStore()
	convPersisted(t, store, "voice-1")
	prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi")
	conv, err := ResumeDuplex("voice-1", createTestPackFile(t, convChatPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation(), WithStateStore(store), WithStreamingConfig(convDuplexAudio))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	require.NoError(t, conv.SendText(context.Background(), "what is my name"))
	require.Eventually(t, func() bool {
		s := prov.GetSession()
		return s != nil && len(s.GetTexts()) > 0
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []providers.HistoryTurn{
		{Role: "user", Text: "my name is Ada"},
		{Role: "assistant", Text: "hi Ada"},
		{Role: "assistant", Text: "anything else?"},
	}, prov.GetSession().GetHistory())
	assert.Equal(t, []string{"what is my name"}, prov.GetSession().GetTexts())
}

// A conversation opened fresh gives its session no history.
func TestConv_OpenDuplexSeedsNoHistory(t *testing.T) {
	prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi")
	conv, err := OpenDuplex(createTestPackFile(t, convChatPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	require.NoError(t, conv.SendText(context.Background(), "hello"))
	require.Eventually(t, func() bool {
		s := prov.GetSession()
		return s != nil && len(s.GetTexts()) > 0
	}, 5*time.Second, 5*time.Millisecond)
	assert.Empty(t, prov.GetSession().GetHistory())
}

// convNoHistoryProvider is a realtime provider whose sessions cannot take
// prior conversation.
type convNoHistoryProvider struct{ mockStreamingProvider }

// ResumeDuplex refuses, with an error a caller can test, to resume a realtime
// conversation on a provider that cannot replay its history, instead of
// silently starting fresh. History with no text to replay needs nothing.
func TestConv_ResumeDuplexRefusesAProviderWithoutHistory(t *testing.T) {
	pack := createTestPackFile(t, convChatPack)
	store := statestore.NewMemoryStore()
	convPersisted(t, store, "voice-2")
	_, err := ResumeDuplex("voice-2", pack, "chat", WithProvider(&convNoHistoryProvider{}),
		WithSkipSchemaValidation(), WithStateStore(store), WithStreamingConfig(convDuplexAudio))
	require.ErrorIs(t, err, ErrResumeHistoryUnsupported)
	assert.Contains(t, err.Error(), `provider "mock"`)

	toolOnly := types.NewToolResultMessage(types.NewTextToolResult("c1", "lookup", "found"))
	require.NoError(t, store.Save(context.Background(),
		&statestore.ConversationState{ID: "voice-3", Messages: []types.Message{toolOnly}}))
	conv, err := ResumeDuplex("voice-3", pack, "chat", WithProvider(&convNoHistoryProvider{}),
		WithSkipSchemaValidation(), WithStateStore(store), WithStreamingConfig(convDuplexAudio))
	require.NoError(t, err, "nothing to replay, nothing to refuse")
	_ = conv.Close()
}
