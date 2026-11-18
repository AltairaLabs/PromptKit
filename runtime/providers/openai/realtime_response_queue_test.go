package openai

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// OpenAI Realtime allows one response at a time: a response.create while one
// is in progress is answered with conversation_already_has_active_response.
// Captured live (realtime-showcase text mode): a guardrail cut the reply and
// the session asked for the guardrail's message to be spoken (Announce); the
// caller typed their next line while that was still being spoken, and the
// session's response.create for it was rejected, which ended the session.

// oneResponseServer is a Realtime server that enforces one response at a
// time. A response.create it accepts starts a response (response.created
// echoing the request's metadata, one audio delta), which ends when the test
// sends a status on end. One received while a response is in progress is
// rejected as the server does. The test can push any server event, and have
// the next create rejected (rejectNext, an error code) or its error sent
// without an event_id (noEventID). It reports every client event it read.
type oneResponseServer struct {
	srv        *httptest.Server
	events     chan map[string]any
	end        chan string
	push       chan map[string]any
	rejectNext chan string
	noEventID  bool
	startOwn   bool // start a response of its own as the first create arrives
}

func newOneResponseServer(t *testing.T, configure ...func(*oneResponseServer)) *oneResponseServer {
	t.Helper()
	os := &oneResponseServer{events: make(chan map[string]any, 64), end: make(chan string, 8),
		push: make(chan map[string]any, 8), rejectNext: make(chan string, 1)}
	for _, c := range configure {
		c(os)
	}
	delta := base64.StdEncoding.EncodeToString(make([]byte, 480))
	os.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		n, active, startedOwn := 0, "", false
		start := func(meta any) {
			n++
			active = []string{"", "R1", "R2", "R3", "R4"}[min(n, 4)]
			resp := map[string]any{"id": active, "status": "in_progress"}
			if meta != nil {
				resp["metadata"] = meta
			}
			write(map[string]any{"type": "response.created", "response": resp})
			write(map[string]any{"type": "response.output_audio.delta", "item_id": "item_" + active,
				"response_id": active, "content_index": 0, "delta": delta})
		}
		reject := func(ev map[string]any, code, msg string) {
			e := map[string]any{"type": "invalid_request_error", "code": code, "message": msg}
			if !os.noEventID {
				e["event_id"] = ev["event_id"]
			}
			write(map[string]any{"type": "error", "error": e})
		}
		for {
			select {
			case ev, ok := <-client:
				if !ok {
					return
				}
				typ, _ := ev["type"].(string)
				os.events <- ev
				if typ != realtimeEventResponseCreate {
					continue
				}
				if os.startOwn && !startedOwn {
					// The server starts a response of its own (as server VAD
					// does) just as the client's create arrives: the client
					// sent it before it could know, and it is rejected.
					startedOwn = true
					start(nil)
				}
				select {
				case code := <-os.rejectNext:
					reject(ev, code, "rejected")
					continue
				default:
				}
				if active != "" {
					reject(ev, errCodeActiveResponse, "Conversation already has an active response in progress: "+active)
					continue
				}
				resp, _ := ev["response"].(map[string]any)
				start(resp["metadata"])
			case status := <-os.end:
				if active != "" {
					write(map[string]any{"type": "response.done", "response": map[string]any{"id": active, "status": status}})
					active = ""
				}
			case ev := <-os.push:
				write(ev)
			case <-r.Context().Done():
				return
			}
		}
	}))
	t.Cleanup(os.srv.Close)
	return os
}

// await returns the next client event the server read.
func (os *oneResponseServer) await(t *testing.T) map[string]any {
	t.Helper()
	select {
	case e := <-os.events:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no client event")
		return nil
	}
}

// awaitType returns the next client event, asserting its type.
func (os *oneResponseServer) awaitType(t *testing.T, typ string) map[string]any {
	t.Helper()
	ev := os.await(t)
	require.Equal(t, typ, ev["type"])
	return ev
}

// quiet asserts the server reads no client event for a while.
func (os *oneResponseServer) quiet(t *testing.T) {
	t.Helper()
	select {
	case e := <-os.events:
		t.Fatalf("unexpected client event %v", e["type"])
	case <-time.After(150 * time.Millisecond):
	}
}

// instructions is a response.create's instructions.
func instructions(ev map[string]any) string {
	resp, _ := ev["response"].(map[string]any)
	s, _ := resp["instructions"].(string)
	return s
}

