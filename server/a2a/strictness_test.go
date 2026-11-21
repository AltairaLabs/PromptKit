package a2aserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// Strictness decisions of #2133: version resolution, the JSON-RPC envelope,
// error.data and status timestamps, checked on the wire.

// utcTimestamp is a ProtoJSON google.protobuf.Timestamp: RFC 3339 in UTC
// ("Z"), with 0, 3, 6 or 9 fractional digits.
var utcTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{3}|\.\d{6}|\.\d{9})?Z$`)

// statusOf returns the status object of a stream result in either version's
// shape: 1.0 wraps it ({"task"|"statusUpdate": ...}), 0.3 sends it bare.
func statusOf(result map[string]any) (map[string]any, bool) {
	for _, key := range []string{"task", "statusUpdate"} {
		if inner, ok := result[key].(map[string]any); ok {
			result = inner
		}
	}
	status, ok := result["status"].(map[string]any)
	return status, ok
}

// Every status a stream carries — the opening task and each status update —
// says when it was recorded, in UTC (A2A 1.0 TaskStatus.timestamp).
func TestStrictness_StreamedStatusesCarryUTCTimestamps(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
		return streamConv(StreamEvent{Kind: EventText, Text: "a"}), nil
	})
	defer ts.Close()

	for _, tc := range []struct{ version, method string }{
		{"1.0", a2a.MethodV1SendStreamingMessage},
		{"0.3", a2a.MethodV03SendStreamingMessage},
	} {
		before := time.Now().Add(-time.Second)
		_, events := rawStream(t, rawRPC(t, ts, tc.version, tc.method,
			a2a.SendMessageRequest{Message: userMessage("ctx-ts-" + tc.version)}))
		after := time.Now().Add(time.Second)

		statuses := 0
		for _, event := range events {
			status, ok := statusOf(event)
			if !ok {
				continue
			}
			statuses++
			raw, _ := status["timestamp"].(string)
			require.Regexp(t, utcTimestamp, raw, "%s status %v", tc.version, status)
			at, err := time.Parse(time.RFC3339Nano, raw)
			require.NoError(t, err)
			assert.True(t, at.After(before) && at.Before(after), "%s timestamp %s is not now", tc.version, raw)
		}
		assert.Equal(t, 2, statuses, "%s: the opening task and the final status update", tc.version)
	}
}

// zonedStore serves every task with a fixed status time in a non-UTC zone, as
// a store backed by a database in local time would.
type zonedStore struct {
	TaskStore
	at time.Time
}

func (z zonedStore) zoned(task *a2a.Task) *a2a.Task {
	at := z.at
	task.Status.Timestamp = &at
	return task
}

func (z zonedStore) Get(taskID string) (*a2a.Task, error) {
	task, err := z.TaskStore.Get(taskID)
	if err != nil {
		return nil, err
	}
	return z.zoned(task), nil
}

func (z zonedStore) List(contextID string, limit, offset int) ([]*a2a.Task, error) {
	tasks, err := z.TaskStore.List(contextID, limit, offset)
	for _, task := range tasks {
		z.zoned(task)
	}
	return tasks, err
}

// A status time is sent as a UTC instant whatever zone the store keeps it in.
func TestStrictness_TaskTimestampsAreSentInUTC(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 11, 21, 15, 30, 0, 0, ist), "2026-11-21T10:00:00Z"},
		{time.Date(2026, 11, 21, 15, 30, 0, 120_000_000, ist), "2026-11-21T10:00:00.120Z"},
		{time.Date(2026, 11, 21, 15, 30, 0, 123_456_700, ist), "2026-11-21T10:00:00.123456700Z"},
	} {
		inner := NewInMemoryTaskStore()
		_, err := inner.Create("t1", "ctx-zone")
		require.NoError(t, err)
		_, ts := newTestServer(nopOpener, WithTaskStore(zonedStore{TaskStore: inner, at: tc.at}))

		v1, _ := statusOf(rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: "t1"})))
		assert.Equal(t, tc.want, v1["timestamp"], "1.0 GetTask")

		v03, _ := statusOf(rawResult(t, rawRPC(t, ts, "0.3", a2a.MethodV03GetTask, a2a.GetTaskRequest{ID: "t1"})))
		assert.Equal(t, tc.want, v03["timestamp"], "0.3 tasks/get")

		list := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks, a2a.ListTasksRequest{ContextID: "ctx-zone"}))
		tasks, _ := list["tasks"].([]any)
		require.Len(t, tasks, 1)
		listed, _ := statusOf(tasks[0].(map[string]any))
		assert.Equal(t, tc.want, listed["timestamp"], "1.0 ListTasks")
		ts.Close()
	}
}

// postBody posts a raw JSON-RPC body with A2A-Version: 1.0 and returns the
// HTTP status and the decoded response envelope, id kept as its JSON text.
func postBody(t *testing.T, ts *httptest.Server, body string) (int, rawEnvelope) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/a2a", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(a2a.HeaderVersion, "1.0")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var env rawEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	return resp.StatusCode, env
}

// rawEnvelope is a JSON-RPC response with its id and error kept as sent.
type rawEnvelope struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Result  json.RawMessage   `json:"result"`
	Error   *a2a.JSONRPCError `json:"error"`
}

// A request object that is not a valid JSON-RPC 2.0 Request is Invalid
// Request (-32600), answered with its id when the id itself is valid
// (JSON-RPC 2.0 §4, §5, §5.1). Malformed JSON stays Parse error (-32700).
func TestStrictness_InvalidRequestEnvelopes(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	for _, tc := range []struct {
		name, body, id, message string
		code                    int
	}{
		{"jsonrpc missing", `{"id":1,"method":"GetTask","params":{"id":"x"}}`,
			"1", `Invalid Request: "jsonrpc" must be exactly "2.0"`, a2a.ErrCodeInvalidRequest},
		{"jsonrpc 1.0", `{"jsonrpc":"1.0","id":"a","method":"GetTask","params":{"id":"x"}}`,
			`"a"`, `Invalid Request: "jsonrpc" must be exactly "2.0"`, a2a.ErrCodeInvalidRequest},
		{"jsonrpc as a number", `{"jsonrpc":2.0,"id":2,"method":"GetTask","params":{"id":"x"}}`,
			"2", `Invalid Request: "jsonrpc" must be exactly "2.0"`, a2a.ErrCodeInvalidRequest},
		{"method missing", `{"jsonrpc":"2.0","id":3,"params":{"id":"x"}}`,
			"3", `Invalid Request: "method" must be a non-empty string`, a2a.ErrCodeInvalidRequest},
		{"method not a string", `{"jsonrpc":"2.0","id":4,"method":5}`,
			"4", `Invalid Request: "method" must be a non-empty string`, a2a.ErrCodeInvalidRequest},
		{"id an object", `{"jsonrpc":"2.0","id":{"n":1},"method":"GetTask","params":{"id":"x"}}`,
			"null", `Invalid Request: "id" must be a string, a number or null`, a2a.ErrCodeInvalidRequest},
		{"id a boolean", `{"jsonrpc":"2.0","id":true,"method":"GetTask","params":{"id":"x"}}`,
			"null", `Invalid Request: "id" must be a string, a number or null`, a2a.ErrCodeInvalidRequest},
		{"id an array", `{"jsonrpc":"2.0","id":[1],"method":"GetTask","params":{"id":"x"}}`,
			"null", `Invalid Request: "id" must be a string, a number or null`, a2a.ErrCodeInvalidRequest},
		{"method empty", `{"jsonrpc":"2.0","id":5,"method":""}`,
			"5", `Invalid Request: "method" must be a non-empty string`, a2a.ErrCodeInvalidRequest},
		{"null", `null`,
			"null", "Invalid Request: the request must be a JSON object", a2a.ErrCodeInvalidRequest},
		{"not an object", `"GetTask"`,
			"null", "Invalid Request: the request must be a JSON object", a2a.ErrCodeInvalidRequest},
		{"batch", `[{"jsonrpc":"2.0","id":1,"method":"GetTask","params":{"id":"x"}}]`,
			"null", "Invalid Request: batch requests are not supported", a2a.ErrCodeInvalidRequest},
		{"malformed JSON", `{"jsonrpc":"2.0",`,
			"null", "Parse error", a2a.ErrCodeParse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, env := postBody(t, ts, tc.body)
			assert.Equal(t, http.StatusOK, status)
			assert.Equal(t, "2.0", env.JSONRPC)
			assert.JSONEq(t, tc.id, string(env.ID))
			require.NotNil(t, env.Error)
			assert.Equal(t, tc.code, env.Error.Code)
			assert.Equal(t, tc.message, env.Error.Message)
		})
	}
}

// The response id is the request id exactly as sent: a number past 2^53 is
// not rounded through a float (JSON-RPC 2.0 §5: "the same as the value of
// the id member in the Request Object").
func TestStrictness_ResponseEchoesTheRequestIDExactly(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	for _, id := range []string{`9007199254740993`, `"req-1"`, `1.5`, `null`} {
		_, env := postBody(t, ts, `{"jsonrpc":"2.0","id":`+id+`,"method":"GetTask","params":{"id":"missing"}}`)
		assert.Equal(t, id, string(env.ID))
		require.NotNil(t, env.Error)
		assert.Equal(t, a2a.ErrCodeTaskNotFound, env.Error.Code)
	}
}

// wantErrorInfo is the google.rpc.ErrorInfo an A2A 1.0 error carries in
// error.data (A2A 1.0 §9.5).
func wantErrorInfo(reason string, metadata map[string]any) []any {
	info := map[string]any{
		"@type":  "type.googleapis.com/google.rpc.ErrorInfo",
		"reason": reason,
		"domain": "a2a-protocol.org",
	}
	if metadata != nil {
		info["metadata"] = metadata
	}
	return []any{info}
}

// A request is answered in the version its A2A-Version names, and a request
// without one is 0.3 (A2A 1.0 §3.6.1, §3.6.2). A method name that version
// does not have is refused, not served in the other version's shapes.
func TestStrictness_VersionAndMethodNameMustAgree(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()

	send := a2a.SendMessageRequest{Message: userMessage("ctx-agree"), Configuration: &a2a.SendMessageConfiguration{Blocking: true}}
	for _, tc := range []struct {
		name, version, method string
		code                  int
		message               string
		data                  any
	}{
		{"1.0 method without a header", "", a2a.MethodV1SendMessage, a2a.ErrCodeVersionNotSupported,
			"Version not supported: SendMessage is an A2A 1.0 method, and a request without A2A-Version is A2A 0.3; " +
				"send A2A-Version: 1.0",
			wantErrorInfo("VERSION_NOT_SUPPORTED", map[string]any{"requestedVersion": "0.3", "supportedVersions": "1.0,0.3"})},
		{"1.0 method with A2A-Version 0.3", "0.3", a2a.MethodV1GetTask, a2a.ErrCodeVersionNotSupported,
			"Version not supported: GetTask is an A2A 1.0 method, and the request names A2A 0.3; send A2A-Version: 1.0",
			wantErrorInfo("VERSION_NOT_SUPPORTED", map[string]any{"requestedVersion": "0.3", "supportedVersions": "1.0,0.3"})},
		{"0.3 method with A2A-Version 1.0", "1.0", a2a.MethodV03SendMessage, a2a.ErrCodeMethodNotFound,
			"Method not found: message/send is not an A2A 1.0 method (A2A 1.0 names it SendMessage)", nil},
		{"unsupported version", "2.0", a2a.MethodV1SendMessage, a2a.ErrCodeVersionNotSupported,
			`a2a: protocol version "2.0" is not supported (supported: 1.0, 0.3)`,
			wantErrorInfo("VERSION_NOT_SUPPORTED", map[string]any{"requestedVersion": "2.0", "supportedVersions": "1.0,0.3"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := rawRPC(t, ts, tc.version, tc.method, send)
			defer resp.Body.Close()
			var env struct {
				ID    any            `json:"id"`
				Error map[string]any `json:"error"`
			}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
			assert.Equal(t, float64(7), env.ID)
			assert.Equal(t, float64(tc.code), env.Error["code"])
			assert.Equal(t, tc.message, env.Error["message"])
			assert.Equal(t, tc.data, env.Error["data"])
		})
	}

	// The A2A-Version query parameter counts as the header (§3.6.1).
	body, err := json.Marshal(a2a.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: a2a.MethodV1SendMessage, Params: mustMarshal(t, send)})
	require.NoError(t, err)
	resp, err := http.Post(ts.URL+"/a2a?A2A-Version=1.0", "application/json", strings.NewReader(string(body)))
	require.NoError(t, err)
	result := rawResult(t, resp)
	assert.Equal(t, "TASK_STATE_COMPLETED", result["task"].(map[string]any)["status"].(map[string]any)["state"])

	// Each version's own method names, with its own header, are served.
	v03 := rawResult(t, rawRPC(t, ts, "", a2a.MethodV03SendMessage, send))
	assert.Equal(t, "task", v03["kind"])
	v03 = rawResult(t, rawRPC(t, ts, "0.3.0", a2a.MethodV03SendMessage, send))
	assert.Equal(t, "task", v03["kind"])
}

// An A2A error answered in 1.0 carries a google.rpc.ErrorInfo in error.data
// naming the error (A2A 1.0 §9.5), with the task it is about. In 0.3, whose
// clients read data as an object, error.data stays unset.
func TestStrictness_A2AErrorsCarryErrorInfo(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()

	done := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-info")}))
	doneID := done["task"].(map[string]any)["id"].(string)

	errorOf := func(version, method string, params any) map[string]any {
		resp := rawRPC(t, ts, version, method, params)
		defer resp.Body.Close()
		var env struct {
			Error map[string]any `json:"error"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
		require.NotNil(t, env.Error)
		return env.Error
	}

	for _, tc := range []struct {
		name, method string
		params       any
		code         int
		data         any
	}{
		{"task not found", a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: "nope"}, a2a.ErrCodeTaskNotFound,
			wantErrorInfo("TASK_NOT_FOUND", map[string]any{"taskId": "nope"})},
		{"cancel a missing task", a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: "gone"}, a2a.ErrCodeTaskNotFound,
			wantErrorInfo("TASK_NOT_FOUND", map[string]any{"taskId": "gone"})},
		{"task not cancelable", a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: doneID}, a2a.ErrCodeTaskNotCancelable,
			wantErrorInfo("TASK_NOT_CANCELABLE", map[string]any{"taskId": doneID})},
		{"subscribe to a missing task", a2a.MethodV1SubscribeToTask, a2a.SubscribeTaskRequest{ID: "absent"},
			a2a.ErrCodeTaskNotFound, wantErrorInfo("TASK_NOT_FOUND", map[string]any{"taskId": "absent"})},
		{"push notifications", a2a.MethodV1GetTaskPushNotificationConfig, map[string]any{"taskId": "x"},
			a2a.ErrCodePushNotificationNotSupported, wantErrorInfo("PUSH_NOTIFICATION_NOT_SUPPORTED", nil)},
		{"extended card", a2a.MethodV1GetExtendedAgentCard, map[string]any{},
			a2a.ErrCodeUnsupportedOperation, wantErrorInfo("UNSUPPORTED_OPERATION", nil)},
		{"content type", a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: userMessage("ctx-octet",
			a2a.Part{Raw: []byte{1, 2}, MediaType: "application/octet-stream"})},
			a2a.ErrCodeContentTypeNotSupported, wantErrorInfo("CONTENT_TYPE_NOT_SUPPORTED", nil)},
		{"invalid params carry no ErrorInfo", a2a.MethodV1GetTask, "not-an-object", a2a.ErrCodeInvalidParams, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := errorOf("1.0", tc.method, tc.params)
			assert.Equal(t, float64(tc.code), e["code"])
			assert.Equal(t, tc.data, e["data"])
		})
	}

	e := errorOf("", a2a.MethodV03GetTask, a2a.GetTaskRequest{ID: "nope"})
	assert.Equal(t, float64(a2a.ErrCodeTaskNotFound), e["code"])
	assert.NotContains(t, e, "data", "a 0.3 error has no 1.0 ErrorInfo array")
}

