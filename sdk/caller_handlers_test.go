package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// handlerMember is the caller the handler tests open conversations for.
var handlerMember = types.Caller{ID: "member@example.com", Claims: map[string]any{"tenant": "acme"}}

// recordedCalls collects what tool handlers receive.
type recordedCalls[T any] struct {
	mu    sync.Mutex
	calls []T
}

func (r *recordedCalls[T]) add(call T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *recordedCalls[T]) seen() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]T(nil), r.calls...)
}

// handlerConv opens a conversation over callerToolsPack for handlerMember,
// unary or ASM duplex.
func handlerConv(t *testing.T, duplex bool) *Conversation {
	t.Helper()
	pack := createTestPackFile(t, callerToolsPack)
	var conv *Conversation
	var err error
	if duplex {
		prov := &callerStreamProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false)}
		conv, err = OpenDuplex(pack, "chat", WithProvider(prov), WithSkipSchemaValidation(),
			WithStreamingConfig(convDuplexAudio), WithCaller(handlerMember))
	} else {
		conv, err = Open(pack, "chat", WithProvider(newCallerToolProvider()), WithSkipSchemaValidation(),
			WithCaller(handlerMember))
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// handlerTurn runs one turn that calls lookup with who "ada". In duplex it
// waits until done reports true.
func handlerTurn(t *testing.T, conv *Conversation, duplex bool, done func() bool) {
	t.Helper()
	if !duplex {
		_, err := conv.Send(context.Background(), "ada")
		require.NoError(t, err)
		return
	}
	convDrain(t, conv)
	require.NoError(t, conv.SendText(context.Background(), "ada"))
	require.Eventually(t, done, 5*time.Second, 5*time.Millisecond)
}

// lookupToolResult returns the content of the tool result conv holds for the
// lookup call.
func lookupToolResult(t *testing.T, conv *Conversation) string {
	t.Helper()
	var content string
	require.Eventually(t, func() bool {
		for _, m := range conv.Messages(context.Background()) {
			if m.ToolResult != nil && m.ToolResult.ID == callerCallID {
				content = m.GetContent()
				return true
			}
		}
		return false
	}, 5*time.Second, 5*time.Millisecond)
	return content
}

// An OnToolCall handler, registered after Open, receives the whole call: the
// tool's name, the provider's call ID, the parsed arguments and the caller;
// its return value is the call's result.
func TestOnToolCall_HandlerReceivesCall(t *testing.T) {
	for name, duplex := range map[string]bool{"unary": false, "duplex-asm": true} {
		t.Run(name, func(t *testing.T) {
			conv := handlerConv(t, duplex)
			rec := &recordedCalls[ToolCall]{}
			conv.OnToolCall("lookup", func(_ context.Context, call ToolCall) (any, error) {
				rec.add(call)
				return map[string]any{"found": call.Args["who"], "for": call.Caller.ID}, nil
			})

			handlerTurn(t, conv, duplex, func() bool { return len(rec.seen()) > 0 })

			require.Len(t, rec.seen(), 1)
			assert.Equal(t, ToolCall{
				Name: "lookup", CallID: callerCallID, Args: map[string]any{"who": "ada"}, Caller: handlerMember,
			}, rec.seen()[0])
			if !duplex {
				assert.JSONEq(t, `{"found":"ada","for":"member@example.com"}`, lookupToolResult(t, conv))
			}
		})
	}
}

// A client tool handler's request carries the call's ID and the caller.
func TestOnClientTool_RequestCarriesCaller(t *testing.T) {
	for name, duplex := range map[string]bool{"unary": false, "duplex-asm": true} {
		t.Run(name, func(t *testing.T) {
			conv := handlerConv(t, duplex)
			rec := &recordedCalls[ClientToolRequest]{}
			conv.OnClientTool("lookup", func(_ context.Context, req ClientToolRequest) (any, error) {
				rec.add(req)
				return map[string]any{"lat": 1}, nil
			})

			handlerTurn(t, conv, duplex, func() bool { return len(rec.seen()) > 0 })

			require.Len(t, rec.seen(), 1)
			req := rec.seen()[0]
			assert.Equal(t, "lookup", req.ToolName)
			assert.Equal(t, callerCallID, req.CallID)
			assert.Equal(t, map[string]any{"who": "ada"}, req.Args)
			assert.Equal(t, handlerMember, req.Caller)
		})
	}
}

// approvalCheck holds every call for approval.
func approvalCheck(context.Context, string, map[string]any) sdktools.PendingResult {
	return sdktools.PendingResult{Reason: "needs_approval", Message: "approve the lookup"}
}

// approvalCallCheck is approvalCheck for an OnToolAsyncCall check.
func approvalCallCheck(context.Context, ToolCall) sdktools.PendingResult {
	return sdktools.PendingResult{Reason: "needs_approval", Message: "approve the lookup"}
}

// An OnToolAsyncCall check receives the same call its exec does: the tool's
// name, the provider's call ID, the parsed arguments and the caller.
func TestOnToolAsyncCall_CheckReceivesCall(t *testing.T) {
	for name, duplex := range map[string]bool{"unary": false, "duplex-asm": true} {
		t.Run(name, func(t *testing.T) {
			conv := handlerConv(t, duplex)
			checked := &recordedCalls[ToolCall]{}
			ran := &recordedCalls[ToolCall]{}
			conv.OnToolAsyncCall("lookup", func(_ context.Context, call ToolCall) sdktools.PendingResult {
				checked.add(call)
				return sdktools.PendingResult{}
			}, func(_ context.Context, call ToolCall) (any, error) {
				ran.add(call)
				return "ok", nil
			})

			handlerTurn(t, conv, duplex, func() bool { return len(ran.seen()) > 0 })

			want := ToolCall{Name: "lookup", CallID: callerCallID, Args: map[string]any{"who": "ada"}, Caller: handlerMember}
			require.Len(t, checked.seen(), 1)
			assert.Equal(t, want, checked.seen()[0])
			require.Len(t, ran.seen(), 1)
			assert.Equal(t, want, ran.seen()[0])
		})
	}
}

// A call an OnToolAsyncCall check holds does not run until it is approved;
// approved, its exec receives the held call's provider call ID and the
// caller, and its result is the resolution's.
func TestOnToolAsyncCall_ApprovedExecutionCarriesCaller(t *testing.T) {
	for name, duplex := range map[string]bool{"unary": false, "duplex-asm": true} {
		t.Run(name, func(t *testing.T) {
			conv := handlerConv(t, duplex)
			rec := &recordedCalls[ToolCall]{}
			conv.OnToolAsyncCall("lookup", approvalCallCheck, func(_ context.Context, call ToolCall) (any, error) {
				rec.add(call)
				return map[string]any{"refunded": call.Caller.ID}, nil
			})
			ctx := context.Background()

			held := func() bool {
				pending, err := conv.PendingTools(ctx)
				return err == nil && len(pending) == 1
			}
			handlerTurn(t, conv, duplex, held)
			pending, err := conv.PendingTools(ctx)
			require.NoError(t, err)
			require.Len(t, pending, 1)
			assert.Equal(t, callerCallID, pending[0].ID)
			assert.Empty(t, rec.seen(), "a held call must not run before it is approved")

			resolution, err := conv.ResolveTool(ctx, pending[0].ID)
			require.NoError(t, err)
			require.NoError(t, resolution.Error)
			assert.JSONEq(t, `{"refunded":"member@example.com"}`, string(resolution.ResultJSON))
			if duplex {
				require.NoError(t, conv.ContinueDuplex(ctx))
			} else {
				_, err = conv.Continue(ctx)
				require.NoError(t, err)
			}

			require.Len(t, rec.seen(), 1)
			assert.Equal(t, ToolCall{
				Name: "lookup", CallID: callerCallID, Args: map[string]any{"who": "ada"}, Caller: handlerMember,
			}, rec.seen()[0])
		})
	}
}

// A call an OnToolAsyncCall check lets through runs at once, with the call's
// ID and the caller.
func TestOnToolAsyncCall_UnheldCallCarriesCaller(t *testing.T) {
	for name, duplex := range map[string]bool{"unary": false, "duplex-asm": true} {
		t.Run(name, func(t *testing.T) {
			conv := handlerConv(t, duplex)
			rec := &recordedCalls[ToolCall]{}
			proceed := func(context.Context, ToolCall) sdktools.PendingResult {
				return sdktools.PendingResult{}
			}
			conv.OnToolAsyncCall("lookup", proceed, func(_ context.Context, call ToolCall) (any, error) {
				rec.add(call)
				return "ok", nil
			})

			handlerTurn(t, conv, duplex, func() bool { return len(rec.seen()) > 0 })

			require.Len(t, rec.seen(), 1)
			assert.Equal(t, ToolCall{
				Name: "lookup", CallID: callerCallID, Args: map[string]any{"who": "ada"}, Caller: handlerMember,
			}, rec.seen()[0])
		})
	}
}

// An OnToolAsyncCall registered over an OnToolAsync for the same tool
// replaces its execution handler, on the held and the unheld path alike.
func TestOnToolAsyncCall_ReplacesOnToolAsync(t *testing.T) {
	for name, duplex := range map[string]bool{"unary": false, "duplex-asm": true} {
		t.Run(name, func(t *testing.T) {
			conv := handlerConv(t, duplex)
			var oldRan atomic.Bool
			proceed := func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{} }
			conv.OnToolAsync("lookup", proceed, func(map[string]any) (any, error) {
				oldRan.Store(true)
				return "old", nil
			})
			rec := &recordedCalls[ToolCall]{}
			conv.OnToolAsyncCall("lookup", func(context.Context, ToolCall) sdktools.PendingResult {
				return sdktools.PendingResult{}
			}, func(_ context.Context, call ToolCall) (any, error) {
				rec.add(call)
				return "new", nil
			})

			handlerTurn(t, conv, duplex, func() bool { return len(rec.seen()) > 0 || oldRan.Load() })

			assert.False(t, oldRan.Load(), "the replaced handler must not run")
			require.Len(t, rec.seen(), 1)
			assert.Equal(t, handlerMember, rec.seen()[0].Caller)
		})
	}
}

