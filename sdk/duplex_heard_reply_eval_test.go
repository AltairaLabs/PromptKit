package sdk

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// heardEvalProvider hands out one scripted realtime session.
type heardEvalProvider struct {
	*mock.StreamingProvider
	sess *mock.MockStreamSession
}

func (p *heardEvalProvider) CreateStreamSession(
	context.Context, *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return p.sess, nil
}

// Closing a realtime conversation whose last reply is still playing releases
// that reply into the transcript before the session evals run, so they score
// it.
func TestDuplexHeardReply_SessionEvalsSeeTheHeldReply(t *testing.T) {
	bus, col, registry, _ := newStepEvalHarness(t)
	sess := &stepRecordingHandler{typeName: "sess_stub"}
	registry.Register(sess)
	registry.Register(&stepRecordingHandler{typeName: "conv_stub"})
	ms := mock.NewMockStreamSession()
	conv, err := OpenDuplex(writeWorkflowTestPack(t, lifecyclePackJSON), "chat",
		WithSkipSchemaValidation(),
		WithProvider(&heardEvalProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false), sess: ms}),
		WithStateStore(statestore.NewMemoryStore()),
		WithEvalRegistry(registry),
		WithEventBus(bus),
		WithPlayoutTracking(),
		WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio}}),
	)
	require.NoError(t, err)
	ch, err := conv.Response()
	require.NoError(t, err)
	var turns atomic.Int32
	go func() {
		for c := range ch {
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				turns.Add(1)
			}
		}
	}()

	require.NoError(t, conv.SendText(context.Background(), "tell me a story"))
	require.Eventually(t, func() bool { return len(ms.GetTexts()) > 0 }, 2*time.Second, 2*time.Millisecond)
	ms.EmitChunk(&providers.StreamChunk{Delta: "Once upon a time.", Metadata: map[string]any{"type": "output_transcription"}})
	ms.EmitChunk(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: make([]byte, 5*24000*2), MIMEType: "audio/pcm", SampleRate: 24000, Channels: 1}})
	stop := "stop"
	ms.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
	require.Eventually(t, func() bool { return turns.Load() >= 1 }, 3*time.Second, 2*time.Millisecond)

	require.NoError(t, conv.Close())
	require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerOnSessionComplete)) >= 1 },
		5*time.Second, 5*time.Millisecond, "the session evals never ran")
	seen := sess.transcripts()
	require.Len(t, seen, 1)
	assert.Equal(t, []string{"tell me a story", "Once upon a time."}, seen[0])
}
