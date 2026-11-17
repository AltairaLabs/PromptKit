package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

var plumbedCaller = types.Caller{ID: "member@example.com", Claims: map[string]any{"tenant": "acme"}}

// callerSeen records callers under a lock: hooks and stages run off the
// caller's goroutine.
type callerSeen struct {
	mu   sync.Mutex
	seen []types.Caller
}

func (c *callerSeen) add(caller types.Caller) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, caller)
}

func (c *callerSeen) get() []types.Caller {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]types.Caller(nil), c.seen...)
}

// callerProviderHook records the caller of every provider hook call and every
// chunk it intercepts.
type callerProviderHook struct {
	before, after, chunks callerSeen
}

func (h *callerProviderHook) Name() string { return "caller-provider-hook" }

func (h *callerProviderHook) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	h.before.add(req.Caller)
	return hooks.Allow
}

func (h *callerProviderHook) AfterCall(
	_ context.Context, req *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	h.after.add(req.Caller)
	return hooks.Allow
}

func (h *callerProviderHook) InterceptChunk(_ context.Context, req hooks.ChunkRequest) hooks.Decision {
	h.chunks.add(req.Caller)
	return hooks.Allow
}

// distinctCallers returns got with repeats removed, in first-seen order: a
// turn makes several hook calls and chunks, each with the same caller.
func distinctCallers(got []types.Caller) []types.Caller {
	var out []types.Caller
	for _, c := range got {
		if !slices.ContainsFunc(out, func(o types.Caller) bool { return reflect.DeepEqual(o, c) }) {
			out = append(out, c)
		}
	}
	return out
}

func plumbingCases() []struct {
	name string
	opts []Option
	want types.Caller
} {
	return []struct {
		name string
		opts []Option
		want types.Caller
	}{
		{"set", []Option{WithCaller(plumbedCaller)}, plumbedCaller},
		{"unset", nil, types.Caller{}},
	}
}

func TestCaller_ProviderHooksSeeTheCaller(t *testing.T) {
	for _, tc := range plumbingCases() {
		t.Run("send/"+tc.name, func(t *testing.T) {
			hook := &callerProviderHook{}
			conv, err := Open(createTestPackFile(t, convChatPack), "chat", append([]Option{
				WithProvider(mock.NewProvider("mock", "mock-model", false)),
				WithSkipSchemaValidation(), WithProviderHook(hook)}, tc.opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })

			_, err = conv.Send(context.Background(), "hello")
			require.NoError(t, err)

			assert.Equal(t, []types.Caller{tc.want}, distinctCallers(hook.before.get()))
			assert.Equal(t, []types.Caller{tc.want}, distinctCallers(hook.after.get()))
		})
		t.Run("stream/"+tc.name, func(t *testing.T) {
			hook := &callerProviderHook{}
			conv, err := Open(createTestPackFile(t, convChatPack), "chat", append([]Option{
				WithProvider(mock.NewProvider("mock", "mock-model", false)),
				WithSkipSchemaValidation(), WithProviderHook(hook)}, tc.opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })

			for chunk := range conv.Stream(context.Background(), "hello") {
				require.NoError(t, chunk.Error)
			}

			assert.Equal(t, []types.Caller{tc.want}, distinctCallers(hook.before.get()))
			assert.Equal(t, []types.Caller{tc.want}, distinctCallers(hook.after.get()))
			assert.Equal(t, []types.Caller{tc.want}, distinctCallers(hook.chunks.get()))
		})
		t.Run("duplex/"+tc.name, func(t *testing.T) {
			hook := &callerProviderHook{}
			prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("hi there")
			conv, err := OpenDuplex(createTestPackFile(t, convChatPack), "chat", append([]Option{
				WithProvider(prov), WithSkipSchemaValidation(),
				WithStreamingConfig(convDuplexAudio), WithProviderHook(hook)}, tc.opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })

			require.NoError(t, conv.SendText(context.Background(), "hello"))
			require.Eventually(t, func() bool { return len(hook.after.get()) > 0 },
				5*time.Second, 5*time.Millisecond)

			assert.Equal(t, []types.Caller{tc.want}, distinctCallers(hook.before.get()))
			assert.Equal(t, []types.Caller{tc.want}, distinctCallers(hook.after.get()))
			assert.Equal(t, []types.Caller{tc.want}, distinctCallers(hook.chunks.get()))
		})
	}
}

// callerRetriever records the caller of each retrieval request.
type callerRetriever struct{ callerSeen }

