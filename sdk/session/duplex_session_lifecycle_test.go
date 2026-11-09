package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// lifecycleWait bounds how long a lifecycle test waits for a channel the
// session should close promptly.
const lifecycleWait = 2 * time.Second

// requireClosedSoon fails unless done closes within lifecycleWait.
func requireClosedSoon(t *testing.T, done <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(lifecycleWait):
		t.Fatal(msg)
	}
}

// drainResponse reads the response channel until the session closes it and
// returns the chunks, failing if it stays open.
func drainResponse(t *testing.T, resp <-chan providers.StreamChunk) []providers.StreamChunk {
	t.Helper()
	var chunks []providers.StreamChunk
	timeout := time.After(lifecycleWait)
	for {
		select {
		case c, ok := <-resp:
			if !ok {
				return chunks
			}
			chunks = append(chunks, c)
		case <-timeout:
			t.Fatal("Response() was never closed")
			return nil
		}
	}
}

func newASMSession(t *testing.T) (DuplexSession, *mock.StreamingProvider) {
	t.Helper()
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        provider,
		Config:          &providers.StreamingInputConfig{},
		PipelineBuilder: testPipelineBuilder,
	})
	require.NoError(t, err)
	return sess, provider
}

// Close ends the session whether or not anything was ever sent: Done fires,
// Response is closed, further sends are refused, and a second Close is a
// no-op. A caller that opens a session and hangs up before speaking must not
// block forever on Done or on ranging over Response.
func TestDuplexSession_CloseEndsTheSession(t *testing.T) {
	for _, sendFirst := range []bool{false, true} {
		name := "never sent"
		if sendFirst {
			name = "after a send"
		}
		t.Run(name, func(t *testing.T) {
			sess, provider := newASMSession(t)
			resp := sess.Response()
			done := sess.Done()
			if sendFirst {
				require.NoError(t, sess.SendText(context.Background(), "hello"))
				require.NotNil(t, waitForSession(provider, lifecycleWait), "test premise: the session started")
			}

			require.NoError(t, sess.Close())
			requireClosedSoon(t, done, "Done() must close once the session is closed")
			drainResponse(t, resp)

			err := sess.SendText(context.Background(), "late")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "session is closed")
			assert.NoError(t, sess.Close(), "a second Close is a no-op")
		})
	}
}

// Drain finishes a started session: the text reached the provider session,
// Done fires and Response closes.
func TestDuplexSession_DrainFinishesAStartedSession(t *testing.T) {
	sess, provider := newASMSession(t)
	resp := sess.Response()
	require.NoError(t, sess.SendText(context.Background(), "hello"))
	mockSession := waitForSession(provider, lifecycleWait)
	require.NotNil(t, mockSession)
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = mockSession.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, sess.Drain(ctx))

	requireClosedSoon(t, sess.Done(), "Done() must close after Drain")
	drainResponse(t, resp)
	assert.Equal(t, []string{"hello"}, mockSession.GetTexts())
}

// In pipeline mode (no streaming config) the session feeds a unary provider
// stage, which answers once input ends: draining the session delivers the
// provider's reply on Response before the channel closes.
func TestDuplexSession_PipelineModeDeliversReplyOnDrain(t *testing.T) {
	provider := mock.NewProvider("mock", "mock-model", false)
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(stage.NewProviderStage(provider, nil, nil, nil)).Build()
	}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		PipelineBuilder: builder,
		Provider:        provider,
	})
	require.NoError(t, err)
	resp := sess.Response()

	require.NoError(t, sess.SendText(context.Background(), "test message"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, sess.Drain(ctx))

	var text string
	for _, c := range drainResponse(t, resp) {
		require.NoError(t, c.Error)
		text += c.Content + c.Delta
	}
	assert.Contains(t, text, "Mock response from mock model mock-model")
}

// Image frames and video chunks reach the provider session with their bytes
// and MIME type intact.
func TestDuplexSession_MediaReachesProviderSession(t *testing.T) {
	t.Run("image frame", func(t *testing.T) {
		sess, provider := newASMSession(t)
		defer sess.Close()
		data := []byte{0xFF, 0xD8, 0xFF, 0xE0}
		require.NoError(t, sess.SendFrame(context.Background(), &ImageFrame{
			Data: data, MIMEType: "image/jpeg", Width: 640, Height: 480, FrameNum: 1, Timestamp: time.Now(),
		}))
		chunk := waitForChunk(t, provider)
		assert.Equal(t, data, chunk.Data)
		assert.Equal(t, "image/jpeg", chunk.Metadata["mime_type"], "metadata: %v", chunk.Metadata)
	})

	t.Run("video chunk", func(t *testing.T) {
		sess, provider := newASMSession(t)
		defer sess.Close()
		data := []byte{0x00, 0x00, 0x00, 0x01}
		require.NoError(t, sess.SendVideoChunk(context.Background(), &VideoChunk{
			Data: data, MIMEType: "video/h264", Width: 1920, Height: 1080,
			IsKeyFrame: true, Timestamp: time.Now(), Duration: 33 * time.Millisecond,
		}))
		chunk := waitForChunk(t, provider)
		assert.Equal(t, data, chunk.Data)
		assert.Equal(t, "video/h264", chunk.Metadata["mime_type"], "metadata: %v", chunk.Metadata)
	})
}