// requireUTCStatuses asserts that every status in events carries a UTC
// timestamp, and returns how many statuses there were.
func requireUTCStatuses(t *testing.T, events []map[string]any) int {
	t.Helper()
	n := 0
	for _, event := range events {
		if status, ok := statusOf(event); ok {
			n++
			assert.Regexp(t, utcTimestamp, status["timestamp"], "status %v", status)
		}
	}
	return n
}

// subscribed waits until taskID has a subscriber on srv's in-process bus.
func subscribed(t *testing.T, srv *Server, taskID string) {
	t.Helper()
	bus := srv.events.(*localTaskEvents)
	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subs[taskID]) > 0
	}, 2*time.Second, time.Millisecond)
}

// A subscriber's status updates carry timestamps too.
func TestStrictness_SubscribedStatusesCarryUTCTimestamps(t *testing.T) {
	release := make(chan struct{})
	mock := &mockConv{sendFunc: func(context.Context, any) (SendResult, error) {
		<-release
		return &mockSendResult{text: "done"}, nil
	}}
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := decodeTaskResult(t, a2aRPCRequest(t, ts, a2a.MethodV1SendMessage, a2a.SendMessageRequest{
		Message:       userMessage("ctx-sub-ts"),
		Configuration: &a2a.SendMessageConfiguration{ReturnImmediately: true},
	}).Result)

	got := make(chan []map[string]any, 1)
	go func() {
		_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SubscribeToTask, a2a.SubscribeTaskRequest{ID: task.ID}))
		got <- events
	}()
	subscribed(t, srv, task.ID)
	close(release)

	select {
	case events := <-got:
		assert.Equal(t, 2, requireUTCStatuses(t, events), "the opening task and the completion")
	case <-time.After(3 * time.Second):
		t.Fatal("subscription did not end when the task completed")
	}
}