func (r *callerRetriever) RetrieveContext(
	context.Context, map[string]string, []types.Message,
) ([]*memory.Memory, error) {
	r.add(types.Caller{ID: "plain RetrieveContext"})
	return nil, nil
}

func (r *callerRetriever) RetrieveContextRequest(
	_ context.Context, req memory.ContextRequest,
) ([]*memory.Memory, error) {
	r.add(req.Caller)
	return []*memory.Memory{{ID: "m1", Content: "grounding"}}, nil
}

func TestCaller_RetrieverSeesTheCaller(t *testing.T) {
	for _, tc := range plumbingCases() {
		t.Run(tc.name, func(t *testing.T) {
			retriever := &callerRetriever{}
			conv, err := Open(createTestPackFile(t, convChatPack), "chat", append([]Option{
				WithProvider(mock.NewProvider("mock", "mock-model", false)),
				WithSkipSchemaValidation(), WithRetriever(retriever)}, tc.opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })

			_, err = conv.Send(context.Background(), "hello")
			require.NoError(t, err)

			assert.Equal(t, []types.Caller{tc.want}, retriever.get())
		})
	}
}

// callerExtractor extracts one memory per turn, recording the caller.
type callerExtractor struct{ callerSeen }

func (e *callerExtractor) Extract(context.Context, map[string]string, []types.Message) ([]*memory.Memory, error) {
	e.add(types.Caller{ID: "plain Extract"})
	return nil, nil
}

func (e *callerExtractor) ExtractRequest(_ context.Context, req memory.ContextRequest) ([]*memory.Memory, error) {
	e.add(req.Caller)
	return []*memory.Memory{{Type: "fact", Content: "likes tea", Confidence: 0.9}}, nil
}

// callerMemoryStore records the caller of each save.
type callerMemoryStore struct {
	*memory.InMemoryStore
	saved callerSeen
}

func (s *callerMemoryStore) SaveWithOptions(ctx context.Context, m *memory.Memory, opts memory.SaveOptions) error {
	s.saved.add(opts.Caller)
	return s.Save(ctx, m)
}

func TestCaller_MemoryExtractionSeesTheCaller(t *testing.T) {
	for _, tc := range plumbingCases() {
		t.Run(tc.name, func(t *testing.T) {
			extractor := &callerExtractor{}
			store := &callerMemoryStore{InMemoryStore: memory.NewInMemoryStore()}
			conv, err := Open(createTestPackFile(t, convChatPack), "chat", append([]Option{
				WithProvider(mock.NewProvider("mock", "mock-model", false)),
				WithSkipSchemaValidation(),
				WithMemory(store, map[string]string{"user_id": "u1"}, WithMemoryExtractor(extractor)),
			}, tc.opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })

			_, err = conv.Send(context.Background(), "I like tea")
			require.NoError(t, err)

			assert.Equal(t, []types.Caller{tc.want}, extractor.get())
			assert.Equal(t, []types.Caller{tc.want}, store.saved.get())
		})
	}
}

// The memory tools hand the store the caller of the call the model made.
func TestCaller_MemoryToolsHandTheStoreTheCaller(t *testing.T) {
	for _, tc := range plumbingCases() {
		t.Run(tc.name, func(t *testing.T) {
			store := &callerMemoryStore{InMemoryStore: memory.NewInMemoryStore()}
			conv, err := Open(writeWorkflowTestPack(t, toolGrantPackJSON), "chat", append([]Option{
				WithProvider(newRememberOnceProvider("likes tea")),
				WithSkipSchemaValidation(),
				WithMemory(store, map[string]string{"user_id": "u1"}),
			}, tc.opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })

			_, err = conv.Send(context.Background(), "remember this")
			require.NoError(t, err)

			assert.Equal(t, []types.Caller{tc.want}, store.saved.get())
		})
	}
}

// callerEvalHandler records the caller of each evaluation.
type callerEvalHandler struct{ callerSeen }

func (h *callerEvalHandler) Type() string { return "turn_align_stub" }

func (h *callerEvalHandler) Eval(
	_ context.Context, evalCtx *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	h.add(evalCtx.Caller)
	score := 1.0
	return &evals.EvalResult{Score: &score}, nil
}

