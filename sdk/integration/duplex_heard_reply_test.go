package integration

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/recording"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// These tests script a realtime session whose reply's audio arrives far
// faster than it plays, as OpenAI Realtime and Gemini Live send it, and check
// what the conversation stores for it: the reply as the caller heard it.

// heardReplyProvider hands out one scripted session.
type heardReplyProvider struct {
	*mock.StreamingProvider
	sess *mock.MockStreamSession
}

func (p *heardReplyProvider) CreateStreamSession(
	context.Context, *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return p.sess, nil
}

// heardReplyCall is an open realtime conversation over a scripted session,
// saving to a state store and recording to an event store.
type heardReplyCall struct {
	t      *testing.T
	conv   *sdk.Conversation
	sess   *mock.MockStreamSession
	store  *statestore.MemoryStore
	events events.EventStore
}

func openHeardReplyCall(t *testing.T) *heardReplyCall {
	t.Helper()
	sess := mock.NewMockStreamSession()
	prov := &heardReplyProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false), sess: sess}
	store := statestore.NewMemoryStore()
	conv, evs := openRecordedDuplex(t, minimalPackJSON, prov, sdk.WithStateStore(store))
	drainDuplexResponse(t, conv)
	return &heardReplyCall{t: t, conv: conv, sess: sess, store: store, events: evs}
}

// ask sends the caller's typed turn and waits for the session to get it.
func (c *heardReplyCall) ask(text string) {
	c.t.Helper()
	require.NoError(c.t, c.conv.SendText(context.Background(), text))
	require.Eventually(c.t, func() bool { return len(c.sess.GetTexts()) > 0 }, 2*time.Second, 2*time.Millisecond)
}

// reply emits a spoken reply: its transcript, d of 24 kHz audio at once, and
// its completion.
func (c *heardReplyCall) reply(text string, d time.Duration) {
	c.sess.EmitChunk(&providers.StreamChunk{Delta: text, Metadata: map[string]any{"type": "output_transcription"}})
	c.sess.EmitChunk(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: make([]byte, int(d.Seconds()*24000)*2), MIMEType: "audio/pcm", SampleRate: 24000, Channels: 1}})
	stop := "stop"
	c.sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
}

// bargeIn is the caller talking over the reply still playing: the provider's
// in-band interruption.
func (c *heardReplyCall) bargeIn() {
	c.sess.EmitChunk(&providers.StreamChunk{Interrupted: true})
}

// savedReply waits for the stored assistant message and returns it.
func (c *heardReplyCall) savedReply() types.Message {
	c.t.Helper()
	var reply *types.Message
	require.Eventually(c.t, func() bool {
		state, err := c.store.Load(context.Background(), c.conv.ID())
		if err != nil || state == nil {
			return false
		}
		for i := range state.Messages {
			if state.Messages[i].Role == "assistant" {
				m := state.Messages[i]
				reply = &m
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond, "no reply was stored")
	return *reply
}

// heardMeta is a stored reply's playout metadata.
type heardMeta struct {
	Finish   string
	Cause    any
	HeardMs  float64
	AudioMs  float64
	Fraction float64
}

func heardOf(m types.Message) heardMeta {
	num := func(v any) float64 {
		switch n := v.(type) {
		case int64:
			return float64(n)
		case float64:
			return n
		}
		return -1
	}
	return heardMeta{Finish: string(m.FinishReason), Cause: m.Meta[types.MetaInterruptedCause],
		HeardMs: num(m.Meta["heard_ms"]), AudioMs: num(m.Meta["audio_ms"]), Fraction: num(m.Meta["played_fraction"])}
}

// The caller barges in 400 ms into a two-second reply that finished
// generating: the stored reply keeps its full text, is marked interrupted,
// and says how much the caller heard.
func TestDuplexHeardReply_CutDuringPlayout(t *testing.T) {
	c := openHeardReplyCall(t)
	c.ask("tell me a story")
	c.reply("Once upon a time there was a lighthouse.", 2*time.Second)
	time.Sleep(400 * time.Millisecond)
	c.bargeIn()

	got := c.savedReply()
	assert.Equal(t, "Once upon a time there was a lighthouse.", got.Content, "the text is kept whole")
	h := heardOf(got)
	assert.Equal(t, "interrupted", h.Finish)
	assert.Equal(t, "barge-in-playout", h.Cause)
	assert.InDelta(t, 400, h.HeardMs, 150)
	assert.Equal(t, float64(2000), h.AudioMs)
	assert.InDelta(t, 0.2, h.Fraction, 0.075)
}

// A reply the caller hears to the end is stored as it is today, once its
// audio has played; the recording's message.created events keep their
// transcript order and indexes.
func TestDuplexHeardReply_FullyPlayedStoredUnchanged(t *testing.T) {
	c := openHeardReplyCall(t)
	c.ask("hi")
	start := time.Now()
	c.reply("Hello there.", 300*time.Millisecond)

	got := c.savedReply()
	assert.GreaterOrEqual(t, time.Since(start), 280*time.Millisecond, "stored once the audio played")
	assert.Equal(t, "Hello there.", got.Content)
	assert.Equal(t, "stop", string(got.FinishReason))
	assert.Equal(t, map[string]interface{}{"finish_reason": "stop"}, got.Meta)

	sessionID := c.conv.ID()
	require.NoError(t, c.conv.Close())
	rec, err := recording.Export(context.Background(), c.events, sessionID)
	require.NoError(t, err)
	evs, err := rec.ToTypedEvents()
	require.NoError(t, err)
	var created []string
	for _, e := range evs {
		if d, ok := e.Data.(*events.MessageCreatedData); ok && e.Type == events.EventMessageCreated {
			created = append(created, d.Role+":"+d.Content+":"+strconv.Itoa(d.Index))
		}
	}
	assert.Equal(t, []string{"user:hi:0", "assistant:Hello there.:1"}, created)
}

// A session closed while the reply still plays stores it with what the
// caller had heard by then.
func TestDuplexHeardReply_SessionClosedWhilePlaying(t *testing.T) {
	c := openHeardReplyCall(t)
	c.ask("tell me a story")
	c.reply("Once upon a time.", 2*time.Second)
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, c.conv.Close())

	got := c.savedReply()
	assert.Equal(t, "Once upon a time.", got.Content)
	h := heardOf(got)
	assert.Equal(t, "interrupted", h.Finish)
	assert.Equal(t, "session-closed", h.Cause)
	assert.InDelta(t, 300, h.HeardMs, 150)
	assert.Equal(t, float64(2000), h.AudioMs)
}
