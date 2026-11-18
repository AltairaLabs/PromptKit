package openai

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// ForgetTurn takes a spoken turn an input guardrail blocked out of the
// model's conversation (providers.TurnForgetter): the caller's audio item and
// the items of the reply to it are deleted with conversation.item.delete, so a
// later turn cannot revive what was blocked.

// forgetStep is one step of a scripted Realtime server: the events it sends,
// then, with await set, the client events it reads until one of that type.
type forgetStep struct {
	send  []map[string]any
	await string
}

// forgetServer runs the steps, then reads client events until
// input_audio_buffer.commit and reports every client event it read after
// session.update, as "type" or "type:item_id". A "$TAG" metadata value in an
// event it sends is replaced by the request tag of the last response.create
// it read, as the server echoes it. With rejectCode
// set, each conversation.item.delete is answered with an error of that code
// (errCodeItemNotFound is what the server says for an item it does not
// have); otherwise with conversation.item.deleted. After the commit it sends
// the events in after.
type forgetServer struct {
	srv    *httptest.Server
	client chan []string
}

func newForgetServer(t *testing.T, steps []forgetStep, rejectCode string, after ...map[string]any) *forgetServer {
	t.Helper()
	fs := &forgetServer{client: make(chan []string, 1)}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		write := func(v map[string]any) { _ = conn.WriteJSON(v) }
		write(map[string]any{"type": "session.created", "session": map[string]any{"id": "s", "model": "gpt-realtime"}})
		if _, _, err := conn.ReadMessage(); err != nil { // session.update
			return
		}
		var got []string
		tag := ""
		read := func() string {
			ev := readClientEvent(conn)
			if ev == nil {
				return ""
			}
			typ, _ := ev["type"].(string)
			entry := typ
			if id, ok := ev["item_id"].(string); ok {
				entry += ":" + id
			}
			if resp, ok := ev["response"].(map[string]any); ok {
				if meta, ok := resp["metadata"].(map[string]any); ok {
					tag = fmt.Sprint(meta[metaKeyRequest])
				}
			}
			if typ == realtimeEventItemDelete {
				if rejectCode != "" {
					write(map[string]any{"type": "error", "error": map[string]any{
						"type": "invalid_request_error", "code": rejectCode,
						"message": "Error deleting item", "event_id": ev["event_id"]}})
				} else {
					write(map[string]any{"type": "conversation.item.deleted", "item_id": ev["item_id"]})
				}
			}
			if typ != "input_audio_buffer.commit" {
				got = append(got, entry)
			}
			return typ
		}
		for _, step := range steps {
			for _, ev := range step.send {
				write(withTag(ev, tag))
			}
			for step.await != "" {
				typ := read()
				if typ == "" {
					return
				}
				if typ == step.await {
					break
				}
			}
		}
		for {
			typ := read()
			if typ == "" {
				return
			}
			if typ == "input_audio_buffer.commit" {
				fs.client <- got
				break
			}
		}
		for _, ev := range after {
			write(ev)
		}
		<-r.Context().Done()
	}))
	t.Cleanup(fs.srv.Close)
	return fs
}

// withTag returns ev with a "$TAG" response metadata value replaced by tag.
func withTag(ev map[string]any, tag string) map[string]any {
	resp, ok := ev["response"].(map[string]any)
	if !ok {
		return ev
	}
	meta, ok := resp["metadata"].(map[string]any)
	if !ok {
		return ev
	}
	out := map[string]any{}
	for k, v := range meta {
		if v == "$TAG" {
			v = tag
		}
		out[k] = v
	}
	resp["metadata"] = out
	return ev
}

// forgetRun is a session on a forgetServer, with helpers to read its chunks.
type forgetRun struct {
	t       *testing.T
	ctx     context.Context
	session *RealtimeSession
	fs      *forgetServer
}

func startForgetRun(t *testing.T, fs *forgetServer) *forgetRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{endpoint: bargeInWSURL(fs.srv)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return &forgetRun{t: t, ctx: ctx, session: session, fs: fs}
}

