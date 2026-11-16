package sdk

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callerToolsPack declares one tool, lookup, on the chat prompt.
const callerToolsPack = `{
  "id": "caller-tools", "name": "caller-tools", "version": "1.0.0",
  "template_engine": {"version": "v1", "syntax": "{{variable}}"},
  "tools": {
    "lookup": {"name": "lookup", "description": "look up",
      "parameters": {"type": "object", "properties": {"who": {"type": "string"}}}}
  },
  "prompts": {
    "chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
      "system_template": "You look things up.", "tools": ["lookup"]}
  }
}`

// callerCallID is the call ID every caller-test provider gives its lookup call.
const callerCallID = "call-lookup-1"

// callerToolProvider calls lookup, with the latest user text as "who", until
// the request ends in a tool result, then answers. It keeps no state, so one
// instance can serve concurrent conversations.
type callerToolProvider struct {
	*mock.ToolProvider
}

func newCallerToolProvider() *callerToolProvider {
	return &callerToolProvider{ToolProvider: mock.NewToolProvider("caller", "caller-model", false, nil)}
}

func (p *callerToolProvider) SupportsStreaming() bool { return false }

func (p *callerToolProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "tool" {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	who := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			who = m.GetContent()
		}
	}
	args, _ := json.Marshal(map[string]string{"who": who})
	calls := []types.MessageToolCall{{ID: callerCallID, Name: "lookup", Args: args}}
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

// callerToolSession is a realtime session that answers its first text with a
// lookup call and the tool result with "done".
type callerToolSession struct {
	*mock.MockStreamSession
	mu   sync.Mutex
	sent bool
}

func (s *callerToolSession) SendText(ctx context.Context, text string) error {
	s.mu.Lock()
	first := !s.sent
	s.sent = true
	s.mu.Unlock()
	if !first {
		return s.MockStreamSession.SendText(ctx, text)
	}
	finish := "tool_calls"
	go s.EmitChunk(&providers.StreamChunk{
		ToolCalls: []types.MessageToolCall{
			{ID: callerCallID, Name: "lookup", Args: json.RawMessage(`{"who":"` + text + `"}`)},
		},
		FinishReason: &finish,
	})
	return nil
}

func (s *callerToolSession) SendToolResponses(_ context.Context, _ []providers.ToolResponse) error {
	stop := "stop"
	go s.EmitChunk(&providers.StreamChunk{Content: "done", Delta: "done", FinishReason: &stop})
	return nil
}

func (s *callerToolSession) SendToolResponse(ctx context.Context, id, result string) error {
	return s.SendToolResponses(ctx, []providers.ToolResponse{{ToolCallID: id, Result: result}})
}

// callerStreamProvider creates callerToolSessions.
type callerStreamProvider struct {
	*mock.StreamingProvider
}

func (p *callerStreamProvider) CreateStreamSession(
	context.Context, *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return &callerToolSession{MockStreamSession: mock.NewMockStreamSession()}, nil
}

// callerExecutor is a host CallExecutor that records every call it is given.
// One instance may serve several conversations.
type callerExecutor struct {
	mu    sync.Mutex
	calls []tools.ToolCall
}

func (e *callerExecutor) Name() string { return "caller-exec" }

func (e *callerExecutor) Execute(
	_ context.Context, d *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, tools.ToolCall{Name: d.Name, Args: args})
	return json.RawMessage(`{"found":true}`), nil
}

func (e *callerExecutor) ExecuteCall(
	_ context.Context, _ *tools.ToolDescriptor, call tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, call)
	return json.RawMessage(`{"found":true}`), nil, nil
}

func (e *callerExecutor) seen() []tools.ToolCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]tools.ToolCall(nil), e.calls...)
}

// callerToolHook records every tool hook request.
type callerToolHook struct {
	mu   sync.Mutex
	reqs []hooks.ToolRequest
}

func (h *callerToolHook) Name() string { return "caller-tool-hook" }

func (h *callerToolHook) BeforeExecution(_ context.Context, req hooks.ToolRequest) hooks.Decision {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reqs = append(h.reqs, req)
	return hooks.Allow
}

func (h *callerToolHook) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

func (h *callerToolHook) seen() []hooks.ToolRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hooks.ToolRequest(nil), h.reqs...)
}

// callerCase opens a conversation with opts, registers exec on it, and runs
// one turn that calls lookup.
type callerCase struct {
	name string
	// hookRuns is false where tool hooks do not run (ASM duplex, #2696).
	hookRuns bool
	run      func(t *testing.T, exec *callerExecutor, opts ...Option)
}