// A conversation's own evals are given its caller.
func TestCaller_ConversationEvalsSeeTheCaller(t *testing.T) {
	for _, tc := range plumbingCases() {
		t.Run(tc.name, func(t *testing.T) {
			handler := &callerEvalHandler{}
			registry := evals.NewEvalTypeRegistry()
			registry.Register(handler)
			conv, err := Open(createTestPackFile(t, turnAlignPackJSON), "default", append([]Option{
				WithProvider(mock.NewProvider("mock", "mock-model", false)),
				WithSkipSchemaValidation(), WithEvalRegistry(registry)}, tc.opts...)...)
			require.NoError(t, err)

			_, err = conv.Send(context.Background(), "hello")
			require.NoError(t, err)
			require.NoError(t, conv.Close())

			assert.Equal(t, []types.Caller{tc.want}, distinctCallers(handler.get()))
		})
	}
}

// Evaluate hands its handlers EvaluateOpts.Caller.
func TestCaller_EvaluateHandsHandlersTheCaller(t *testing.T) {
	handler := &callerEvalHandler{}
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(handler)

	_, err := Evaluate(context.Background(), EvaluateOpts{
		EvalDefs: []evals.EvalDef{{ID: "e1", Type: "turn_align_stub", Trigger: evals.TriggerEveryTurn}},
		Messages: []types.Message{types.NewUserMessage("hi"), types.NewAssistantMessage("hello")},
		Registry: registry,
		Caller:   plumbedCaller,
	})
	require.NoError(t, err)

	assert.Equal(t, []types.Caller{plumbedCaller}, handler.get())
}

// callerSessionRecorder records the caller each conversation starts with.
type callerSessionRecorder struct{ callerSeen }

func (h *callerSessionRecorder) Name() string { return "caller-session" }
func (h *callerSessionRecorder) OnSessionStart(_ context.Context, e hooks.SessionEvent) error {
	h.add(e.Caller)
	return nil
}
func (h *callerSessionRecorder) OnSessionUpdate(context.Context, hooks.SessionEvent) error {
	return nil
}
func (h *callerSessionRecorder) OnSessionEnd(context.Context, hooks.SessionEvent) error { return nil }

// A2ACallerOpener opens each conversation for the caller its request names,
// over a WithCaller in its options.
func TestCaller_A2ACallerOpenerActsForTheRequestsCaller(t *testing.T) {
	hook := &callerSessionRecorder{}
	callerOf := func(r *http.Request) types.Caller { return types.Caller{ID: r.Header.Get("X-User")} }
	open, err := A2ACallerOpener(createTestPackFile(t, convChatPack), "chat", callerOf,
		WithProvider(mock.NewProvider("mock", "mock-model", false)), WithSkipSchemaValidation(),
		WithSessionHook(hook), WithCaller(types.Caller{ID: "fixed"}))
	require.NoError(t, err)

	for _, user := range []string{"alice", "bob"} {
		r := httptest.NewRequest(http.MethodPost, "/a2a", http.NoBody)
		r.Header.Set("X-User", user)
		conv, err := open(r, "ctx-"+user)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })
		_, err = conv.Send(context.Background(), "hello")
		require.NoError(t, err)
	}

	assert.Equal(t, []types.Caller{{ID: "alice"}, {ID: "bob"}}, hook.get())
}

func TestCaller_A2ACallerOpenerRefusesANilCallerOf(t *testing.T) {
	open, err := A2ACallerOpener(createTestPackFile(t, convChatPack), "chat", nil)

	assert.ErrorIs(t, err, ErrNilCallerOf)
	assert.Nil(t, open)
}

// Func guardrails are given the caller on both sides of the call.
func TestCaller_FuncGuardrailsSeeTheCaller(t *testing.T) {
	for _, tc := range plumbingCases() {
		t.Run(tc.name, func(t *testing.T) {
			var in, out callerSeen
			conv, err := Open(createTestPackFile(t, convChatPack), "chat", append([]Option{
				WithProvider(mock.NewProvider("mock", "mock-model", false)), WithSkipSchemaValidation(),
				WithGuardrail(
					guardrails.InputFunc("tenant-in", func(_ context.Context, r *hooks.InputRequest) hooks.Decision {
						in.add(r.Caller)
						return hooks.Allow
					}),
					guardrails.OutputFunc("tenant-out", func(_ context.Context, r *hooks.OutputRequest) hooks.Decision {
						out.add(r.Caller)
						return hooks.Allow
					}),
				)}, tc.opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })

			_, err = conv.Send(context.Background(), "hello")
			require.NoError(t, err)

			assert.Equal(t, []types.Caller{tc.want}, in.get())
			assert.Equal(t, []types.Caller{tc.want}, out.get())
		})
	}
}
