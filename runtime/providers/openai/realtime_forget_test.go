package openai

import (
	"context"
	"encoding/base64"
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
// the reply's items are deleted with conversation.item.delete, so a later
// turn cannot revive what was blocked.

// forgetStep is one step of a scripted Realtime server: the events it sends,
// then, with await set, the client events it reads until one of that type.
type forgetStep struct {
	send  []map[string]any
	await string
}

// forgetServer runs the steps, then reads client events until
// input_audio_buffer.commit and reports every client event it read after
// session.update, as "type" or "type:item_id". With rejectDeletes, each
// conversation.item.delete is answered with the error the server gives for an
// item it no longer has; otherwise with conversation.item.deleted. After the
// commit it sends the events in after.
type forgetServer struct {
	srv    *httptest.Server
	client chan []string
}

func newForgetServer(t *testing.T, steps []forgetStep, rejectDeletes bool, after ...map[string]any) *forgetServer {
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
			if typ == realtimeEventItemDelete {
				if rejectDeletes {
					write(map[string]any{"type": "error", "error": map[string]any{
						"type": "invalid_request_error", "code": "item_delete_invalid_item_id",
						"message": "Item with item_id not found", "event_id": ev["event_id"]}})
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
				write(ev)
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
// item are deleted.
func TestRealtimeForget_CompletedReplyDeletesTheUserAndReplyItems(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{{send: []map[string]any{
		committed("user_1"), responseCreated("R1"), audioDelta("R1", "item_R1"),
		responseDone("R1", "completed", assistantItem("item_R1")),
		{"type": "conversation.item.input_audio_transcription.completed", "item_id": "user_1",
			"content_index": 0, "transcript": "tell me about Bruno"},
	}}}, false)
	run := startForgetRun(t, fs)
	run.until(transcribed)

	require.NoError(t, run.session.ForgetTurn(run.ctx, 1))

	assert.Equal(t, []string{"conversation.item.delete:user_1", "conversation.item.delete:item_R1"},
		run.clientEvents())
}

// A reply still in progress is deleted once it ends, with the caller's item:
// its items are complete only then.
func TestRealtimeForget_ReplyInProgressIsDeletedWhenItEnds(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{
		{send: []map[string]any{committed("user_1"), responseCreated("R1"), audioDelta("R1", "item_R1")},
			await: realtimeEventSessionUpdate},
		{send: []map[string]any{responseDone("R1", "cancelled", assistantItem("item_R1"))}},
	}, false)
	run := startForgetRun(t, fs)
	run.until(func(c providers.StreamChunk) bool { return c.MediaData != nil })

	require.NoError(t, run.session.ForgetTurn(run.ctx, 1))
	run.marker()
	run.until(finished)

	assert.Equal(t, []string{
		realtimeEventSessionUpdate,
		"conversation.item.delete:user_1", "conversation.item.delete:item_R1",
	}, run.clientEvents(), "nothing is deleted before the reply ends")
}

// A tool turn's follow-up reply not created yet is forgotten when it ends:
// the caller's item and that reply go; the earlier reply that made the tool
// call stays, with its result.
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
	}, false)
	run := startForgetRun(t, fs)
	run.until(finished)

	require.NoError(t, run.session.ForgetTurn(run.ctx, 2))
	run.marker()
	run.until(finished)

	assert.Equal(t, []string{
		realtimeEventSessionUpdate,
		"conversation.item.delete:user_1", "conversation.item.delete:item_R2",
	}, run.clientEvents())
}

// A reply not created yet when the caller starts a new turn is not the
// blocked turn's: the caller's blocked item is deleted then, and the reply to
// the new turn is left alone.
func TestRealtimeForget_NewTurnLeavesItsReplyAlone(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{
		{send: []map[string]any{
			committed("user_1"), responseCreated("R1"), responseDone("R1", "completed", assistantItem("item_R1")),
		}, await: realtimeEventSessionUpdate},
		{send: []map[string]any{
			committed("user_2"), responseCreated("R2"), audioDelta("R2", "item_R2"),
			responseDone("R2", "completed", assistantItem("item_R2")),
		}},
	}, false)
	run := startForgetRun(t, fs)
	run.until(finished)

	require.NoError(t, run.session.ForgetTurn(run.ctx, 2))
	run.marker()
	run.until(finished)

	assert.Equal(t, []string{realtimeEventSessionUpdate, "conversation.item.delete:user_1"}, run.clientEvents())
}

// The server rejecting a delete (the item is already gone) is not a session
// error: the next reply streams with no error chunk.
func TestRealtimeForget_RejectedDeleteIsNotAnError(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{{send: []map[string]any{
		committed("user_1"), responseCreated("R1"), responseDone("R1", "completed", assistantItem("item_R1")),
	}}}, true,
		responseCreated("R2"), audioDelta("R2", "item_R2"), responseDone("R2", "completed", assistantItem("item_R2")))
	run := startForgetRun(t, fs)
	run.until(finished)

	require.NoError(t, run.session.ForgetTurn(run.ctx, 1))
	assert.Equal(t, []string{"conversation.item.delete:user_1", "conversation.item.delete:item_R1"},
		run.clientEvents())

	chunks := run.until(finished)
	for _, c := range chunks {
		assert.NoError(t, c.Error, "a rejected delete surfaced as a session error")
	}
	assert.Equal(t, "stop", *chunks[len(chunks)-1].FinishReason)
}

// A turn forgotten once is not deleted again.
func TestRealtimeForget_ForgottenTurnIsDeletedOnce(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{{send: []map[string]any{
		committed("user_1"), responseCreated("R1"), responseDone("R1", "completed", assistantItem("item_R1")),
	}}}, false)
	run := startForgetRun(t, fs)
	run.until(finished)

	require.NoError(t, run.session.ForgetTurn(run.ctx, 1))
	require.NoError(t, run.session.ForgetTurn(run.ctx, 1))

	assert.Equal(t, []string{"conversation.item.delete:user_1", "conversation.item.delete:item_R1"},
		run.clientEvents())
}