// An OnToolAsync registered over an OnToolCall for the same tool is what an
// approved held call runs.
func TestOnToolAsync_ReplacesCallHandlerOnApproval(t *testing.T) {
	conv := handlerConv(t, false)
	var callRan atomic.Bool
	conv.OnToolCall("lookup", func(context.Context, ToolCall) (any, error) {
		callRan.Store(true)
		return "call", nil
	})
	conv.OnToolAsyncWithContext("lookup", approvalCheck, func(map[string]any) (any, error) {
		return "async", nil
	})
	ctx := context.Background()
	_, err := conv.Send(ctx, "ada")
	require.NoError(t, err)

	resolution, err := conv.ResolveTool(ctx, callerCallID)
	require.NoError(t, err)
	assert.JSONEq(t, `"async"`, string(resolution.ResultJSON))
	assert.False(t, callRan.Load(), "the replaced call handler must not run")
}

// An OnToolHTTP registered over a CallExecutor for the same tool replaces it.
func TestCaller_OnToolHTTPReplacesCallExecutor(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"via":"http"}`))
	}))
	t.Cleanup(srv.Close)
	conv := handlerConv(t, false)
	exec := &callerExecutor{}
	conv.OnToolExecutor("lookup", exec)
	conv.OnToolHTTP("lookup", sdktools.NewHTTPToolConfig(srv.URL, sdktools.WithMethod(http.MethodPost)))

	_, err := conv.Send(context.Background(), "ada")
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load(), "the later OnToolHTTP runs the call")
	assert.Empty(t, exec.seen(), "the replaced executor must not run")
}

// A host running a client tool through the conversation's registry gives the
// handler the call's ID and caller; a call without an ID takes the one on the
// context.
func TestOnClientTool_RegistryCallCarriesCaller(t *testing.T) {
	conv := handlerConv(t, false)
	rec := &recordedCalls[ClientToolRequest]{}
	conv.OnClientTool("lookup", func(_ context.Context, req ClientToolRequest) (any, error) {
		rec.add(req)
		return "here", nil
	})
	ctx := context.Background()

	res, err := conv.ToolRegistry().ExecuteCall(ctx, tools.ToolCall{
		Name: "lookup", Args: json.RawMessage(`{"who":"ada"}`), ID: "host-call", Caller: handlerMember,
	})
	require.NoError(t, err)
	assert.Empty(t, res.Error)
	assert.JSONEq(t, `"here"`, string(res.Result))

	_, err = conv.ToolRegistry().Execute(tools.WithCallID(ctx, "ctx-call"), "lookup", json.RawMessage(`{"who":"bo"}`))
	require.NoError(t, err)

	reqs := rec.seen()
	require.Len(t, reqs, 2)
	assert.Equal(t, "host-call", reqs[0].CallID)
	assert.Equal(t, handlerMember, reqs[0].Caller)
	assert.Equal(t, "ctx-call", reqs[1].CallID)
	assert.Equal(t, types.Caller{}, reqs[1].Caller)
}

// An OnToolAsync registered over a CallExecutor leaves the executor running a
// call the check lets through, and the executor still receives the call's ID
// and the caller.
func TestOnToolAsync_UnheldCallKeepsCallExecutorCaller(t *testing.T) {
	conv := handlerConv(t, false)
	exec := &callerExecutor{}
	conv.OnToolExecutor("lookup", exec)
	conv.OnToolAsync("lookup", func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{} },
		func(map[string]any) (any, error) { return "async", nil })

	_, err := conv.Send(context.Background(), "ada")
	require.NoError(t, err)

	calls := exec.seen()
	require.Len(t, calls, 1)
	assert.Equal(t, callerCallID, calls[0].ID)
	assert.Equal(t, handlerMember, calls[0].Caller)
}

// While an OnToolAsyncCall check is active, a later handler for the same tool
// does not unregister the approval: an approved held call runs the async
// exec, with the caller.
func TestOnToolAsyncCall_ApprovalSurvivesLaterHandler(t *testing.T) {
	for name, register := range map[string]func(*Conversation){
		"OnToolCtx": func(c *Conversation) {
			c.OnToolCtx("lookup", func(context.Context, map[string]any) (any, error) { return "ctx", nil })
		},
		"OnToolHTTP": func(c *Conversation) {
			c.OnToolHTTP("lookup", sdktools.NewHTTPToolConfig("http://127.0.0.1:1"))
		},
		"OnToolExecutor": func(c *Conversation) {
			c.OnToolExecutor("lookup", &recordingExecutor{seen: map[string]*packspec.Tool{}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			conv := handlerConv(t, false)
			rec := &recordedCalls[ToolCall]{}
			conv.OnToolAsyncCall("lookup", approvalCallCheck, func(_ context.Context, call ToolCall) (any, error) {
				rec.add(call)
				return "approved", nil
			})
			register(conv)
			ctx := context.Background()
			_, err := conv.Send(ctx, "ada")
			require.NoError(t, err)

			resolution, err := conv.ResolveTool(ctx, callerCallID)
			require.NoError(t, err)
			assert.JSONEq(t, `"approved"`, string(resolution.ResultJSON))
			require.Len(t, rec.seen(), 1)
			assert.Equal(t, handlerMember, rec.seen()[0].Caller)
			assert.Equal(t, callerCallID, rec.seen()[0].CallID)
		})
	}
}

// The approval rule does not depend on the executor kind: an approved held
// call runs the async exec registered for the tool, even over a CallExecutor
// registered after it, and a fork keeps the rule.
func TestOnToolAsync_ApprovalRunsAsyncExecOverLaterCallExecutor(t *testing.T) {
	for _, fork := range []bool{false, true} {
		t.Run(map[bool]string{false: "conversation", true: "fork"}[fork], func(t *testing.T) {
			conv := handlerConv(t, false)
			conv.OnToolAsyncWithContext("lookup", approvalCheck, func(map[string]any) (any, error) {
				return "async", nil
			})
			exec := &callerExecutor{}
			conv.OnToolExecutor("lookup", exec)
			if fork {
				forked, err := conv.Fork()
				require.NoError(t, err)
				t.Cleanup(func() { _ = forked.Close() })
				conv = forked
			}
			ctx := context.Background()
			_, err := conv.Send(ctx, "ada")
			require.NoError(t, err)

			resolution, err := conv.ResolveTool(ctx, callerCallID)
			require.NoError(t, err)
			assert.JSONEq(t, `"async"`, string(resolution.ResultJSON))
			assert.Empty(t, exec.seen(), "the executor must not run the approved call")
		})
	}
}

// An approved held call runs the OnToolAsync exec even over a plain OnTool
// handler registered after it, and a fork keeps the rule.
func TestOnToolAsync_ApprovalRunsAsyncExecOverLaterOnTool(t *testing.T) {
	for _, fork := range []bool{false, true} {
		t.Run(map[bool]string{false: "conversation", true: "fork"}[fork], func(t *testing.T) {
			conv := handlerConv(t, false)
			conv.OnToolAsyncWithContext("lookup", approvalCheck, func(map[string]any) (any, error) {
				return "async", nil
			})
			conv.OnTool("lookup", func(map[string]any) (any, error) { return "plain", nil })
			if fork {
				forked, err := conv.Fork()
				require.NoError(t, err)
				t.Cleanup(func() { _ = forked.Close() })
				conv = forked
			}
			ctx := context.Background()
			_, err := conv.Send(ctx, "ada")
			require.NoError(t, err)

			resolution, err := conv.ResolveTool(ctx, callerCallID)
			require.NoError(t, err)
			assert.JSONEq(t, `"async"`, string(resolution.ResultJSON))
		})
	}
}

// textExecutor is a plain executor whose result is not JSON.
type textExecutor struct{}

func (textExecutor) Name() string { return "text-exec" }

func (textExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`plain text`), nil
}

// A plain executor's result that is not JSON reaches the model as a string.
func TestOnToolExecutor_NonJSONResultIsAString(t *testing.T) {
	conv := handlerConv(t, false)
	conv.OnToolExecutor("lookup", textExecutor{})
	_, err := conv.Send(context.Background(), "ada")
	require.NoError(t, err)
	assert.Equal(t, "plain text", lookupToolResult(t, conv))
}