// openOneResponseSession opens an audio session on os.
func openOneResponseSession(t *testing.T, os *oneResponseServer, configure ...func(*RealtimeSessionConfig)) *RealtimeSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	config := DefaultRealtimeSessionConfig()
	for _, c := range configure {
		c(&config)
	}
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{endpoint: bargeInWSURL(os.srv)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// responseChunks reads Response() until n responses have ended and returns
// every chunk, failing on an error chunk.
func responseChunks(t *testing.T, session *RealtimeSession, n int) []providers.StreamChunk {
	t.Helper()
	var out []providers.StreamChunk
	ends := 0
	deadline := time.After(3 * time.Second)
	for ends < n {
		select {
		case c, ok := <-session.Response():
			require.True(t, ok, "Response() closed")
			require.NoError(t, c.Error, "the session reported an error")
			out = append(out, c)
			if c.FinishReason != nil {
				ends++
			}
		case <-deadline:
			t.Fatalf("got %d response ends, want %d", ends, n)
		}
	}
	return out
}

// endsOf returns the response IDs of the chunks that end a response.
func endsOf(chunks []providers.StreamChunk) []string {
	var ids []string
	for _, c := range chunks {
		if c.FinishReason != nil {
			id, _ := c.Metadata[keyResponseID].(string)
			ids = append(ids, id)
		}
	}
	return ids
}

// announcementGens returns the generations whose chunks are marked as the
// reply an Announce asked for.
func announcementGens(chunks []providers.StreamChunk) map[int64]bool {
	gens := map[int64]bool{}
	for _, c := range chunks {
		if marked, _ := c.Metadata[providers.MetaKeyAnnouncement].(bool); marked {
			gens[c.Generation] = true
		}
	}
	return gens
}

const franceQuestion = "What is the capital of France?"

// Text typed while a response is in progress (here the guardrail's spoken
// message) is added at once; its response.create waits for that response to
// end, so the server never rejects it, and the reply to the text follows.
// The announcement's chunks are marked; the text reply's are not.
func TestRealtimeSendText_WhileAResponseIsInProgressWaitsForItsEnd(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os)

	announced, err := session.Announce(context.Background(), "Sorry, I can't say that word.")
	require.NoError(t, err)
	require.True(t, announced)
	first := os.awaitType(t, realtimeEventResponseCreate)
	assert.Contains(t, instructions(first), "Sorry, I can't say that word.")

	require.NoError(t, session.SendText(context.Background(), franceQuestion))
	os.awaitType(t, realtimeEventItemCreate) // the text is added at once
	os.quiet(t)                              // no response.create while the message is in progress

	os.end <- "completed" // the spoken message ends
	second := os.awaitType(t, realtimeEventResponseCreate)
	assert.Empty(t, instructions(second), "the text's own request, after the message")
	os.end <- "completed"
	chunks := responseChunks(t, session, 2)
	assert.Equal(t, []string{"R1", "R2"}, endsOf(chunks))
	assert.Equal(t, map[int64]bool{1: true}, announcementGens(chunks), "only the message is marked")
}

// A response.create sent but not yet answered with response.created counts
// as in progress: text typed then waits too.
func TestRealtimeSendText_AfterAnUnansweredResponseCreateWaits(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os)

	_, err := session.Announce(context.Background(), "Sorry, I can't say that word.")
	require.NoError(t, err)
	require.NoError(t, session.SendText(context.Background(), franceQuestion))
	assert.Contains(t, instructions(os.awaitType(t, realtimeEventResponseCreate)), "Sorry")
	os.awaitType(t, realtimeEventItemCreate)
	os.quiet(t)

	os.end <- "completed"
	assert.Empty(t, instructions(os.awaitType(t, realtimeEventResponseCreate)))
	os.end <- "completed"
	assert.Equal(t, []string{"R1", "R2"}, endsOf(responseChunks(t, session, 2)))
}

// Tool results sent while a response is in progress wait for it the same way.
func TestRealtimeSendToolResponses_WhileAResponseIsInProgressWaitsForItsEnd(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os)

	_, err := session.Announce(context.Background(), "One moment.")
	require.NoError(t, err)
	os.awaitType(t, realtimeEventResponseCreate)
	require.NoError(t, session.SendToolResponses(context.Background(),
		[]providers.ToolResponse{{ToolCallID: "call_1", Result: `{"temp":21}`}}))
	os.awaitType(t, realtimeEventItemCreate)
	os.quiet(t)

	os.end <- "completed"
	assert.Empty(t, instructions(os.awaitType(t, realtimeEventResponseCreate)))
	os.end <- "completed"
	assert.Len(t, endsOf(responseChunks(t, session, 2)), 2)
}

