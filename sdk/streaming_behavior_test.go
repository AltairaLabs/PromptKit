package sdk

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	streamPkg "github.com/AltairaLabs/PromptKit/sdk/v2/stream"
)

// StreamRaw yields the reply's text as stream.Chunks and ends with Done.
func TestConv_StreamRawYieldsTextThenDone(t *testing.T) {
	w := newConvWire(t, convWireReply{Text: "raw reply"})
	conv := openConv(t, w)
	chunks, err := conv.StreamRaw(context.Background(), "go")
	require.NoError(t, err)
	var text strings.Builder
	var done int
	for c := range chunks {
		require.NoError(t, c.Error)
		if c.Type == streamPkg.ChunkText {
			text.WriteString(c.Text)
		}
		if c.Done {
			done++
		}
	}
	assert.Equal(t, "raw reply", text.String())
	assert.Equal(t, 1, done, "exactly one Done chunk")
}

// StreamWithCallback hands each text delta to the handler, then the final
// response, and returns that same response; with no handler it still returns
// the reply.
func TestConv_StreamWithCallbackDeliversDeltasAndResponse(t *testing.T) {
	ctx := context.Background()
	w := newConvWire(t, convWireReply{Text: "called back"})
	conv := openConv(t, w)
	var deltas strings.Builder
	var done *Response
	conv.OnStreamEvent(func(e StreamEvent) {
		switch ev := e.(type) {
		case TextDeltaEvent:
			deltas.WriteString(ev.Delta)
		case StreamDoneEvent:
			done = ev.Response
		}
	})
	resp, err := conv.StreamWithCallback(ctx, "go")
	require.NoError(t, err)
	assert.Equal(t, "called back", deltas.String())
	require.NotNil(t, done)
	assert.Equal(t, "called back", done.Text())
	assert.Equal(t, "called back", resp.Text())

	w2 := newConvWire(t, convWireReply{Text: "no handler"})
	resp, err = openConv(t, w2).StreamWithCallback(ctx, "go")
	require.NoError(t, err)
	assert.Equal(t, "no handler", resp.Text())
}

// Send options that attach media work on Stream as on Send.
func TestConv_StreamCarriesMediaOptions(t *testing.T) {
	w := newConvWire(t)
	conv := openConv(t, w, WithUnsafePrivateNetworkMedia())
	for c := range conv.Stream(context.Background(), "look", WithImageURL("https://example.com/i.jpg")) {
		require.NoError(t, c.Error)
	}
	parts := convUserParts(t, w.lastChat(t))
	require.Len(t, parts, 2)
	assert.Equal(t, "look", parts[0]["text"])
	assert.Equal(t, "https://example.com/i.jpg", parts[1]["image_url"].(map[string]any)["url"])
}

// A caller that stops reading a stream and cancels its context frees the
// conversation: the next turn runs. stream.Process does the same when its
// handler stops early.
func TestConv_AbandonedStreamReleasesTheConversation(t *testing.T) {
	long := strings.Repeat("x", 4000) // more chunks than the stream buffers
	t.Run("Stream with cancel", func(t *testing.T) {
		w := newConvWire(t, convWireReply{Text: long, Chunks: 400}, convWireReply{Text: "next"})
		conv := openConv(t, w)
		ctx, cancel := context.WithCancel(context.Background())
		for range conv.Stream(ctx, "go") {
			break
		}
		cancel()
		assertConvNextTurnRuns(t, conv)
	})
	t.Run("stream.Process handler error", func(t *testing.T) {
		w := newConvWire(t, convWireReply{Text: long, Chunks: 400}, convWireReply{Text: "next"})
		conv := openConv(t, w)
		err := streamPkg.Process(context.Background(), conv, "go", func(streamPkg.Chunk) error { return assert.AnError })
		require.ErrorIs(t, err, assert.AnError)
		assertConvNextTurnRuns(t, conv)
	})
}

// assertConvNextTurnRuns fails unless a Send completes promptly.
func assertConvNextTurnRuns(t *testing.T, conv *Conversation) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := conv.Send(ctx, "after")
	require.NoError(t, err, "the abandoned stream still holds the conversation")
	assert.Equal(t, "next", resp.Text())
}
