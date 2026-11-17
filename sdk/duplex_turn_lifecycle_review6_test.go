package sdk

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// A turn runs its lifecycle once it is stored, not once its output reaches
// the caller: a reply whose audio is still being paced out when the
// conversation closes has its session update run before the session ends.
func TestDuplexRealtime_StoredTurnRunsThoughItsOutputIsStillPaced(t *testing.T) {
	stop := "stop"
	speech := make([]byte, 24000*2*5) // five seconds of 24kHz PCM16, paced out in real time
	prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("unused").WithResponseChunks(
		providers.StreamChunk{MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: speech,
			SampleRate: 24000, Channels: 1}},
		providers.StreamChunk{Content: "hi", Delta: "hi", FinishReason: &stop},
	)
	store := statestore.NewMemoryStore()
	order := &lifecycleOrder{}
	sess := &fakeAudioSession{sinks: []audio.Sink{audio.NewMemSink(audio.KindAudio)}}
	conv, err := OpenDuplex(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithAudioSession(sess), WithStateStore(store), WithConversationID("paced"), WithSessionHook(order))
	require.NoError(t, err)
	convDrain(t, conv)

	require.NoError(t, conv.SendText(context.Background(), "hello"))
	require.Eventually(t, func() bool { return len(storedReplies(t, store, "paced")) == 1 },
		5*time.Second, time.Millisecond, "the reply is stored")
	require.NoError(t, closeWithin(t, conv)) // its audio is still being paced out

	assert.Equal(t, []string{"start", "update", "end"}, kinds(order.snapshot()))
}

// sendingHook sends text into the conversation from its first OnSessionUpdate.
type sendingHook struct {
	conv    atomic.Pointer[Conversation]
	once    sync.Once
	sent    chan error
	updates atomic.Int32
}

func (h *sendingHook) Name() string                                             { return "sending" }
func (h *sendingHook) OnSessionStart(context.Context, hooks.SessionEvent) error { return nil }
func (h *sendingHook) OnSessionEnd(context.Context, hooks.SessionEvent) error   { return nil }
func (h *sendingHook) OnSessionUpdate(ctx context.Context, _ hooks.SessionEvent) error {
	h.updates.Add(1)
	h.once.Do(func() { h.sent <- h.conv.Load().SendText(ctx, "and another thing") })
	return nil
}

// A session hook may send into a VAD conversation: the lifecycle runs off the
// session's output path, so the send does not deadlock, and the text it sent
// is a turn of its own.
func TestDuplexVAD_HookSendingTextDoesNotDeadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	hook := &sendingHook{sent: make(chan error, 1)}
	conv, err := OpenDuplex(createTestPackFile(t, duplexOrderPack), "chat",
		WithProvider(&turnRecordingProvider{}), WithSkipSchemaValidation(),
		WithVADMode(newScriptedSTT("one"), newConvMockTTSService(), vadTestConfig()),
		WithSessionHook(hook))
	require.NoError(t, err)
	hook.conv.Store(conv)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	convDrain(t, conv)

	speakOneUtterance(t, conv)
	select {
	case err := <-hook.sent:
		require.NoError(t, err)
	case <-time.After(8 * time.Second):
		t.Fatal("deadlock: the hook's SendText never returned")
	}
	require.Eventually(t, func() bool { return hook.updates.Load() == 2 }, 8*time.Second, 5*time.Millisecond,
		"the text the hook sent is a turn of its own")
}
