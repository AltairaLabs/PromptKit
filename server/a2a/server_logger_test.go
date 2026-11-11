package a2aserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// logRecords is a slog handler that keeps every record it is given.
type logRecords struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *logRecords) Enabled(context.Context, slog.Level) bool { return true }
func (h *logRecords) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *logRecords) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logRecords) WithGroup(string) slog.Handler      { return h }

// find returns the first record whose message is msg, with its attributes.
func (h *logRecords) find(msg string) (slog.Level, map[string]string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message != msg {
			continue
		}
		attrs := map[string]string{}
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()
			return true
		})
		return r.Level, attrs, true
	}
	return 0, nil, false
}

// stdLogOutput captures what the standard log package writes during a test.
func stdLogOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	return &buf
}

// refusingStore refuses the move to one state with the error a test chooses,
// and is otherwise in-memory.
type refusingStore struct {
	*InMemoryTaskStore
	refuseTo a2a.TaskState
	err      error
}

func (s *refusingStore) SetState(taskID string, state a2a.TaskState, msg *a2a.Message) error {
	if state == s.refuseTo {
		return s.err
	}
	return s.InMemoryTaskStore.SetState(taskID, state, msg)
}

func sendThroughStore(t *testing.T, store TaskStore, opts ...Option) {
	t.Helper()
	srv, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		append([]Option{WithTaskStore(store)}, opts...)...)
	defer ts.Close()
	defer func() { _ = srv.Shutdown(context.Background()) }()
	resp := rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: userMessage("ctx-log")})
	require.Equal(t, 200, resp.StatusCode)
	_ = resp.Body.Close()
}

// A streamed turn's end-of-turn refusal is logged the same way.
func TestServer_StreamedTurnRefusalIsLoggedAtDebug(t *testing.T) {
	std := stdLogOutput(t)
	records := &logRecords{}
	store := &refusingStore{InMemoryTaskStore: NewInMemoryTaskStore(), refuseTo: a2a.TaskStateCompleted,
		err: fmt.Errorf("held: %w", ErrTransitionRefused)}
	_, ts := newTestServer(func(string) (Conversation, error) {
		return streamConv(StreamEvent{Kind: EventText, Text: "x"}), nil
	}, WithTaskStore(store), WithLogger(slog.New(records)))
	defer ts.Close()

	rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-log-stream")}))

	level, attrs, ok := records.find("a2a: task state change refused by the store")
	require.True(t, ok)
	assert.Equal(t, slog.LevelDebug, level)
	assert.Equal(t, string(a2a.TaskStateCompleted), attrs["state"])
	assert.Empty(t, std.String())
}

// A store that refuses the end-of-turn state on purpose (a task held for
// approval) is not reported as a failure: the server logs the refusal at
// debug level, on the embedder's logger, and nothing reaches the std logger.
func TestServer_IntendedRefusalIsLoggedAtDebugOnTheConfiguredLogger(t *testing.T) {
	std := stdLogOutput(t)
	records := &logRecords{}
	store := &refusingStore{InMemoryTaskStore: NewInMemoryTaskStore(), refuseTo: a2a.TaskStateCompleted,
		err: fmt.Errorf("task is held for approval: %w", ErrTransitionRefused)}

	sendThroughStore(t, store, WithLogger(slog.New(records)))

	level, attrs, ok := records.find("a2a: task state change refused by the store")
	require.True(t, ok, "the refusal is logged on the configured logger")
	assert.Equal(t, slog.LevelDebug, level)
	assert.Equal(t, string(a2a.TaskStateCompleted), attrs["state"])
	assert.NotEmpty(t, attrs["task_id"])
	assert.Contains(t, attrs["error"], "held for approval")
	assert.Empty(t, std.String(), "nothing goes to the std logger")
}

// Any other store error is a failure, logged as a warning.
func TestServer_StateWriteFailureIsAWarningOnTheConfiguredLogger(t *testing.T) {
	std := stdLogOutput(t)
	records := &logRecords{}
	store := &refusingStore{InMemoryTaskStore: NewInMemoryTaskStore(), refuseTo: a2a.TaskStateCompleted,
		err: errors.New("redis down")}

	sendThroughStore(t, store, WithLogger(slog.New(records)))

	level, attrs, ok := records.find("a2a: failed to set task state")
	require.True(t, ok)
	assert.Equal(t, slog.LevelWarn, level)
	assert.Equal(t, "redis down", attrs["error"])
	assert.Empty(t, std.String())
}

// Without WithLogger, the server logs on PromptKit's logger, so
// logger.SetLogger and logger.Configure apply to it.
func TestServer_LogsOnPromptKitsLoggerByDefault(t *testing.T) {
	records := &logRecords{}
	prev := logger.GetLogger()
	logger.SetLogger(slog.New(records))
	t.Cleanup(func() { logger.SetLogger(prev) })
	// SetLogger points the std logger at the new handler; capture it after,
	// so a std-log line from the server lands here and fails the test.
	std := stdLogOutput(t)
	store := &refusingStore{InMemoryTaskStore: NewInMemoryTaskStore(), refuseTo: a2a.TaskStateCompleted,
		err: errors.New("redis down")}

	sendThroughStore(t, store)

	_, _, ok := records.find("a2a: failed to set task state")
	assert.True(t, ok, "the server's log goes to PromptKit's logger")
	assert.Empty(t, std.String())
}