// An Announce asked for while typed text waits goes first, so the message
// follows the reply it explains; the typed text's reply comes after it.
func TestRealtimeAnnounce_GoesBeforeTheRequestsWaiting(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os)

	require.NoError(t, session.TriggerResponse(nil)) // R1, the reply a guardrail cuts
	os.awaitType(t, realtimeEventResponseCreate)
	require.NoError(t, session.SendText(context.Background(), franceQuestion))
	os.awaitType(t, realtimeEventItemCreate)
	_, err := session.Announce(context.Background(), "Sorry, I can't say that word.")
	require.NoError(t, err)
	os.quiet(t)

	os.end <- "cancelled"
	assert.Contains(t, instructions(os.awaitType(t, realtimeEventResponseCreate)), "Sorry", "the message first")
	os.end <- "completed"
	assert.Empty(t, instructions(os.awaitType(t, realtimeEventResponseCreate)), "then the typed text's reply")
	os.end <- "completed"
	chunks := responseChunks(t, session, 3)
	assert.Equal(t, []string{"R1", "R2", "R3"}, endsOf(chunks))
	assert.Equal(t, map[int64]bool{2: true}, announcementGens(chunks))
}

// The caller speaking over a reply (server VAD that starts responses) holds
// what is waiting: the interrupted reply's end sends nothing, so nothing talks
// over them. The server's own response to their speech reads the whole
// conversation, the typed text and tool results included, so the requests
// waiting are not sent after it as a second reply.
func TestRealtimeBargeIn_WhileARequestWaits(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os, func(c *RealtimeSessionConfig) {
		c.TurnDetection = &TurnDetectionConfig{Type: "server_vad", CreateResponse: true}
	})

	require.NoError(t, session.TriggerResponse(nil))
	os.awaitType(t, realtimeEventResponseCreate)
	require.NoError(t, session.SendText(context.Background(), franceQuestion))
	os.awaitType(t, realtimeEventItemCreate)
	require.NoError(t, session.SendToolResponses(context.Background(),
		[]providers.ToolResponse{{ToolCallID: "call_1", Result: `{"temp":21}`}}))
	os.awaitType(t, realtimeEventItemCreate)

	os.push <- map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "speech_1"}
	os.end <- "cancelled" // the server cancels the reply the caller spoke over
	for {                 // the session cuts the reply to what was heard
		select {
		case ev := <-os.events:
			require.NotEqual(t, realtimeEventResponseCreate, ev["type"], "nothing is requested over the caller")
			continue
		case <-time.After(150 * time.Millisecond):
		}
		break
	}
	os.push <- map[string]any{"type": "response.created", "response": map[string]any{"id": "R9", "status": "in_progress"}}
	os.push <- map[string]any{"type": "response.done", "response": map[string]any{"id": "R9", "status": "completed"}}
	os.quiet(t) // the server's response answered what was waiting

	var ends []string
	deadline := time.After(2 * time.Second)
	for len(ends) < 2 {
		select {
		case c := <-session.Response():
			require.NoError(t, c.Error)
			if c.FinishReason != nil {
				ends = append(ends, c.Metadata[keyResponseID].(string))
			}
		case <-deadline:
			t.Fatalf("ends %v", ends)
		}
	}
	assert.Equal(t, []string{"R1", "R9"}, ends)
}

// The caller's interruption (a guardrail cut) drops nothing waiting: typed
// text's reply is still asked for once the cut reply has ended.
func TestRealtimeInterrupt_WhileARequestWaits(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os)

	require.NoError(t, session.TriggerResponse(nil))
	os.awaitType(t, realtimeEventResponseCreate)
	require.NoError(t, session.SendText(context.Background(), franceQuestion))
	os.awaitType(t, realtimeEventItemCreate)
	require.NoError(t, session.InterruptResponse(context.Background(), 0))
	os.awaitType(t, realtimeEventResponseCancel)

	os.end <- "cancelled"
	for ev := os.await(t); ev["type"] != realtimeEventResponseCreate; ev = os.await(t) {
		require.Equal(t, realtimeEventItemTruncate, ev["type"], "only the cut of what was heard comes first")
	}
	os.end <- "completed"
	assert.Equal(t, []string{"R1", "R2"}, endsOf(responseChunks(t, session, 2)))
}

