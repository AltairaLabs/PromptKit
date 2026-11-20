package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

const brunoLine = "We don't talk about Bruno."

// oobAudio and replyAudio are the PCM the scripted server sends for an
// out-of-band response and for a normal reply, told apart by their bytes.
var (
	oobAudio   = bytes.Repeat([]byte{0x00, 0x40}, 2400)
	replyAudio = bytes.Repeat([]byte{0x11, 0x02}, 24000) // 1 s: still playing when the caller interrupts
)

// oobUsage is the usage the scripted server reports for an out-of-band response.
func oobUsage() map[string]any {
	return map[string]any{"total_tokens": 100, "input_tokens": 40, "output_tokens": 60,
		"input_token_details":  map[string]any{"text_tokens": 40},
		"output_token_details": map[string]any{"text_tokens": 10, "audio_tokens": 50}}
}

// oobServer is a Realtime server that answers a response.create with
// response.conversation "none" as the live API does (probed 2026-11-18): an
// out-of-band response, with its metadata echoed, two audio deltas, its
// transcript, conversation.item.done and response.output_item.done for its
// item, and a response.done with conversation_id null. Several run at once. A
// normal response.create starts a reply (audio and transcript), which ends
// when the test sends a status on end; one sent while a reply is in progress
// is rejected as the server does. session.updated is sent once the test
// closes updated (at once when it is nil). It reports every client event.
type oobServer struct {
	srv     *httptest.Server
	events  chan map[string]any
	end     chan string
	push    chan map[string]any
	updated chan struct{}
	// said is what each out-of-band response says, in order; with none left,
	// it says brunoLine.
	said chan string
	// hold, when set, keeps each out-of-band response open after its audio
	// until it is closed.
	hold chan struct{}
	// oobStatus is each out-of-band response's final status ("completed").
	oobStatus string
	// rejectOOB, when set, rejects each out-of-band create with this code.
	rejectOOB string
	// rejectUpdate rejects the session.update, and no session.updated follows.
	rejectUpdate bool
	// delayCreated, when set, holds each out-of-band response.created until
	// it is closed.
	delayCreated chan struct{}
	// rejectAfterCreated sends an error naming the out-of-band create after
	// its response.created; the response then goes on to its end.
	rejectAfterCreated bool

	// cancels are the out-of-band responses running, by ID: a response.cancel
	// naming one ends it as cancelled.
	mu      sync.Mutex
	cancels map[string]chan struct{}
}

// oobCancel is the cancel channel of the out-of-band response id, if running.
func (s *oobServer) oobCancel(id string) (chan struct{}, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cancels[id]
	if ok {
		delete(s.cancels, id)
	}
	return c, ok
}

func newOOBServer(t *testing.T, configure ...func(*oobServer)) *oobServer {
	t.Helper()
	s := &oobServer{events: make(chan map[string]any, 64), end: make(chan string, 8),
		push: make(chan map[string]any, 8), said: make(chan string, 8), oobStatus: "completed"}
	for _, c := range configure {
		c(s)
	}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var wmu sync.Mutex
		write := func(v map[string]any) {
			wmu.Lock()
			defer wmu.Unlock()
			_ = conn.WriteJSON(v)
		}
		write(map[string]any{"type": "session.created", "session": map[string]any{"id": "s", "model": "gpt-realtime"}})
		update := readClientEvent(conn)
		if update == nil {
			return
		}
		if s.rejectUpdate {
			write(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error",
				"code": "invalid_value", "message": "bad voice", "event_id": update["event_id"]}})
		}
		go func() {
			if s.rejectUpdate {
				return
			}
			if s.updated != nil {
				select {
				case <-s.updated:
				case <-r.Context().Done():
					return
				}
			}
			write(map[string]any{"type": "session.updated", "session": map[string]any{"id": "s"}})
		}()
		client := make(chan map[string]any, 16)
		go func() {
			defer close(client)
			for {
				ev := readClientEvent(conn)
				if ev == nil {
					return
				}
				client <- ev
			}
		}()
		normal, oob, active := 0, 0, ""
		for {
			select {
			case ev, ok := <-client:
				if !ok {
					return
				}
				s.events <- ev
				if ev["type"] == realtimeEventResponseCancel {
					id, _ := ev["response_id"].(string)
					if c, ok := s.oobCancel(id); ok {
						close(c)
					} else if id == active && active != "" {
						s.end <- "cancelled"
					} else {
						write(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error",
							"code": "response_cancel_not_active", "message": "none", "event_id": ev["event_id"]}})
					}
					continue
				}
				if ev["type"] != realtimeEventResponseCreate {
					continue
				}
				resp, _ := ev["response"].(map[string]any)
				if resp["conversation"] == "none" {
					if s.rejectOOB != "" {
						write(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error",
							"code": s.rejectOOB, "message": "rejected", "event_id": ev["event_id"]}})
						continue
					}
					oob++
					id := fmt.Sprintf("OOB%d", oob)
					cancel := make(chan struct{})
					s.mu.Lock()
					if s.cancels == nil {
						s.cancels = map[string]chan struct{}{}
					}
					s.cancels[id] = cancel
					s.mu.Unlock()
					go s.outOfBand(r.Context(), write, id, resp["metadata"], ev["event_id"], cancel)
					continue
				}
				if active != "" {
					write(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error",
						"code": errCodeActiveResponse, "message": "active", "event_id": ev["event_id"]}})
					continue
				}
				normal++
				active = fmt.Sprintf("R%d", normal)
				s.reply(write, active, resp["metadata"])
			case status := <-s.end:
				if active != "" {
					write(map[string]any{"type": "response.done", "response": map[string]any{"id": active,
						"status": status, "conversation_id": "conv_1",
						"output": []any{map[string]any{"id": "item_" + active, "type": "message"}}}})
					active = ""
				}
			case ev := <-s.push:
				write(ev)
			case <-r.Context().Done():
				return
			}
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// reply starts a normal reply: its audio and transcript; it ends on end.
func (s *oobServer) reply(write func(map[string]any), id string, meta any) {
	resp := map[string]any{"id": id, "status": "in_progress", "conversation_id": "conv_1"}
	if meta != nil {
		resp["metadata"] = meta
	}
	item := "item_" + id
	write(map[string]any{"type": "response.created", "response": resp})
	write(map[string]any{"type": "response.output_audio.delta", "response_id": id, "item_id": item,
		"content_index": 0, "delta": base64.StdEncoding.EncodeToString(replyAudio)})
	write(map[string]any{"type": "response.output_audio_transcript.delta", "response_id": id, "item_id": item,
		"content_index": 0, "delta": "Normal reply."})
}