// failingAuth refuses every request.
type failingAuth struct{}

func (failingAuth) Authenticate(*http.Request) error { return errors.New("bad token") }

// failingCard cannot produce the agent card.
type failingCard struct{}

func (failingCard) AgentCard(*http.Request) (*a2a.AgentCard, error) {
	return nil, errors.New("config unreachable")
}

// A failed authentication is routine (any caller can send a bad token), so it
// is logged at debug, not at the default level.
func TestServer_AuthFailureIsLoggedAtDebug(t *testing.T) {
	records := &logRecords{}
	_, ts := newTestServer(nopOpener, WithAuthenticator(failingAuth{}), WithLogger(slog.New(records)))
	defer ts.Close()

	resp := rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: userMessage("ctx-auth")})
	_ = resp.Body.Close()

	level, attrs, ok := records.find("a2a: authentication failed")
	require.True(t, ok)
	assert.Equal(t, slog.LevelDebug, level)
	assert.Equal(t, "bad token", attrs["error"])
}

// A card the provider cannot build breaks discovery for every caller, so it is
// logged as an error.
func TestServer_AgentCardFailureIsLoggedAsAnError(t *testing.T) {
	records := &logRecords{}
	_, ts := newTestServer(nopOpener, WithCardProvider(failingCard{}), WithLogger(slog.New(records)))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/agent-card.json")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	level, attrs, ok := records.find("a2a: failed to get agent card")
	require.True(t, ok)
	assert.Equal(t, slog.LevelError, level)
	assert.Equal(t, "config unreachable", attrs["error"])
}

// A message for a task the store keeps from resuming, by policy, is answered
// as "not waiting for input" and the refusal is logged at debug, not reported
// as an internal error.
func TestServer_ResumeRefusedByTheStoreIsNotAnInternalError(t *testing.T) {
	records := &logRecords{}
	store := &refusingStore{InMemoryTaskStore: NewInMemoryTaskStore(), refuseTo: a2a.TaskStateWorking,
		err: fmt.Errorf("held for approval: %w", ErrTransitionRefused)}
	_, err := store.Create("held", "ctx-held")
	require.NoError(t, err)
	require.NoError(t, store.InMemoryTaskStore.SetState("held", a2a.TaskStateWorking, nil))
	require.NoError(t, store.InMemoryTaskStore.SetState("held", a2a.TaskStateInputRequired, nil))
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithTaskStore(store), WithLogger(slog.New(records)))
	defer ts.Close()

	msg := userMessage("ctx-held")
	msg.TaskID = "held"
	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: msg}))

	assert.Equal(t, a2a.ErrCodeUnsupportedOperation, e.Code)
	level, attrs, ok := records.find("a2a: task state change refused by the store")
	require.True(t, ok)
	assert.Equal(t, slog.LevelDebug, level)
	assert.Equal(t, "held", attrs["task_id"])
	_, _, internal := records.find("a2a: failed to resume task held")
	assert.False(t, internal, "a refusal is not an internal error")
}

// The in-process event bus reports a dropped event on the logger it was given.
func TestLocalTaskEvents_DropIsLoggedOnItsLogger(t *testing.T) {
	records := &logRecords{}
	bus := newLocalTaskEvents(func() *slog.Logger { return slog.New(records) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := bus.Subscribe(ctx, "slow")
	require.NoError(t, err)

	for i := 0; i < subscriberBuffer+1; i++ {
		require.NoError(t, bus.Publish(ctx, "slow", TaskEvent{}))
	}

	level, attrs, ok := records.find("a2a: dropped a task event for a slow subscriber (buffer full)")
	require.True(t, ok)
	assert.Equal(t, slog.LevelWarn, level)
	assert.Equal(t, "slow", attrs["task_id"])
}

// A call built without a logger (as tests and internal paths do) still logs,
// on PromptKit's logger, instead of panicking.
func TestRPCCall_WithoutALoggerFallsBackToPromptKits(t *testing.T) {
	records := &logRecords{}
	prev := logger.GetLogger()
	logger.SetLogger(slog.New(records))
	t.Cleanup(func() { logger.SetLogger(prev) })
	w := httptest.NewRecorder()
	call := &rpcCall{w: w, req: &a2a.JSONRPCRequest{ID: 1}}

	call.internalError("opening the conversation", errors.New("opener down"))

	assert.Equal(t, a2a.ErrCodeInternal, decodeError(t, w.Body.Bytes()).Code)
	_, attrs, ok := records.find("a2a: opening the conversation")
	require.True(t, ok)
	assert.Equal(t, "opener down", attrs["error"])
}
