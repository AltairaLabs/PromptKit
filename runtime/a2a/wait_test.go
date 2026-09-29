package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// slowAgent answers SendMessage at once with a working task and reports it
// completed after doneAfter polls (never, if doneAfter < 0). It records what
// it was sent.
type slowAgent struct {
	doneAfter int

	mu        sync.Mutex
	sends     []SendMessageRequest
	polls     int
	cancelled []string
}

func (a *slowAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := decodeRPC(r)
	a.mu.Lock()
	defer a.mu.Unlock()
	working := &Task{ID: "slow-1", ContextID: "c", Status: TaskStatus{State: TaskStateWorking}}
	switch op, _, _ := LookupMethod(req.Method); op {
	case OpSendMessage:
		var params SendMessageRequest
		_ = json.Unmarshal(req.Params, &params)
		a.sends = append(a.sends, params)
		rpcResult(w, req.ID, SendMessageResponse{Task: working})
	case OpGetTask:
		a.polls++
		if a.doneAfter >= 0 && a.polls >= a.doneAfter {
			text := "finally"
			rpcResult(w, req.ID, &Task{ID: "slow-1", ContextID: "c",
				Status:    TaskStatus{State: TaskStateCompleted},
				Artifacts: []Artifact{{ArtifactID: "a", Parts: []Part{{Text: &text}}}}})
			return
		}
		rpcResult(w, req.ID, working)
	case OpCancelTask:
		var params CancelTaskRequest
		_ = json.Unmarshal(req.Params, &params)
		a.cancelled = append(a.cancelled, params.ID)
		rpcResult(w, req.ID, &Task{ID: params.ID, Status: TaskStatus{State: TaskStateCanceled}})
	default:
		rpcErrorResp(w, req.ID, ErrCodeMethodNotFound, "Method not found")
	}
}

func slowTool(url string, timeoutMs int) *tools.ToolDescriptor {
	return &tools.ToolDescriptor{Name: "a2a__slow__s", Mode: "a2a",
		A2AConfig: &tools.A2AConfig{AgentURL: url, SkillID: "s", TimeoutMs: timeoutMs}}
}

func TestExecutor_WaitsForAWorkingTask(t *testing.T) {
	agent := &slowAgent{doneAfter: 2}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	exec := NewExecutor(WithNoRetry())
	defer exec.Close()
	out, err := exec.Execute(context.Background(), slowTool(srv.URL, 0), json.RawMessage(`{"query":"q"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"response":"finally"}`, string(out),
		"a task that is still working when SendMessage returns must be waited for, not read empty")

	agent.mu.Lock()
	defer agent.mu.Unlock()
	require.Len(t, agent.sends, 1)
	require.NotNil(t, agent.sends[0].Configuration)
	assert.True(t, agent.sends[0].Configuration.ReturnImmediately, "the executor polls rather than holding a request open")
	assert.NotEmpty(t, agent.sends[0].Message.MessageID, "messageId is required by the spec")
	assert.Empty(t, agent.cancelled)
}

// The hang: the agent never finishes. The tool's timeout ends the wait, the
// call fails, and the task nobody will read is canceled.
func TestExecutor_HungTaskTimesOutAndIsCanceled(t *testing.T) {
	agent := &slowAgent{doneAfter: -1}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	exec := NewExecutor(WithNoRetry())
	defer exec.Close()
	begin := time.Now()
	_, err := exec.Execute(context.Background(), slowTool(srv.URL, 300), json.RawMessage(`{"query":"q"}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(begin), 3*time.Second)

	agent.mu.Lock()
	defer agent.mu.Unlock()
	assert.Equal(t, []string{"slow-1"}, agent.cancelled, "the abandoned task must be canceled")
	assert.Len(t, agent.sends, 1, "a timeout must not resend the message")
}

func TestWaitForTask(t *testing.T) {
	agent := &slowAgent{doneAfter: 1}
	srv := httptest.NewServer(agent)
	defer srv.Close()
	c := NewClient(srv.URL, WithProtocolVersion(ProtocolVersion10))

	done := &Task{ID: "x", Status: TaskStatus{State: TaskStateInputRequired}}
	got, err := c.WaitForTask(context.Background(), done)
	require.NoError(t, err)
	assert.Same(t, done, got, "an interrupted task is not polled")

	got, err = c.WaitForTask(context.Background(), &Task{ID: "slow-1", Status: TaskStatus{State: TaskStateWorking}})
	require.NoError(t, err)
	assert.Equal(t, TaskStateCompleted, got.Status.State)

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rpcErrorResp(w, decodeRPC(r).ID, ErrCodeTaskNotFound, "Task not found")
	}))
	defer failing.Close()
	start := &Task{ID: "gone", Status: TaskStatus{State: TaskStateWorking}}
	got, err = NewClient(failing.URL, WithProtocolVersion(ProtocolVersion10)).WaitForTask(context.Background(), start)
	assert.Error(t, err)
	assert.Same(t, start, got, "the last state seen comes back with the error")
}

func TestCancelAbandonedTask_SkipsWhatNeedsNoCancel(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		rpcErrorResp(w, decodeRPC(r).ID, ErrCodeTaskNotCancelable, "no")
	}))
	defer srv.Close()
	c := NewClient(srv.URL, WithProtocolVersion(ProtocolVersion10))

	cancelAbandonedTask(c, nil)
	cancelAbandonedTask(c, &Task{})
	cancelAbandonedTask(c, &Task{ID: "t", Status: TaskStatus{State: TaskStateCompleted}})
	assert.Zero(t, calls.Load())
	cancelAbandonedTask(c, &Task{ID: "t", Status: TaskStatus{State: TaskStateWorking}})
	assert.Equal(t, int32(1), calls.Load(), "a failed cancel is logged, not retried")
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestIsA2ARetryableError_Timeouts(t *testing.T) {
	assert.False(t, isA2ARetryableError(timeoutErr{}),
		"a response timeout may mean the turn started; resending would start another")
	assert.True(t, isA2ARetryableError(&net.OpError{Op: "dial", Err: timeoutErr{}}),
		"a connect timeout sent nothing and is safe to retry")
	assert.False(t, isA2ARetryableError(&net.OpError{Op: "read", Err: timeoutErr{}}))
	assert.True(t, isA2ARetryableError(&net.OpError{Op: "read", Err: errors.New("connection reset")}))
}