// A request the server rejects as busy because it had just started a
// response of its own (server VAD, as the create went out) is not a session
// error. It is sent again after that response: its input (here the typed
// text; tool results the same) may have reached the server after that
// response began, and so not be in it.
func TestRealtimeSendText_RejectedAsBusyByTheServersOwnResponse(t *testing.T) {
	os := newOneResponseServer(t, func(s *oneResponseServer) { s.startOwn = true })
	session := openOneResponseSession(t, os)

	require.NoError(t, session.SendText(context.Background(), franceQuestion))
	os.awaitType(t, realtimeEventItemCreate)
	os.awaitType(t, realtimeEventResponseCreate) // rejected: R1 is the server's
	os.quiet(t)
	os.end <- "completed"
	os.awaitType(t, realtimeEventResponseCreate) // sent again after R1
	os.end <- "completed"
	assert.Equal(t, []string{"R1", "R2"}, endsOf(responseChunks(t, session, 2)))
}

// vadConfig has server VAD start the response to the caller's speech.
func vadConfig(c *RealtimeSessionConfig) {
	c.TurnDetection = &TurnDetectionConfig{Type: "server_vad", CreateResponse: true}
}

// speechHeldRequest opens a server-VAD session, has the caller start
// speaking, and asks for a response, which is held for the server's.
func speechHeldRequest(t *testing.T, grace time.Duration) (*oneResponseServer, *RealtimeSession) {
	t.Helper()
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os, vadConfig)
	session.recvMu.Lock()
	session.speechGrace = grace
	session.recvMu.Unlock()
	os.push <- map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "speech_1"}
	require.Eventually(t, func() bool {
		session.recvMu.Lock()
		defer session.recvMu.Unlock()
		return session.speechTurn
	}, 2*time.Second, time.Millisecond)
	require.NoError(t, session.TriggerResponse(&ResponseConfig{Instructions: "held"}))
	os.quiet(t)
	return os, session
}

// Speech that gets no response does not hold the requests waiting for good:
// the hold ends when the input audio is cleared, on a server error while no
// request is pending (the server's response failed), and a grace after
// speech_stopped with no response.created.
func TestRealtimeSpeech_WithNoResponseReleasesTheWaitingRequests(t *testing.T) {
	t.Run("input audio cleared", func(t *testing.T) {
		os, _ := speechHeldRequest(t, time.Minute)
		os.push <- map[string]any{"type": "input_audio_buffer.cleared"}
		assert.Equal(t, "held", instructions(os.awaitType(t, realtimeEventResponseCreate)))
	})
	t.Run("server error", func(t *testing.T) {
		os, session := speechHeldRequest(t, time.Minute)
		os.push <- map[string]any{"type": "error", "error": map[string]any{"type": "server_error", "message": "boom"}}
		assert.Equal(t, "held", instructions(os.awaitType(t, realtimeEventResponseCreate)))
		select {
		case c := <-session.Response():
			require.Error(t, c.Error, "the server's error is still reported")
			assert.Contains(t, c.Error.Error(), "boom")
		case <-time.After(2 * time.Second):
			t.Fatal("the server's error was not reported")
		}
	})
	t.Run("grace after speech stopped", func(t *testing.T) {
		os, _ := speechHeldRequest(t, 300*time.Millisecond)
		os.push <- map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": "speech_1"}
		os.quiet(t) // within the grace
		assert.Equal(t, "held", instructions(os.awaitType(t, realtimeEventResponseCreate)))
	})
}

// A request asked for once the caller began to speak is not answered by the
// server's response to that speech (its input may have reached the server
// after it began): it is sent after it.
func TestRealtimeSpeech_RequestAskedDuringTheSpeechIsSentAfterTheServersResponse(t *testing.T) {
	os, session := speechHeldRequest(t, time.Minute)
	os.push <- map[string]any{"type": "response.created", "response": map[string]any{"id": "R9", "status": "in_progress"}}
	os.push <- map[string]any{"type": "response.done", "response": map[string]any{"id": "R9", "status": "completed"}}
	assert.Equal(t, "held", instructions(os.awaitType(t, realtimeEventResponseCreate)))
	os.end <- "completed"
	assert.Equal(t, []string{"R9", "R1"}, endsOf(responseChunks(t, session, 2)))
}