// outOfBand runs one out-of-band response.
func (s *oobServer) outOfBand(ctx context.Context, write func(map[string]any), id string, meta, createID any,
	cancel chan struct{},
) {
	defer s.oobCancel(id)
	said := brunoLine
	select {
	case said = <-s.said:
	default:
	}
	item := map[string]any{"id": "item_" + id, "type": "message", "role": "assistant", "status": "completed",
		"content": []any{map[string]any{"type": "output_audio", "transcript": said}}}
	ref := func(typ string, extra map[string]any) map[string]any {
		ev := map[string]any{"type": typ, "response_id": id, "item_id": "item_" + id, "output_index": 0,
			"content_index": 0}
		for k, v := range extra {
			ev[k] = v
		}
		return ev
	}
	if s.delayCreated != nil {
		select {
		case <-s.delayCreated:
		case <-ctx.Done():
			return
		}
	}
	write(map[string]any{"type": "response.created", "response": map[string]any{"id": id,
		"status": "in_progress", "conversation_id": nil, "metadata": meta}})
	if s.rejectAfterCreated {
		write(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error",
			"code": "invalid_value", "message": "late", "event_id": createID}})
	}
	write(map[string]any{"type": "response.output_item.added", "response_id": id, "output_index": 0,
		"item": map[string]any{"id": "item_" + id, "type": "message", "role": "assistant", "status": "in_progress"}})
	half := len(oobAudio) / 2
	write(ref("response.output_audio.delta", map[string]any{"delta": base64.StdEncoding.EncodeToString(oobAudio[:half])}))
	write(ref("response.output_audio_transcript.delta", map[string]any{"delta": said}))
	if s.hold != nil {
		select {
		case <-s.hold:
		case <-cancel:
			write(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "cancelled",
				"conversation_id": nil, "metadata": meta, "output": []any{}, "usage": oobUsage()}})
			return
		case <-ctx.Done():
			return
		}
	}
	write(ref("response.output_audio.delta", map[string]any{"delta": base64.StdEncoding.EncodeToString(oobAudio[half:])}))
	write(ref("response.output_audio.done", nil))
	write(ref("response.output_audio_transcript.done", map[string]any{"transcript": said}))
	write(map[string]any{"type": "conversation.item.done", "item": item})
	write(map[string]any{"type": "response.output_item.done", "response_id": id, "output_index": 0, "item": item})
	write(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": s.oobStatus,
		"conversation_id": nil, "metadata": meta, "output": []any{item}, "usage": oobUsage()}})
}

func (s *oobServer) await(t *testing.T) map[string]any {
	t.Helper()
	select {
	case e := <-s.events:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no client event")
		return nil
	}
}

func (s *oobServer) awaitType(t *testing.T, typ string) map[string]any {
	t.Helper()
	ev := s.await(t)
	require.Equal(t, typ, ev["type"])
	return ev
}