// until reads chunks up to and including the first match accepts, and returns
// them.
func (r *forgetRun) until(match func(providers.StreamChunk) bool) []providers.StreamChunk {
	r.t.Helper()
	var out []providers.StreamChunk
	for {
		select {
		case c, ok := <-r.session.Response():
			require.True(r.t, ok, "Response() closed")
			out = append(out, c)
			if match(c) {
				return out
			}
		case <-r.ctx.Done():
			r.t.Fatal("timed out")
			return out
		}
	}
}

// clientEvents commits the audio buffer, which ends the server's recording,
// and returns the client events it read.
func (r *forgetRun) clientEvents() []string {
	r.t.Helper()
	require.NoError(r.t, r.session.CommitAudioBuffer())
	select {
	case got := <-r.fs.client:
		return got
	case <-r.ctx.Done():
		r.t.Fatal("the server never saw the commit")
		return nil
	}
}

// marker sends a session.update the server awaits, so a step follows what
// the client did before it.
func (r *forgetRun) marker() {
	r.t.Helper()
	require.NoError(r.t, r.session.SendSystemContext(r.ctx, "marker"))
}

func finished(c providers.StreamChunk) bool { return c.FinishReason != nil }

// finishedResponse matches the end of the response named id.
func finishedResponse(id string) func(providers.StreamChunk) bool {
	return func(c providers.StreamChunk) bool { return c.FinishReason != nil && c.Metadata[keyResponseID] == id }
}

func transcribed(c providers.StreamChunk) bool {
	_, ok := c.Metadata["transcription"]
	return ok
}

func committed(item string) map[string]any {
	return map[string]any{"type": "input_audio_buffer.committed", "item_id": item}
}

func responseCreated(id string) map[string]any {
	return map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress"}}
}

func responseDone(id, status string, output ...map[string]any) map[string]any {
	return map[string]any{"type": "response.done", "response": map[string]any{
		"id": id, "status": status, "output": output}}
}

func audioDelta(response, item string) map[string]any {
	return map[string]any{"type": "response.output_audio.delta", "item_id": item, "response_id": response,
		"content_index": 0, "delta": base64.StdEncoding.EncodeToString(make([]byte, 480))}
}

func assistantItem(id string) map[string]any {
	return map[string]any{"id": id, "type": "message", "role": "assistant"}
}

// A turn whose transcript arrives after its reply completed (the usual order
// on OpenAI Realtime) is forgotten at once: the caller's item and the reply's
// item are deleted. The transcript names the input ForgetTurn takes.
func TestRealtimeForget_CompletedReplyDeletesTheUserAndReplyItems(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{{send: []map[string]any{
		committed("user_1"), responseCreated("R1"), audioDelta("R1", "item_R1"),
		responseDone("R1", "completed", assistantItem("item_R1")),
		{"type": "conversation.item.input_audio_transcription.completed", "item_id": "user_1",
			"content_index": 0, "transcript": "tell me about Bruno"},
	}}}, "")
	run := startForgetRun(t, fs)
	chunks := run.until(transcribed)
	assert.Equal(t, "user_1", chunks[len(chunks)-1].Metadata[providers.InputItemIDKey])

	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))

	assert.Equal(t, []string{"conversation.item.delete:user_1", "conversation.item.delete:item_R1"},
		run.clientEvents())
}

// The caller's item is deleted at once; a reply still in progress once it
// ends, when its items are complete.
func TestRealtimeForget_ReplyInProgressIsDeletedWhenItEnds(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{
		{send: []map[string]any{committed("user_1"), responseCreated("R1"), audioDelta("R1", "item_R1")},
			await: realtimeEventSessionUpdate},
		{send: []map[string]any{responseDone("R1", "cancelled", assistantItem("item_R1"))}},
	}, "")
	run := startForgetRun(t, fs)
	run.until(func(c providers.StreamChunk) bool { return c.MediaData != nil })

	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))
	run.marker()
	run.until(finished)

	assert.Equal(t, []string{
		"conversation.item.delete:user_1", realtimeEventSessionUpdate, "conversation.item.delete:item_R1",
	}, run.clientEvents(), "the reply's items are deleted only once it ends")
}