// A spoken message the server refuses is reported to the stage (an
// announcement-failed chunk), not as a session error, and the next request
// waiting goes.
func TestRealtimeAnnounce_RefusedIsReportedToTheStage(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os)

	require.NoError(t, session.TriggerResponse(nil))
	os.awaitType(t, realtimeEventResponseCreate)
	_, err := session.Announce(context.Background(), "Sorry, I can't say that word.")
	require.NoError(t, err)
	require.NoError(t, session.TriggerResponse(&ResponseConfig{Instructions: "after"}))
	os.rejectNext <- "invalid_value"
	os.end <- "cancelled"
	assert.Contains(t, instructions(os.awaitType(t, realtimeEventResponseCreate)), "Sorry")
	assert.Equal(t, "after", instructions(os.awaitType(t, realtimeEventResponseCreate)), "the next request goes")

	var failed bool
	deadline := time.After(2 * time.Second)
	for !failed {
		select {
		case c := <-session.Response():
			require.NoError(t, c.Error, "not a session error")
			failed, _ = c.Metadata[providers.MetaKeyAnnouncementFailed].(bool)
		case <-deadline:
			t.Fatal("the stage was not told the message is not coming")
		}
	}
}

// A request rejected as busy while no response is in progress on the
// session's side is sent again shortly, ahead of any asked for after it, so
// none is stranded or overtaken. An error naming no event_id counts as the
// pending request's.
func TestRealtimeSendText_RejectedAsBusyIsSentAgainInOrder(t *testing.T) {
	for _, noEventID := range []bool{false, true} {
		os := newOneResponseServer(t, func(s *oneResponseServer) { s.noEventID = noEventID })
		session := openOneResponseSession(t, os)
		os.rejectNext <- errCodeActiveResponse

		require.NoError(t, session.TriggerResponse(&ResponseConfig{Instructions: "first"}))
		assert.Equal(t, "first", instructions(os.awaitType(t, realtimeEventResponseCreate)))
		time.Sleep(createRetryDelay / 3) // the rejection is in; the retry is not due
		require.NoError(t, session.TriggerResponse(&ResponseConfig{Instructions: "second"}))
		assert.Equal(t, "first", instructions(os.awaitType(t, realtimeEventResponseCreate)), "sent again, first")
		os.end <- "completed"
		assert.Equal(t, "second", instructions(os.awaitType(t, realtimeEventResponseCreate)))
		os.end <- "completed"
		assert.Equal(t, []string{"R1", "R2"}, endsOf(responseChunks(t, session, 2)), "noEventID=%v", noEventID)
	}
}

// A request rejected for another reason is reported, and the next one
// waiting goes, with or without an event_id on the error.
func TestRealtimeTriggerResponse_OtherRejectionIsReportedAndTheNextGoes(t *testing.T) {
	for _, noEventID := range []bool{false, true} {
		os := newOneResponseServer(t, func(s *oneResponseServer) { s.noEventID = noEventID })
		session := openOneResponseSession(t, os)
		os.rejectNext <- "invalid_value"

		require.NoError(t, session.TriggerResponse(&ResponseConfig{Instructions: "bad"}))
		require.NoError(t, session.TriggerResponse(&ResponseConfig{Instructions: "good"}))
		assert.Equal(t, "bad", instructions(os.awaitType(t, realtimeEventResponseCreate)))
		assert.Equal(t, "good", instructions(os.awaitType(t, realtimeEventResponseCreate)), "noEventID=%v", noEventID)

		var errs []string
		deadline := time.After(2 * time.Second)
	read:
		for {
			select {
			case c := <-session.Response():
				if c.Error != nil {
					errs = append(errs, c.Error.Error())
					break read
				}
			case <-deadline:
				break read
			}
		}
		require.Len(t, errs, 1)
		assert.True(t, strings.Contains(errs[0], "rejected"), errs[0])
	}
}

// A request that cannot be sent is reported to its caller, and leaves
// nothing pending: the next request goes at once.
func TestRealtimeTriggerResponse_FailedSendLeavesNothingPending(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os)

	err := session.TriggerResponse(&ResponseConfig{ToolChoice: make(chan int)}) // cannot be encoded
	require.Error(t, err)
	require.NoError(t, session.SendText(context.Background(), franceQuestion))
	os.awaitType(t, realtimeEventItemCreate)
	os.awaitType(t, realtimeEventResponseCreate)
}

// stripIDs removes a client event's identifiers, which differ run to run:
// its event_id, and the request tag a response.create carries (dropping its
// response when nothing else is in it).
func stripIDs(ev map[string]any) {
	delete(ev, "event_id")
	resp, ok := ev["response"].(map[string]any)
	if !ok {
		return
	}
	if meta, ok := resp["metadata"].(map[string]any); ok {
		delete(meta, metaKeyRequest)
		if len(meta) == 0 {
			delete(resp, "metadata")
		}
	}
	if len(resp) == 0 {
		delete(ev, "response")
	}
}