func (s *oobServer) quiet(t *testing.T) {
	t.Helper()
	select {
	case e := <-s.events:
		t.Fatalf("unexpected client event %v", e)
	case <-time.After(150 * time.Millisecond):
	}
}

func openOOBSession(t *testing.T, s *oobServer, configure ...func(*RealtimeSessionConfig)) *RealtimeSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	config := DefaultRealtimeSessionConfig()
	for _, c := range configure {
		c(&config)
	}
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{endpoint: bargeInWSURL(s.srv)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// speak renders text out of band, in the background.
func speak(session *RealtimeSession, text string) <-chan speakResult {
	out := make(chan speakResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sp, err := session.SpeakOutOfBand(ctx, text)
		out <- speakResult{sp, err}
	}()
	return out
}

type speakResult struct {
	speech providers.OutOfBandSpeech
	err    error
}

func awaitSpeech(t *testing.T, ch <-chan speakResult) speakResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("SpeakOutOfBand did not return")
		return speakResult{}
	}
}

// withoutEventID is a client event without its event_id.
func withoutEventID(ev map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range ev {
		if k != "event_id" {
			out[k] = v
		}
	}
	return out
}

// The render waits for session.updated, then asks for a response outside the
// conversation, tagged with its own event ID, saying exactly the text with
// no tools, audio only. What comes back is the response's audio, transcript
// and cost.
func TestSpeakOutOfBand_RequestsAResponseOutsideTheConversation(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.updated = make(chan struct{}) })
	session := openOOBSession(t, srv)

	res := speak(session, brunoLine)
	srv.quiet(t) // nothing is asked for before session.updated
	close(srv.updated)

	create := srv.awaitType(t, realtimeEventResponseCreate)
	assert.Equal(t, map[string]any{
		"type": realtimeEventResponseCreate,
		"response": map[string]any{
			"conversation":      "none",
			"instructions":      "Say exactly the following, in the same language, and nothing else: " + brunoLine,
			"tool_choice":       "none",
			"output_modalities": []any{"audio"},
			"metadata":          map[string]any{"promptkit_out_of_band": create["event_id"]},
		},
	}, withoutEventID(create))
	assert.NotEmpty(t, create["event_id"])

	r := awaitSpeech(t, res)
	require.NoError(t, r.err)
	assert.Equal(t, oobAudio, r.speech.Audio)
	assert.Equal(t, DefaultRealtimeSampleRate, r.speech.SampleRate)
	assert.Equal(t, brunoLine, r.speech.Transcript)
	usage := &UsageInfo{TotalTokens: 100, InputTokens: 40, OutputTokens: 60}
	usage.InputTokenDetails.TextTokens = 40
	usage.OutputTokenDetails.TextTokens = 10
	usage.OutputTokenDetails.AudioTokens = 50
	require.NotNil(t, r.speech.Cost)
	assert.Equal(t, realtimeCostInfo(usage, 0, 0), r.speech.Cost)
	assert.Positive(t, r.speech.Cost.TotalCost)
}

// Nothing of an out-of-band response reaches Response(): the reply that
// follows is the first thing there, and every chunk is the reply's.
func TestSpeakOutOfBand_NeverReachesResponse(t *testing.T) {
	srv := newOOBServer(t)
	session := openOOBSession(t, srv)

	r := awaitSpeech(t, speak(session, brunoLine))
	require.NoError(t, r.err)
	srv.awaitType(t, realtimeEventResponseCreate)

	require.NoError(t, session.SendText(context.Background(), "What did you just say?"))
	srv.awaitType(t, realtimeEventItemCreate)
	srv.awaitType(t, realtimeEventResponseCreate)
	srv.end <- "completed"
	chunks := responseChunks(t, session, 1)
	var audioOut []byte
	var text string
	for _, c := range chunks {
		id, _ := c.Metadata[keyResponseID].(string)
		if id != "" {
			assert.Equal(t, "R1", id, "a chunk of another response")
		}
		assert.Equal(t, int64(1), c.Generation, "the reply is the session's first response")
		if c.MediaData != nil {
			audioOut = append(audioOut, c.MediaData.Data...)
		}
		text += c.Delta
		assert.Nil(t, c.Metadata["transcript"], "no transcript of the out-of-band response")
	}
	assert.Equal(t, replyAudio, audioOut)
	assert.Equal(t, "Normal reply.", text)
}