// A tool turn whose follow-up reply is not created yet is forgotten with that
// follow-up when it ends; the reply that made the tool call stays, with its
// result.
func TestRealtimeForget_PendingFollowUpReplyIsDeletedWhenItEnds(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{
		{send: []map[string]any{
			committed("user_1"), responseCreated("R1"),
			responseDone("R1", "completed", map[string]any{"id": "fc_1", "type": "function_call", "call_id": "c1"}),
		}, await: realtimeEventSessionUpdate},
		{send: []map[string]any{
			responseCreated("R2"), audioDelta("R2", "item_R2"),
			responseDone("R2", "completed", assistantItem("item_R2")),
		}},
	}, "")
	run := startForgetRun(t, fs)
	run.until(finished)

	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))
	run.marker()
	run.until(finished)

	assert.Equal(t, []string{
		"conversation.item.delete:user_1", realtimeEventSessionUpdate, "conversation.item.delete:item_R2",
	}, run.clientEvents())
}

// A transcript blocked after the caller's next turn was committed, with its
// reply streaming, forgets the blocked input and its own reply: the next
// turn and its reply are left alone.
func TestRealtimeForget_LateTranscriptAfterTheNextCommitForgetsItsOwnReply(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{
		{send: []map[string]any{
			committed("user_1"), responseCreated("R1"), responseDone("R1", "completed", assistantItem("item_R1")),
			committed("user_2"), responseCreated("R2"), audioDelta("R2", "item_R2"),
		}, await: realtimeEventSessionUpdate},
		{send: []map[string]any{responseDone("R2", "completed", assistantItem("item_R2"))}},
	}, "")
	run := startForgetRun(t, fs)
	run.until(func(c providers.StreamChunk) bool { return c.MediaData != nil })

	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))
	run.marker()
	run.until(finished)

	assert.Equal(t, []string{
		"conversation.item.delete:user_1", "conversation.item.delete:item_R1", realtimeEventSessionUpdate,
	}, run.clientEvents())
}

// The caller talked over the reply to their first turn (barge-in), so both
// turns were committed before the first transcript. Forgetting the first
// deletes it and the reply it cut short, not the reply to the second.
func TestRealtimeForget_BargeInTwoCommitsBeforeTheTranscript(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{{send: []map[string]any{
		committed("user_1"), responseCreated("R1"),
		{"type": "input_audio_buffer.speech_started", "item_id": "user_2"},
		responseDone("R1", "cancelled", assistantItem("item_R1")),
		committed("user_2"), responseCreated("R2"), responseDone("R2", "completed", assistantItem("item_R2")),
	}}}, "")
	run := startForgetRun(t, fs)
	run.until(finishedResponse("R2"))

	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))

	assert.Equal(t, []string{"conversation.item.delete:user_1", "conversation.item.delete:item_R1"},
		run.clientEvents())
}

// A blocked turn that got no reply of its own: the spoken guardrail message,
// the next response, is the session's own and is never deleted, and the reply
// to the caller's next turn is not the blocked turn's either.
func TestRealtimeForget_AnnouncementIsNeverDeleted(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{
		{send: []map[string]any{committed("user_1")}, await: realtimeEventResponseCreate},
		{send: []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "R_ann", "status": "in_progress",
				"metadata": map[string]any{metaKeyRequest: "$TAG"}}},
			audioDelta("R_ann", "item_ann"), responseDone("R_ann", "completed", assistantItem("item_ann")),
			committed("user_2"), responseCreated("R2"), responseDone("R2", "completed", assistantItem("item_R2")),
		}},
	}, "")
	run := startForgetRun(t, fs)

	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))
	spoken, err := run.session.Announce(run.ctx, "We don't talk about him.")
	require.NoError(t, err)
	require.True(t, spoken)
	run.until(finishedResponse("R2"))

	assert.Equal(t, []string{"conversation.item.delete:user_1", realtimeEventResponseCreate},
		run.clientEvents())
}