// unstampedStore keeps no status times, as a minimal custom store might.
type unstampedStore struct{ TaskStore }

func (u unstampedStore) Get(taskID string) (*a2a.Task, error) {
	task, err := u.TaskStore.Get(taskID)
	if err == nil {
		task.Status.Timestamp = nil
	}
	return task, err
}

// When a canceled stream reports the task's stored status, it carries a
// timestamp even when the store keeps none.
func TestStrictness_CanceledStreamReportsATimestamp(t *testing.T) {
	streaming := make(chan struct{})
	mock := &mockStreamConv{streamFunc: func(ctx context.Context, _ any) <-chan StreamEvent {
		ch := make(chan StreamEvent)
		go func() {
			ch <- StreamEvent{Kind: EventText, Text: "partial"}
			close(streaming)
			<-ctx.Done()
		}()
		return ch
	}}
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil },
		WithTaskStore(unstampedStore{NewInMemoryTaskStore()}))
	defer ts.Close()

	got := make(chan []map[string]any, 1)
	go func() {
		_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
			a2a.SendMessageRequest{Message: userMessage("ctx-cancel-ts")}))
		got <- events
	}()
	<-streaming

	var taskID string
	require.Eventually(t, func() bool {
		srv.cancelsMu.Lock()
		defer srv.cancelsMu.Unlock()
		for id := range srv.cancels {
			taskID = id
		}
		return taskID != ""
	}, 2*time.Second, time.Millisecond)
	_ = rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: taskID}))

	select {
	case events := <-got:
		require.NotEmpty(t, events)
		last, ok := statusOf(events[len(events)-1])
		require.True(t, ok, "the stream ends with a status")
		assert.Equal(t, "TASK_STATE_CANCELED", last["state"])
		assert.Regexp(t, utcTimestamp, last["timestamp"])
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not end after its task was canceled")
	}
}

