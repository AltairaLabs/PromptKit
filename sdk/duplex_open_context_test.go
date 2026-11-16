package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// openCtxKey stands in for a host's per-call identity key on the context.
type openCtxKey struct{}

// startCtxHook records the context value its OnSessionStart sees.
type startCtxHook struct{ seen any }

func (h *startCtxHook) Name() string { return "start-ctx" }
func (h *startCtxHook) OnSessionStart(ctx context.Context, _ hooks.SessionEvent) error {
	h.seen = ctx.Value(openCtxKey{})
	return nil
}
func (h *startCtxHook) OnSessionUpdate(context.Context, hooks.SessionEvent) error { return nil }
func (h *startCtxHook) OnSessionEnd(context.Context, hooks.SessionEvent) error    { return nil }

// Every context-taking duplex open hands its context to the session-start
// hooks, as it does to the session's tool calls (#2690).
func TestOpenDuplexContext_SessionStartHookSeesTheContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), openCtxKey{}, "member@example.com")
	streaming := func(t *testing.T) []Option {
		prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi")
		return []Option{WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio)}
	}
	openers := map[string]func(t *testing.T, hook *startCtxHook) (*Conversation, error){
		"OpenDuplexContext": func(t *testing.T, hook *startCtxHook) (*Conversation, error) {
			return OpenDuplexContext(ctx, createTestPackFile(t, convChatPack), "chat",
				append(streaming(t), WithSessionHook(hook))...)
		},
		"ResumeDuplexContext": func(t *testing.T, hook *startCtxHook) (*Conversation, error) {
			store := statestore.NewMemoryStore()
			convPersisted(t, store, "voice-1")
			return ResumeDuplexContext(ctx, "voice-1", createTestPackFile(t, convChatPack), "chat",
				append(streaming(t), WithStateStore(store), WithSessionHook(hook))...)
		},
		"OpenVoiceContext": func(t *testing.T, hook *startCtxHook) (*Conversation, error) {
			sess := &fakeAudioSession{
				sources: []audio.Source{audio.NewMemSource(audio.KindAudio, 1)},
				sinks:   []audio.Sink{audio.NewMemSink(audio.KindAudio)},
			}
			return OpenVoiceContext(ctx, writeIngestionTestPack(t), "main",
				WithProvider(&recordingTextProvider{}), WithSkipSchemaValidation(),
				WithVADMode(&convMockSTTService{}, newConvMockTTSService(), nil),
				WithAudioSession(sess), WithSessionHook(hook))
		},
	}
	for name, open := range openers {
		t.Run(name, func(t *testing.T) {
			hook := &startCtxHook{}
			conv, err := open(t, hook)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			assert.Equal(t, "member@example.com", hook.seen)
		})
	}
}
