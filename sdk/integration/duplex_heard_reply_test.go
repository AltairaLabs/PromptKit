package integration

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
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

// heardReplySession is a mock realtime session the test drives event by
// event; it answers the tool calls the stage runs on a channel.
type heardReplySession struct {
	*mock.MockStreamSession
	toolResponses chan []providers.ToolResponse
}

func (s *heardReplySession) SendToolResponse(ctx context.Context, id, result string) error {
	return s.SendToolResponses(ctx, []providers.ToolResponse{{ToolCallID: id, Result: result}})
}

func (s *heardReplySession) SendToolResponses(_ context.Context, rs []providers.ToolResponse) error {
	s.toolResponses <- rs
	return nil
}

// heardReplyProvider hands out one scripted session.
type heardReplyProvider struct {
	*mock.StreamingProvider
	sess *heardReplySession
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
	sess   *heardReplySession
	store  *statestore.MemoryStore
	events events.EventStore
	pack   string
	// turns counts the assistant turns the caller has seen complete.
	turns atomic.Int32
}

// openHeardReplyCall opens the call; opts follow the defaults (pass
// sdk.WithPlayoutTracking() for a host that plays the audio).
func openHeardReplyCall(t *testing.T, opts ...sdk.Option) *heardReplyCall {
	t.Helper()
	sess := &heardReplySession{MockStreamSession: mock.NewMockStreamSession(),
		toolResponses: make(chan []providers.ToolResponse, 4)}
	prov := &heardReplyProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false), sess: sess}
	store := statestore.NewMemoryStore()
	pack := lateTranscriptPackJSON
	conv, evs := openRecordedDuplex(t, pack, prov, append([]sdk.Option{sdk.WithStateStore(store)}, opts...)...)
	conv.OnTool("lookup_order", func(map[string]any) (any, error) {
		return map[string]any{"status": "shipped"}, nil
	})
	c := &heardReplyCall{t: t, conv: conv, sess: sess, store: store, events: evs, pack: writePackFile(t, pack)}
	ch, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for chunk := range ch {
			if done, _ := chunk.Metadata["assistant_turn_complete"].(bool); done {
				c.turns.Add(1)
			}
		}
	}()
	return c
}

// awaitTurns waits until the caller has seen n assistant turns complete.
func (c *heardReplyCall) awaitTurns(n int32) {
	c.t.Helper()
	require.Eventually(c.t, func() bool { return c.turns.Load() >= n }, 3*time.Second, 2*time.Millisecond,
		"the caller never saw %d replies complete", n)
}

// ask sends the caller's typed turn and waits for the session to get it.
func (c *heardReplyCall) ask(text string) {
	c.t.Helper()
	n := len(c.sess.GetTexts())
	require.NoError(c.t, c.conv.SendText(context.Background(), text))
	require.Eventually(c.t, func() bool { return len(c.sess.GetTexts()) > n }, 2*time.Second, 2*time.Millisecond)
}

// speak emits a reply's spoken words and d of 24 kHz audio, at once.
func (c *heardReplyCall) speak(text string, d time.Duration) {
	c.sess.EmitChunk(&providers.StreamChunk{Delta: text, Metadata: map[string]any{"type": "output_transcription"}})
	c.sess.EmitChunk(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: make([]byte, int(d.Seconds()*24000)*2), MIMEType: "audio/pcm", SampleRate: 24000, Channels: 1}})
}

// finish ends the reply.
func (c *heardReplyCall) finish(reason string, calls ...types.MessageToolCall) {
	c.sess.EmitChunk(&providers.StreamChunk{FinishReason: &reason, ToolCalls: calls})
}

// reply emits a whole spoken reply.
func (c *heardReplyCall) reply(text string, d time.Duration) {
	c.speak(text, d)
	c.finish("stop")
}

// bargeIn is the caller talking over the reply still playing: the provider's
// in-band interruption.
func (c *heardReplyCall) bargeIn() {
	c.sess.EmitChunk(&providers.StreamChunk{Interrupted: true})
}

// saved waits until the store holds n messages and returns them.
func (c *heardReplyCall) saved(n int) []types.Message {
	c.t.Helper()
	var msgs []types.Message
	require.Eventually(c.t, func() bool {
		state, err := c.store.Load(context.Background(), c.conv.ID())
		if err != nil || state == nil {
			return false
		}
		msgs = state.Messages
		return len(msgs) >= n
	}, 5*time.Second, 5*time.Millisecond, "the store never held %d messages", n)
	return msgs
}

// playoutMeta is a stored reply's playout metadata.
type playoutMeta struct {
	Finish   string
	Cut      any
	HeardMs  float64
	AudioMs  float64
	Fraction float64
}

func playoutOf(m types.Message) playoutMeta {
	num := func(v any) float64 {
		switch n := v.(type) {
		case int64:
			return float64(n)
		case float64:
			return n
		}
		return -1
	}
	return playoutMeta{Finish: m.FinishReason, Cut: m.Meta["playout_cut"],
		HeardMs: num(m.Meta["heard_ms"]), AudioMs: num(m.Meta["audio_ms"]), Fraction: num(m.Meta["played_fraction"])}
}