// An out-of-band response's item enters no conversation: neither its
// conversation.item.done nor its response.output_item.done is taken as an
// item of the caller's turn. Forgetting the turn deletes only the caller's
// item, and a barge-in cuts nothing (none of its audio was played).
func TestSpeakOutOfBand_RegistersNoConversationItem(t *testing.T) {
	srv := newOOBServer(t)
	session := openOOBSession(t, srv)
	srv.push <- map[string]any{"type": "input_audio_buffer.committed", "item_id": "user_1"}

	r := awaitSpeech(t, speak(session, brunoLine))
	require.NoError(t, r.err)
	srv.awaitType(t, realtimeEventResponseCreate)

	srv.push <- map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "user_2"}
	srv.quiet(t) // no truncate: nothing of it was played
	require.NoError(t, session.ForgetTurn(context.Background(), "user_1"))
	del := srv.awaitType(t, realtimeEventItemDelete)
	assert.Equal(t, "user_1", del["item_id"])
	srv.quiet(t) // and nothing else is deleted
}

// An out-of-band response is outside the one-reply-at-a-time queue: a reply
// asked for while one runs goes at once, and one runs while a reply does.
func TestSpeakOutOfBand_RunsBesideReplies(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.hold = make(chan struct{}) })
	session := openOOBSession(t, srv)

	res := speak(session, brunoLine)
	srv.awaitType(t, realtimeEventResponseCreate)
	// The out-of-band response is in progress: a reply asked for goes at once.
	require.NoError(t, session.SendText(context.Background(), "Hello?"))
	srv.awaitType(t, realtimeEventItemCreate)
	reply := srv.awaitType(t, realtimeEventResponseCreate)
	assert.Nil(t, reply["response"].(map[string]any)["conversation"])

	// A reply is in progress (R1): a second out-of-band render goes at once.
	second := speak(session, "Second line.")
	assert.Equal(t, "none", srv.awaitType(t, realtimeEventResponseCreate)["response"].(map[string]any)["conversation"])

	// A reply asked for now waits for R1, and the out-of-band responses
	// ending does not send it.
	require.NoError(t, session.SendText(context.Background(), "Still there?"))
	srv.awaitType(t, realtimeEventItemCreate)
	close(srv.hold)
	require.NoError(t, awaitSpeech(t, res).err)
	require.NoError(t, awaitSpeech(t, second).err)
	srv.quiet(t)

	srv.end <- "completed"
	waiting := srv.awaitType(t, realtimeEventResponseCreate)
	assert.Nil(t, waiting["response"].(map[string]any)["conversation"])
	srv.end <- "completed"
	assert.Equal(t, []string{"R1", "R2"}, endsOf(responseChunks(t, session, 2)))
}

// A transcript other than the text comes back as it is: the caller checks it.
func TestSpeakOutOfBand_ReportsWhatTheModelSaid(t *testing.T) {
	srv := newOOBServer(t)
	srv.said <- "Understood. " + brunoLine
	session := openOOBSession(t, srv)

	r := awaitSpeech(t, speak(session, brunoLine))
	require.NoError(t, r.err)
	assert.Equal(t, "Understood. "+brunoLine, r.speech.Transcript)
}

// A response that did not complete fails the render, with its cost; the
// session goes on with no error on Response().
func TestSpeakOutOfBand_IncompleteResponseFails(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.oobStatus = "cancelled" })
	session := openOOBSession(t, srv)

	r := awaitSpeech(t, speak(session, brunoLine))
	require.EqualError(t, r.err, "the out-of-band response ended cancelled")
	require.NotNil(t, r.speech.Cost)
	assertNoResponseChunk(t, session)
}

// A create the server rejects fails the render, not the session.
func TestSpeakOutOfBand_RejectedRequestFailsTheRenderOnly(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.rejectOOB = "invalid_value" })
	session := openOOBSession(t, srv)

	r := awaitSpeech(t, speak(session, brunoLine))
	require.EqualError(t, r.err, "the server rejected the out-of-band response: invalid_value: rejected")
	assertNoResponseChunk(t, session)
}

// A text-only session has no voice to speak in: nothing is asked for.
func TestSpeakOutOfBand_TextOnlySessionAsksForNothing(t *testing.T) {
	srv := newOOBServer(t)
	session := openOOBSession(t, srv, func(c *RealtimeSessionConfig) { c.Modalities = []string{"text"} })

	r := awaitSpeech(t, speak(session, brunoLine))
	require.EqualError(t, r.err, "the session's replies are not spoken")
	srv.quiet(t)
}

// A render waiting for session.updated returns when its context ends.
func TestSpeakOutOfBand_ContextEndsTheWait(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.updated = make(chan struct{}) })
	session := openOOBSession(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := session.SpeakOutOfBand(ctx, brunoLine)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	srv.quiet(t)
}

func assertNoResponseChunk(t *testing.T, session *RealtimeSession) {
	t.Helper()
	select {
	case c := <-session.Response():
		t.Fatalf("unexpected chunk on Response(): %+v", c)
	case <-time.After(100 * time.Millisecond):
	}
}