// sendWithVersionLines posts a blocking SendMessage with one A2A-Version
// header line per entry of lines, to path (which may carry a query).
func sendWithVersionLines(t *testing.T, ts *httptest.Server, path string, lines ...string) map[string]any {
	t.Helper()
	body, err := json.Marshal(a2a.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: a2a.MethodV1SendMessage,
		Params: mustMarshal(t, a2a.SendMessageRequest{Message: userMessage("ctx-lines")})})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(string(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	for _, line := range lines {
		req.Header.Add(a2a.HeaderVersion, line)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var env map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	return env
}

// A blank A2A-Version is no version: the query parameter is read next, and
// with neither the request is 0.3. Every header line counts, and lines that
// name different versions are refused.
func TestStrictness_VersionHeaderLines(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()

	served := func(env map[string]any) string {
		result, _ := env["result"].(map[string]any)
		task, _ := result["task"].(map[string]any)
		status, _ := task["status"].(map[string]any)
		state, _ := status["state"].(string)
		return state
	}
	errorMessage := func(env map[string]any) string {
		e, _ := env["error"].(map[string]any)
		msg, _ := e["message"].(string)
		return msg
	}

	assert.Equal(t, "TASK_STATE_COMPLETED", served(sendWithVersionLines(t, ts, "/a2a?A2A-Version=1.0", "  ")),
		"a blank header falls through to the query parameter")
	assert.Equal(t, "TASK_STATE_COMPLETED", served(sendWithVersionLines(t, ts, "/a2a", " ", " 1.0 ")),
		"a blank first line does not hide the next one")
	assert.Equal(t, "TASK_STATE_COMPLETED", served(sendWithVersionLines(t, ts, "/a2a", "1.0", "1.0.2")),
		"lines naming the same Major.Minor agree")

	assert.Equal(t, "Version not supported: SendMessage is an A2A 1.0 method, and a request without "+
		"A2A-Version is A2A 0.3; send A2A-Version: 1.0", errorMessage(sendWithVersionLines(t, ts, "/a2a", " \t")),
		"a blank header is no header")

	conflict := sendWithVersionLines(t, ts, "/a2a", "1.0", "0.3")
	e, _ := conflict["error"].(map[string]any)
	assert.Equal(t, float64(a2a.ErrCodeVersionNotSupported), e["code"])
	assert.Equal(t, `a2a: A2A-Version names more than one version ("1.0", "0.3")`, e["message"])
}

// recordedAt is the status time storedAtStore records, far from now so an
// emitted status stamped with the current time cannot match it by chance.
var recordedAt = time.Date(2001, 2, 3, 4, 5, 6, 789_000_000, time.UTC)

// storedAtStore is a store whose every status was recorded at recordedAt.
func storedAtStore() TaskStore {
	return zonedStore{TaskStore: NewInMemoryTaskStore(), at: recordedAt}
}

// The status a stream ends with, and the one a subscriber sees, is the
// status the store recorded: GetTask returns the same timestamp.
func TestStrictness_EmittedStatusIsTheStoredStatus(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
		return streamConv(StreamEvent{Kind: EventText, Text: "a"}), nil
	}, WithTaskStore(storedAtStore()))
	defer ts.Close()

	_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-stored")}))
	require.NotEmpty(t, events)
	final, ok := statusOf(events[len(events)-1])
	require.True(t, ok)
	opening := events[0]["task"].(map[string]any)

	stored, _ := statusOf(rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask,
		a2a.GetTaskRequest{ID: opening["id"].(string)})))
	assert.Equal(t, "TASK_STATE_COMPLETED", stored["state"])
	assert.Equal(t, "2001-02-03T04:05:06.789Z", stored["timestamp"])
	assert.Equal(t, stored["timestamp"], final["timestamp"])
}

