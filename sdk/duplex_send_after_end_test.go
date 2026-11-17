package sdk

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2/session"
)

// sendBound is how long a send on an ended session may take before it counts
// as blocked: far beyond an immediate return, far short of any real timeout.
const sendBound = 2 * time.Second

// endedDuplex is a duplex conversation whose pipeline has ended, with what
// its Response() reader received.
type endedDuplex struct {
	conv   *Conversation
	mu     sync.Mutex
	chunks []providers.StreamChunk
	closed chan struct{}
}

func (e *endedDuplex) errs() []error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var errs []error
	for _, c := range e.chunks {
		if c.Error != nil {
			errs = append(errs, c.Error)
		}
	}
	return errs
}

// openAndEndDuplex opens a duplex conversation on prov, reads Response(),
// sends one text turn, and waits for the pipeline to end: Done() closed and
// Response() closed.
func openAndEndDuplex(t *testing.T, prov *mock.StreamingProvider, opts ...Option) *endedDuplex {
	t.Helper()
	opts = append([]Option{
		WithProvider(prov), WithSkipSchemaValidation(),
		WithStreamingConfig(&providers.StreamingInputConfig{Config: types.StreamingMediaConfig{
			Type: types.ContentTypeAudio, ChunkSize: 3200, SampleRate: 16000, Channels: 1, BitDepth: 16,
			Encoding: "pcm_linear16"}}),
	}, opts...)
	conv, err := OpenDuplex(createTestPackFile(t, convChatPack), "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	e := &endedDuplex{conv: conv, closed: make(chan struct{})}
	resp, err := conv.Response()
	require.NoError(t, err)
	go func() {
		defer close(e.closed)
		for c := range resp {
			e.mu.Lock()
			e.chunks = append(e.chunks, c)
			e.mu.Unlock()
		}
	}()

	require.NoError(t, conv.SendText(context.Background(), "hi"))

	done, err := conv.Done()
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the session's pipeline did not end")
	}
	select {
	case <-e.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Response() did not close when the pipeline ended")
	}
	return e
}

// sendWithin runs send and returns its error, failing the test if it does not
// return within sendBound.
func sendWithin(t *testing.T, name string, send func() error) error {
	t.Helper()
	got := make(chan error, 1)
	go func() { got <- send() }()
	select {
	case err := <-got:
		return err
	case <-time.After(sendBound):
		t.Fatalf("%s blocked on a session whose pipeline had ended", name)
		return nil
	}
}

// everySend is each input entry point of a duplex conversation.
func everySend(conv *Conversation) map[string]func() error {
	ctx := context.Background()
	return map[string]func() error{
		"SendText": func() error { return conv.SendText(ctx, "anyone there?") },
		"SendChunk": func() error {
			return conv.SendChunk(ctx, &providers.StreamChunk{MediaData: &providers.StreamMediaData{
				Data: make([]byte, 320), MIMEType: "audio/pcm", SampleRate: 16000, Channels: 1}})
		},
		"SendFrame": func() error {
			return conv.SendFrame(ctx, &session.ImageFrame{Data: []byte("frame"), MIMEType: "image/jpeg"})
		},
		"SendVideoChunk": func() error {
			return conv.SendVideoChunk(ctx, &session.VideoChunk{Data: []byte("video"), MIMEType: "video/h264"})
		},
		"TriggerStart": func() error { return conv.TriggerStart(ctx, "say hello") },
	}
}

// assertSendsFail checks that every input entry point returns check's error,
// promptly, and keeps doing so past the input buffer's capacity: a send
// must never be swallowed by a channel nobody reads, nor block on it.
func assertSendsFail(t *testing.T, conv *Conversation, check func(t *testing.T, err error)) {
	t.Helper()
	for name, send := range everySend(conv) {
		err := sendWithin(t, name, send)
		require.Errorf(t, err, "%s on an ended session returned no error", name)
		check(t, err)
	}
	// Past the 100-element input buffer: the hang the issue reported.
	for i := 0; i < 250; i++ {
		err := sendWithin(t, "SendText", func() error { return conv.SendText(context.Background(), "again") })
		require.Errorf(t, err, "send %d on an ended session returned no error", i)
	}
}

// A session the idle timeout ended refuses input with the idle-timeout error,
// and its Response() reader got that error before the channel closed.
func TestDuplex_SendAfterIdleTimeoutReturnsTheIdleError(t *testing.T) {
	prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hello")
	e := openAndEndDuplex(t, prov, WithIdleTimeout(50*time.Millisecond))

	errs := e.errs()
	require.NotEmpty(t, errs, "Response() got no error before it closed")
	assert.ErrorIs(t, errs[len(errs)-1], stage.ErrIdleTimeout)

	assertSendsFail(t, e.conv, func(t *testing.T, err error) {
		assert.ErrorIs(t, err, stage.ErrIdleTimeout)
	})
	assert.ErrorIs(t, e.conv.SessionError(), stage.ErrIdleTimeout)
}

// A session the provider's error ended refuses input with that error.
func TestDuplex_SendAfterProviderErrorReturnsThatError(t *testing.T) {
	boom := errors.New("provider exploded")
	prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("unused").
		WithResponseChunks(providers.StreamChunk{Error: boom})
	e := openAndEndDuplex(t, prov)

	errs := e.errs()
	require.NotEmpty(t, errs, "Response() got no error before it closed")
	assert.ErrorIs(t, errs[len(errs)-1], boom)

	assertSendsFail(t, e.conv, func(t *testing.T, err error) {
		assert.ErrorIs(t, err, boom)
	})
	assert.ErrorIs(t, e.conv.SessionError(), boom)
}

// A session the provider closed without an error refuses input with the
// session-closed error.
func TestDuplex_SendAfterProviderClosesReturnsSessionClosed(t *testing.T) {
	prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("bye").WithCloseAfterTurns(1)
	e := openAndEndDuplex(t, prov)

	assertSendsFail(t, e.conv, func(t *testing.T, err error) {
		assert.ErrorIs(t, err, ErrSessionClosed)
	})
	assert.NoError(t, e.conv.SessionError())
}