// The caller barges in on a two-second reply that finished generating, before
// it has played: the stored reply keeps its full text and finish reason, and
// records how little of it was heard.
func TestDuplexHeardReply_CutDuringPlayout(t *testing.T) {
	c := openHeardReplyCall(t, sdk.WithPlayoutTracking())
	c.ask("tell me a story")
	c.reply("Once upon a time there was a lighthouse.", 2*time.Second)
	c.bargeIn()

	got := c.saved(2)[1]
	assert.Equal(t, "Once upon a time there was a lighthouse.", got.Content, "the text is kept whole")
	p := playoutOf(got)
	assert.Equal(t, "stop", p.Finish, "the reply completed; only its playback was cut")
	assert.Equal(t, "barge-in", p.Cut)
	assert.Less(t, p.HeardMs, 1000.0, "barged in before half of it played")
	assert.Equal(t, float64(2000), p.AudioMs)
}

// A reply the caller hears to the end is stored as it is today, once its
// audio has played; the recording's message.created events keep their
// transcript order and indexes.
func TestDuplexHeardReply_FullyPlayedStoredUnchanged(t *testing.T) {
	c := openHeardReplyCall(t, sdk.WithPlayoutTracking())
	c.ask("hi")
	start := time.Now()
	c.reply("Hello there.", 300*time.Millisecond)

	got := c.saved(2)[1]
	assert.GreaterOrEqual(t, time.Since(start), 280*time.Millisecond, "stored once the audio played")
	assert.Equal(t, int32(1), c.turns.Load(), "the caller sees the reply's turn end once")
	assert.Equal(t, "Hello there.", got.Content)
	assert.Equal(t, playoutMeta{Finish: "stop", HeardMs: -1, AudioMs: -1, Fraction: -1}, playoutOf(got),
		"stored unchanged")

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
	c := openHeardReplyCall(t, sdk.WithPlayoutTracking())
	c.ask("tell me a story")
	c.reply("Once upon a time.", 2*time.Second)
	c.awaitTurns(1) // the caller has the reply; it is still playing
	require.NoError(t, c.conv.Close())

	got := c.saved(2)[1]
	assert.Equal(t, "Once upon a time.", got.Content)
	p := playoutOf(got)
	assert.Equal(t, "stop", p.Finish)
	assert.Equal(t, "session-closed", p.Cut)
	assert.Less(t, p.HeardMs, 1000.0)
	assert.Equal(t, float64(2000), p.AudioMs)
}

// Without a listener (no paced speaker, no WithPlayoutTracking) nothing is
// held: the reply is stored as soon as it completes, unmarked, even when the
// session closes before its audio could have played.
func TestDuplexHeardReply_NoListenerStoresAtOnce(t *testing.T) {
	c := openHeardReplyCall(t)
	c.ask("tell me a story")
	start := time.Now()
	c.reply("Once upon a time.", 5*time.Second)
	c.saved(2)
	assert.Less(t, time.Since(start), 2*time.Second, "not held for its five seconds of audio")
	require.NoError(t, c.conv.Close())
	p := playoutOf(c.saved(2)[1])
	assert.Equal(t, playoutMeta{Finish: "stop", HeardMs: -1, AudioMs: -1, Fraction: -1}, p, "nothing marked")
}

// A reply cut during playout that made a tool call stays in model context
// with its call and result: a later turn's provider request carries them.
func TestDuplexHeardReply_CutToolReplyStaysInContext(t *testing.T) {
	c := openHeardReplyCall(t, sdk.WithPlayoutTracking())
	c.ask("where is order 7?")
	c.speak("Let me check.", 2*time.Second)
	c.finish("tool_calls", types.MessageToolCall{ID: "call_1", Name: "lookup_order",
		Args: json.RawMessage(`{"order":"7"}`)})
	select {
	case <-c.sess.toolResponses:
	case <-time.After(3 * time.Second):
		t.Fatal("the tool never ran")
	}
	c.reply("It has shipped.", time.Second)
	c.bargeIn()
	msgs := c.saved(4)
	assert.Equal(t, "barge-in", msgs[1].Meta["playout_cut"])
	convID := c.conv.ID()
	require.NoError(t, c.conv.Close())

	prov := &requestRecordingProvider{Provider: mock.NewProvider("mock", "mock-model", false)}
	resumed, err := sdk.Resume(convID, c.pack, "chat", sdk.WithProvider(prov), sdk.WithSkipSchemaValidation(),
		sdk.WithStateStore(c.store))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resumed.Close() })
	_, err = resumed.Send(context.Background(), "thanks")
	require.NoError(t, err)

	var sent []string
	for _, m := range prov.firstRequest() {
		switch {
		case len(m.ToolCalls) > 0:
			sent = append(sent, m.Role+":calls "+m.ToolCalls[0].ID)
		case m.ToolResult != nil:
			sent = append(sent, m.Role+":result "+m.ToolResult.ID)
		default:
			sent = append(sent, m.Role+":"+m.GetContent())
		}
	}
	assert.Equal(t, []string{
		"user:where is order 7?", "assistant:calls call_1", "tool:result call_1",
		"assistant:It has shipped.", "user:thanks",
	}, sent)
}

// requestRecordingProvider records the messages of each request.
type requestRecordingProvider struct {
	*mock.Provider
	mu       sync.Mutex
	requests [][]types.Message
}

func (p *requestRecordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req.Messages)
	return p.Provider.Predict(ctx, req)
}

func (p *requestRecordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req.Messages)
	return p.Provider.PredictStream(ctx, req)
}

func (p *requestRecordingProvider) record(msgs []types.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, append([]types.Message(nil), msgs...))
}

func (p *requestRecordingProvider) firstRequest() []types.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return nil
	}
	return p.requests[0]
}