func callerUnary(t *testing.T, exec *callerExecutor, opts ...Option) {
	conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
		append([]Option{WithProvider(newCallerToolProvider()), WithSkipSchemaValidation()}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnToolExecutor("lookup", exec)
	_, err = conv.Send(context.Background(), "ada")
	require.NoError(t, err)
}

func callerCases() []callerCase {
	return []callerCase{
		{name: "unary", hookRuns: true, run: callerUnary},
		{name: "template", hookRuns: true, run: func(t *testing.T, exec *callerExecutor, opts ...Option) {
			tmpl, err := LoadTemplate(createTestPackFile(t, callerToolsPack), WithSkipSchemaValidation())
			require.NoError(t, err)
			conv, err := tmpl.Open("chat",
				append([]Option{WithProvider(newCallerToolProvider()), WithSkipSchemaValidation()}, opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			conv.OnToolExecutor("lookup", exec)
			_, err = conv.Send(context.Background(), "ada")
			require.NoError(t, err)
		}},
		{name: "fork", hookRuns: true, run: func(t *testing.T, exec *callerExecutor, opts ...Option) {
			conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
				append([]Option{WithProvider(newCallerToolProvider()), WithSkipSchemaValidation()}, opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			conv.OnToolExecutor("lookup", exec) // the fork copies it
			fork, err := conv.Fork()
			require.NoError(t, err)
			t.Cleanup(func() { _ = fork.Close() })
			_, err = fork.Send(context.Background(), "ada")
			require.NoError(t, err)
		}},
		{name: "duplex-asm", hookRuns: false, run: func(t *testing.T, exec *callerExecutor, opts ...Option) {
			prov := &callerStreamProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false)}
			conv, err := OpenDuplex(createTestPackFile(t, callerToolsPack), "chat",
				append([]Option{WithProvider(prov), WithSkipSchemaValidation(),
					WithStreamingConfig(convDuplexAudio)}, opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			conv.OnToolExecutor("lookup", exec)
			convDrain(t, conv)
			require.NoError(t, conv.SendText(context.Background(), "ada"))
			require.Eventually(t, func() bool { return len(exec.seen()) > 0 }, 5*time.Second, 5*time.Millisecond)
		}},
		{name: "duplex-asm-fork", hookRuns: false, run: func(t *testing.T, exec *callerExecutor, opts ...Option) {
			prov := &callerStreamProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false)}
			conv, err := OpenDuplex(createTestPackFile(t, callerToolsPack), "chat",
				append([]Option{WithProvider(prov), WithSkipSchemaValidation(),
					WithStreamingConfig(convDuplexAudio)}, opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			conv.OnToolExecutor("lookup", exec) // the fork copies it
			fork, err := conv.Fork()
			require.NoError(t, err)
			t.Cleanup(func() { _ = fork.Close() })
			convDrain(t, fork)
			require.NoError(t, fork.SendText(context.Background(), "ada"))
			require.Eventually(t, func() bool { return len(exec.seen()) > 0 }, 5*time.Second, 5*time.Millisecond)
		}},
		{name: "duplex-vad", hookRuns: true, run: func(t *testing.T, exec *callerExecutor, opts ...Option) {
			if testing.Short() {
				t.Skip("drives a real VAD turn")
			}
			conv, err := OpenDuplex(createTestPackFile(t, callerToolsPack), "chat",
				append([]Option{WithProvider(newCallerToolProvider()), WithSkipSchemaValidation(),
					WithVADMode(newScriptedSTT("ada"), newConvMockTTSService(), vadTestConfig())}, opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			conv.OnToolExecutor("lookup", exec)
			convDrain(t, conv)
			speakOneUtterance(t, conv)
			require.Eventually(t, func() bool { return len(exec.seen()) > 0 }, 8*time.Second, 5*time.Millisecond)
		}},
	}
}

// Every tool call a conversation makes carries the caller it was opened with,
// to the host executor and to the tool hook, with the provider's call ID; a
// conversation opened without one carries the zero Caller.
func TestCaller_ToolCallsCarryTheCaller(t *testing.T) {
	member := types.Caller{ID: "member@example.com"}
	for _, tc := range callerCases() {
		for _, set := range []bool{true, false} {
			name, want := tc.name+"/set", member
			opts := []Option{WithCaller(member)}
			if !set {
				name, want, opts = tc.name+"/unset", types.Caller{}, nil
			}
			t.Run(name, func(t *testing.T) {
				exec, hook := &callerExecutor{}, &callerToolHook{}
				tc.run(t, exec, append(opts, WithToolHook(hook))...)

				calls := exec.seen()
				require.Len(t, calls, 1)
				assert.Equal(t, "lookup", calls[0].Name)
				assert.Equal(t, callerCallID, calls[0].ID)
				assert.Equal(t, want, calls[0].Caller)
				assert.JSONEq(t, `{"who":"ada"}`, string(calls[0].Args))

				if !tc.hookRuns {
					return
				}
				reqs := hook.seen()
				require.Len(t, reqs, 1)
				assert.Equal(t, callerCallID, reqs[0].CallID)
				assert.Equal(t, want, reqs[0].Caller)
			})
		}
	}
}

// One host executor serving two conversations opened from one template, with
// different callers and turns running concurrently, sees each call with its
// own conversation's caller.
func TestCaller_ConcurrentConversationsKeepTheirOwnCaller(t *testing.T) {
	tmpl, err := LoadTemplate(createTestPackFile(t, callerToolsPack), WithSkipSchemaValidation())
	require.NoError(t, err)
	exec := &callerExecutor{}
	prov := newCallerToolProvider()
	callers := map[string]types.Caller{
		"a": {ID: "a@example.com", Claims: map[string]any{"tenant": "acme"}},
		"b": {ID: "b@example.com", Claims: map[string]any{"tenant": "globex"}},
	}
	const turns = 5

	var wg sync.WaitGroup
	for who, caller := range callers {
		conv, err := tmpl.Open("chat", WithProvider(prov), WithSkipSchemaValidation(), WithCaller(caller))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })
		conv.OnToolExecutor("lookup", exec)
		wg.Add(1)
		go func(who string, conv *Conversation) {
			defer wg.Done()
			for i := 0; i < turns; i++ {
				_, err := conv.Send(context.Background(), who)
				assert.NoError(t, err)
			}
		}(who, conv)
	}
	wg.Wait()

	calls := exec.seen()
	require.Len(t, calls, 2*turns)
	perCaller := map[string]int{}
	for _, call := range calls {
		var args struct{ Who string }
		require.NoError(t, json.Unmarshal(call.Args, &args))
		assert.Equal(t, callers[args.Who], call.Caller, "call from conversation %q", args.Who)
		perCaller[call.Caller.ID]++
	}
	assert.Equal(t, map[string]int{"a@example.com": turns, "b@example.com": turns}, perCaller)
}

// callerLocalExecutor replaces the conversation's "local" executor and records
// each call it is given whole.
type callerLocalExecutor struct{ callerExecutor }

func (e *callerLocalExecutor) Name() string { return "local" }

// A composition's tool step runs for the conversation's caller.
func TestCaller_CompositionToolStepCarriesTheCaller(t *testing.T) {
	member := types.Caller{ID: "member@example.com"}
	wc, err := OpenWorkflow(createTestPackFile(t, compositionExecPackJSON), WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)), WithCaller(member))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	exec := &callerLocalExecutor{}
	wc.ActiveConversation().ToolRegistry().RegisterExecutor(exec)

	_, err = wc.Send(context.Background(), "hello from composition")
	require.NoError(t, err)
	calls := exec.seen()
	require.Len(t, calls, 1)
	assert.Equal(t, "echo", calls[0].Name)
	assert.Equal(t, member, calls[0].Caller)
}

// A handler registered after a CallExecutor for the same tool replaces it.
func TestCaller_LaterHandlerReplacesCallExecutor(t *testing.T) {
	for name, register := range map[string]func(*Conversation) func() bool{
		"OnToolCtx": func(c *Conversation) func() bool {
			var mu sync.Mutex
			ran := false
			c.OnToolCtx("lookup", func(context.Context, map[string]any) (any, error) {
				mu.Lock()
				defer mu.Unlock()
				ran = true
				return "ctx", nil
			})
			return func() bool { mu.Lock(); defer mu.Unlock(); return ran }
		},
		"OnToolExecutor": func(c *Conversation) func() bool {
			plain := &recordingExecutor{seen: map[string]*packspec.Tool{}}
			c.OnToolExecutor("lookup", plain)
			return func() bool {
				plain.mu.Lock()
				defer plain.mu.Unlock()
				_, ran := plain.seen["lookup"]
				return ran
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
				WithProvider(newCallerToolProvider()), WithSkipSchemaValidation())
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			exec := &callerExecutor{}
			conv.OnToolExecutor("lookup", exec)
			replacementRan := register(conv)
			_, err = conv.Send(context.Background(), "ada")
			require.NoError(t, err)
			assert.True(t, replacementRan(), "the later registration runs the call")
			assert.Empty(t, exec.seen(), "the replaced executor must not run")
		})
	}
}