// A subscriber's completion status is the stored one too.
func TestStrictness_SubscribedStatusIsTheStoredStatus(t *testing.T) {
	release := make(chan struct{})
	mock := &mockConv{sendFunc: func(context.Context, any) (SendResult, error) {
		<-release
		return &mockSendResult{text: "done"}, nil
	}}
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil }, WithTaskStore(storedAtStore()))
	defer ts.Close()

	task := decodeTaskResult(t, a2aRPCRequest(t, ts, a2a.MethodV1SendMessage, a2a.SendMessageRequest{
		Message:       userMessage("ctx-sub-stored"),
		Configuration: &a2a.SendMessageConfiguration{ReturnImmediately: true},
	}).Result)
	got := make(chan []map[string]any, 1)
	go func() {
		_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SubscribeToTask, a2a.SubscribeTaskRequest{ID: task.ID}))
		got <- events
	}()
	subscribed(t, srv, task.ID)
	close(release)

	var events []map[string]any
	select {
	case events = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("subscription did not end when the task completed")
	}
	final, ok := statusOf(events[len(events)-1])
	require.True(t, ok)
	stored, _ := statusOf(rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: task.ID})))
	assert.Equal(t, "TASK_STATE_COMPLETED", final["state"])
	assert.Equal(t, "2001-02-03T04:05:06.789Z", stored["timestamp"])
	assert.Equal(t, stored["timestamp"], final["timestamp"])
}

