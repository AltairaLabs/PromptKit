package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2/session"
)

// openConvDuplex opens a duplex conversation on the mock realtime provider.
func openConvDuplex(t *testing.T) (*Conversation, *sessionConfigSpy) {
	t.Helper()
	prov := &sessionConfigSpy{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi")}
	conv, err := OpenDuplex(createTestPackFile(t, convChatPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation(),
		WithStreamingConfig(&providers.StreamingInputConfig{Config: types.StreamingMediaConfig{
			Type: types.ContentTypeAudio, ChunkSize: 3200, SampleRate: 16000, Channels: 1, BitDepth: 16,
			Encoding: "pcm_linear16"}}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, prov
}

// convSessionChunks waits for the provider's realtime session to receive n
// media chunks and returns them.
func convSessionChunks(t *testing.T, prov *sessionConfigSpy, n int) []*types.MediaChunk {
	t.Helper()
	var got []*types.MediaChunk
	require.Eventually(t, func() bool {
		if s := prov.GetSession(); s != nil {
			got = s.GetChunks()
		}
		return len(got) >= n
	}, 5*time.Second, 5*time.Millisecond, "the realtime session received %d chunks", len(got))
	return got
}

// SendFrame and SendVideoChunk deliver the caller's bytes and MIME type to the
// provider's realtime session.
func TestConv_DuplexFramesAndVideoReachTheSession(t *testing.T) {
	ctx := context.Background()
	conv, prov := openConvDuplex(t)

	require.NoError(t, conv.SendFrame(ctx, &session.ImageFrame{
		Data: []byte("frame-bytes"), MIMEType: "image/jpeg", Width: 640, Height: 480, FrameNum: 1,
		Timestamp: time.Now(),
	}))
	chunks := convSessionChunks(t, prov, 1)
	assert.Equal(t, []byte("frame-bytes"), chunks[0].Data)
	assert.Equal(t, "image/jpeg", chunks[0].Metadata["mime_type"])

	require.NoError(t, conv.SendVideoChunk(ctx, &session.VideoChunk{
		Data: []byte("video-bytes"), MIMEType: "video/h264", ChunkIndex: 0, IsKeyFrame: true,
		Timestamp: time.Now(),
	}))
	chunks = convSessionChunks(t, prov, 2)
	assert.Equal(t, []byte("video-bytes"), chunks[1].Data)
	assert.Equal(t, "video/h264", chunks[1].Metadata["mime_type"])

	// WithStreamingConfig is the session the provider was asked to open, and
	// the rendered system prompt is its instruction.
	cfg := prov.first()
	require.NotNil(t, cfg)
	assert.Equal(t, 16000, cfg.Config.SampleRate)
	assert.Equal(t, "pcm_linear16", cfg.Config.Encoding)
	assert.Equal(t, "You help nobody.", cfg.SystemInstruction)
}

// TriggerStart sends the caller's text to the realtime session so the model
// speaks first.
func TestConv_DuplexTriggerStartSendsTheText(t *testing.T) {
	conv, prov := openConvDuplex(t)
	require.NoError(t, conv.TriggerStart(context.Background(), "Greet the caller"))
	require.Eventually(t, func() bool {
		s := prov.GetSession()
		return s != nil && len(s.GetTexts()) > 0
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"Greet the caller"}, prov.GetSession().GetTexts())
}
