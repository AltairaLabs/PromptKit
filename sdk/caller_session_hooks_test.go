package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callerHook records the caller on the session start and end events.
type callerHook struct {
	mu         sync.Mutex
	start, end *types.Caller
}

func (h *callerHook) Name() string { return "caller-recorder" }
func (h *callerHook) OnSessionStart(_ context.Context, e hooks.SessionEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := e.Caller
	h.start = &c
	return nil
}
func (h *callerHook) OnSessionUpdate(context.Context, hooks.SessionEvent) error { return nil }
func (h *callerHook) OnSessionEnd(_ context.Context, e hooks.SessionEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := e.Caller
	h.end = &c
	return nil
}

func (h *callerHook) seen() (start, end *types.Caller) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.start, h.end
}

func TestCaller_SessionHooksSeeTheCaller(t *testing.T) {
	member := types.Caller{ID: "member@example.com", Claims: map[string]any{"tenant": "acme"}}
	cases := []struct {
		name string
		opts []Option
		want types.Caller
	}{
		{"set", []Option{WithCaller(member)}, member},
		{"unset", nil, types.Caller{}},
	}
	for _, tc := range cases {
		t.Run("unary/"+tc.name, func(t *testing.T) {
			hook := &callerHook{}
			opts := append([]Option{WithProvider(mock.NewProvider("mock", "mock-model", false)),
				WithSkipSchemaValidation(), WithSessionHook(hook)}, tc.opts...)
			conv, err := Open(createTestPackFile(t, convChatPack), "chat", opts...)
			require.NoError(t, err)
			_, err = conv.Send(context.Background(), "hello")
			require.NoError(t, err)
			require.NoError(t, conv.Close())

			start, end := hook.seen()
			require.NotNil(t, start)
			require.NotNil(t, end)
			assert.Equal(t, tc.want, *start)
			assert.Equal(t, tc.want, *end)
		})
		t.Run("duplex/"+tc.name, func(t *testing.T) {
			hook := &callerHook{}
			prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi")
			opts := append([]Option{WithProvider(prov), WithSkipSchemaValidation(),
				WithStreamingConfig(convDuplexAudio), WithSessionHook(hook)}, tc.opts...)
			conv, err := OpenDuplex(createTestPackFile(t, convChatPack), "chat", opts...)
			require.NoError(t, err)
			require.NoError(t, conv.SendText(context.Background(), "hello"))
			require.Eventually(t, func() bool {
				s := prov.GetSession()
				return s != nil && len(s.GetTexts()) > 0
			}, 5*time.Second, 5*time.Millisecond)
			require.NoError(t, conv.Close())

			start, end := hook.seen()
			require.NotNil(t, start)
			require.NotNil(t, end)
			assert.Equal(t, tc.want, *start)
			assert.Equal(t, tc.want, *end)
		})
	}
}

// A fork acts for the same caller as the conversation it came from.
func TestCaller_ForkCarriesTheCaller(t *testing.T) {
	member := types.Caller{ID: "member@example.com"}
	conv, err := Open(createTestPackFile(t, convChatPack), "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(), WithCaller(member))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	_, err = conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	fork, err := conv.Fork()
	require.NoError(t, err)
	t.Cleanup(func() { _ = fork.Close() })
	assert.Equal(t, member, fork.sessionHooks.buildEvent().Caller)
}