// movedOnStore reports every finished task as canceled, as a store does when
// a CancelTask lands between a turn's write and its read-back.
type movedOnStore struct{ TaskStore }

func (m movedOnStore) Get(taskID string) (*a2a.Task, error) {
	task, err := m.TaskStore.Get(taskID)
	if err == nil && task.Status.State == a2a.TaskStateCompleted {
		task.Status = a2a.TaskStatus{State: a2a.TaskStateCanceled, Timestamp: &recordedAt}
	}
	return task, err
}

// When the stored status has already moved on, the stream still reports the
// state this turn recorded, stamped with the current time — not a later
// state that belongs to whoever moved it.
func TestStrictness_EmittedStatusWhenTheTaskMovedOn(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
		return streamConv(StreamEvent{Kind: EventText, Text: "a"}), nil
	}, WithTaskStore(movedOnStore{NewInMemoryTaskStore()}))
	defer ts.Close()

	_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-moved")}))
	require.NotEmpty(t, events)
	final, ok := statusOf(events[len(events)-1])
	require.True(t, ok)
	assert.Equal(t, "TASK_STATE_COMPLETED", final["state"])
	assert.Regexp(t, utcTimestamp, final["timestamp"])
	assert.NotEqual(t, "2001-02-03T04:05:06.789Z", final["timestamp"])
}