// The server rejecting a delete because the item is already gone is not a
// session error: the next reply streams with no error chunk.
func TestRealtimeForget_ItemNotFoundIsNotAnError(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{{send: []map[string]any{
		committed("user_1"), responseCreated("R1"), responseDone("R1", "completed", assistantItem("item_R1")),
	}}}, errCodeItemNotFound,
		responseCreated("R2"), audioDelta("R2", "item_R2"), responseDone("R2", "completed", assistantItem("item_R2")))
	run := startForgetRun(t, fs)
	run.until(finished)

	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))
	assert.Equal(t, []string{"conversation.item.delete:user_1", "conversation.item.delete:item_R1"},
		run.clientEvents())

	chunks := run.until(finished)
	for _, c := range chunks {
		assert.NoError(t, c.Error, "an item-not-found rejection surfaced as a session error")
	}
	assert.Equal(t, "stop", *chunks[len(chunks)-1].FinishReason)
}

// Any other rejection of a delete is a session error: the item may still be
// in the model's context.
func TestRealtimeForget_OtherRejectionIsAnError(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{{send: []map[string]any{
		committed("user_1"), responseCreated("R1"), responseDone("R1", "completed", assistantItem("item_R1")),
	}}}, "server_error",
		responseCreated("R2"), responseDone("R2", "completed", assistantItem("item_R2")))
	run := startForgetRun(t, fs)
	run.until(finished)

	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))
	run.clientEvents()

	var errs []string
	for _, c := range run.until(finished) {
		if c.Error != nil {
			errs = append(errs, c.Error.Error())
		}
	}
	assert.Equal(t, []string{"server error: Error deleting item", "server error: Error deleting item"}, errs)
}

// A turn forgotten once is not deleted again.
func TestRealtimeForget_ForgottenTurnIsDeletedOnce(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{{send: []map[string]any{
		committed("user_1"), responseCreated("R1"), responseDone("R1", "completed", assistantItem("item_R1")),
	}}}, "")
	run := startForgetRun(t, fs)
	run.until(finished)

	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))
	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))

	assert.Equal(t, []string{"conversation.item.delete:user_1", "conversation.item.delete:item_R1"},
		run.clientEvents())
}

// Many deletes awaiting the server's answer at once (more than the truncates
// the session tracks) still tell an item-not-found rejection of the oldest
// from a session error.
func TestRealtimeForget_ManyPendingDeletesStayHarmless(t *testing.T) {
	const turns = maxRespGens // every reply still remembered: 2*turns deletes pending
	var send []map[string]any
	for i := 1; i <= turns; i++ {
		id := fmt.Sprintf("R%d", i)
		send = append(send, committed(fmt.Sprintf("user_%d", i)), responseCreated(id),
			responseDone(id, "completed", assistantItem("item_"+id)))
	}
	fs := newForgetServer(t, []forgetStep{{send: send}}, errCodeItemNotFound,
		responseCreated("R_last"), responseDone("R_last", "completed", assistantItem("item_last")))
	run := startForgetRun(t, fs)
	run.until(finishedResponse(fmt.Sprintf("R%d", turns)))

	for i := 1; i <= turns; i++ {
		require.NoError(t, run.session.ForgetTurn(run.ctx, fmt.Sprintf("user_%d", i)))
	}
	assert.Len(t, run.clientEvents(), 2*turns)
	for _, c := range run.until(finishedResponse("R_last")) {
		assert.NoError(t, c.Error)
	}
}

// A typed turn after the blocked spoken one is not a reply to it: forgetting
// the spoken turn deletes its own reply, and the typed turn's reply stays.
func TestRealtimeForget_TypedTurnsReplyIsNotTheSpokenTurns(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{
		{send: []map[string]any{
			committed("user_1"), responseCreated("R1"), responseDone("R1", "completed", assistantItem("item_R1")),
		}, await: realtimeEventResponseCreate},
		{send: []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "R2", "status": "in_progress",
				"metadata": map[string]any{metaKeyRequest: "$TAG"}}},
			responseDone("R2", "completed", assistantItem("item_R2")),
		}},
	}, "")
	run := startForgetRun(t, fs)
	run.until(finishedResponse("R1"))

	require.NoError(t, run.session.SendText(run.ctx, "what time is it"))
	run.until(finishedResponse("R2"))
	require.NoError(t, run.session.ForgetTurn(run.ctx, "user_1"))

	assert.Equal(t, []string{
		realtimeEventItemCreate, realtimeEventResponseCreate,
		"conversation.item.delete:user_1", "conversation.item.delete:item_R1",
	}, run.clientEvents())
}