// waitForChunk waits for the provider's session to receive one media chunk.
func waitForChunk(t *testing.T, provider *mock.StreamingProvider) *types.MediaChunk {
	t.Helper()
	deadline := time.Now().Add(lifecycleWait)
	for time.Now().Before(deadline) {
		if s := provider.GetSession(); s != nil {
			if chunks := s.GetChunks(); len(chunks) > 0 {
				return chunks[0]
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no media chunk reached the provider session")
	return nil
}

// SendChunk refuses a nil chunk, and a text chunk reaches the provider session.
func TestDuplexSession_SendChunk(t *testing.T) {
	sess, provider := newASMSession(t)
	defer sess.Close()

	err := sess.SendChunk(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunk cannot be nil")

	require.NoError(t, sess.SendChunk(context.Background(), &providers.StreamChunk{Content: "typed"}))
	mockSession := waitForSession(provider, lifecycleWait)
	require.NotNil(t, mockSession)
	require.Eventually(t, func() bool { return len(mockSession.GetTexts()) == 1 }, lifecycleWait, 5*time.Millisecond)
	assert.Equal(t, []string{"typed"}, mockSession.GetTexts())
}

// Clear empties a conversation that has history.
func TestDuplexSession_ClearEmptiesHistory(t *testing.T) {
	store := statestore.NewMemoryStore()
	require.NoError(t, store.Save(context.Background(), &statestore.ConversationState{
		ID: "conv-clear",
		Messages: []types.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
	}))
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        mock.NewStreamingProvider("mock-provider", "mock-model", false),
		PipelineBuilder: testPipelineBuilder,
		ConversationID:  "conv-clear",
		StateStore:      store,
	})
	require.NoError(t, err)

	before, err := sess.Messages(context.Background())
	require.NoError(t, err)
	require.Len(t, before, 2, "test premise: the session reads the stored history")

	require.NoError(t, sess.Clear(context.Background()))
	after, err := sess.Messages(context.Background())
	require.NoError(t, err)
	assert.Empty(t, after)
}

// parentStreamConfig returns the streaming config a session holds.
func parentStreamConfig(t *testing.T, sess DuplexSession) (*duplexSession, *providers.StreamingInputConfig) {
	t.Helper()
	ds, ok := sess.(*duplexSession)
	require.True(t, ok)
	return ds, ds.streamConfig
}

// A fork of a realtime (ASM) session is itself a realtime session: its input
// reaches a new provider stream session, as the parent's does. The fork must
// keep the parent's streaming config, metadata and user, not fall back to a
// text pipeline.
func TestDuplexSession_ForkStaysRealtime(t *testing.T) {
	store := statestore.NewMemoryStore()
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	parent, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        provider,
		Config:          &providers.StreamingInputConfig{SystemInstruction: "parent"},
		PipelineBuilder: testPipelineBuilder,
		ConversationID:  "parent",
		StateStore:      store,
	})
	require.NoError(t, err)
	defer parent.Close()

	var gotStream providers.StreamInputSupport
	var gotConfig *providers.StreamingInputConfig
	recordingBuilder := func(ctx context.Context, p providers.Provider, sp providers.StreamInputSupport,
		cfg *providers.StreamingInputConfig, id string, s statestore.Store) (*stage.StreamPipeline, error) {
		gotStream, gotConfig = sp, cfg
		return testPipelineBuilder(ctx, p, sp, cfg, id, s)
	}
	fork, err := parent.ForkSession(context.Background(), "fork", recordingBuilder)
	require.NoError(t, err)
	defer fork.Close()

	assert.NotNil(t, gotStream, "the fork's pipeline must be built in realtime mode")
	require.NotNil(t, gotConfig)
	assert.Equal(t, "parent", gotConfig.SystemInstruction)
	// The duplex stage writes the rendered instruction into its config, so
	// parent and fork must not share one.
	gotConfig.SystemInstruction = "written by the fork"
	_, parentCfg := parentStreamConfig(t, parent)
	assert.Equal(t, "parent", parentCfg.SystemInstruction, "the fork must not share the parent's config")

	require.NoError(t, fork.SendText(context.Background(), "to the fork"))
	require.Eventually(t, func() bool {
		for _, s := range provider.GetSessions() {
			for _, txt := range s.GetTexts() {
				if txt == "to the fork" {
					return true
				}
			}
		}
		return false
	}, lifecycleWait, 5*time.Millisecond, "the fork's text must reach a provider stream session")
}
