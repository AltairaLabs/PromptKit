package hooks

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// --- test doubles ---

type stubProviderHook struct {
	name      string
	before    Decision
	after     Decision
	onChunk   Decision // only used if streaming is true
	streaming bool
}

func (h *stubProviderHook) Name() string { return h.name }
func (h *stubProviderHook) BeforeCall(_ context.Context, _ *ProviderRequest) Decision {
	return h.before
}
func (h *stubProviderHook) AfterCall(_ context.Context, _ *ProviderRequest, _ *ProviderResponse) Decision {
	return h.after
}
func (h *stubProviderHook) OnChunk(_ context.Context, _ *providers.StreamChunk) Decision {
	return h.onChunk
}

// Ensure stubProviderHook with streaming=true satisfies ChunkInterceptor at compile time.
var _ ChunkInterceptor = (*stubProviderHook)(nil)

type stubToolHook struct {
	name   string
	before Decision
	after  Decision
}

func (h *stubToolHook) Name() string { return h.name }
func (h *stubToolHook) BeforeExecution(_ context.Context, _ ToolRequest) Decision {
	return h.before
}
func (h *stubToolHook) AfterExecution(_ context.Context, _ ToolRequest, _ ToolResponse) Decision {
	return h.after
}

type stubSessionHook struct {
	name     string
	startFn  func(context.Context, SessionEvent) error
	updateFn func(context.Context, SessionEvent) error
	endFn    func(context.Context, SessionEvent) error
}

func (h *stubSessionHook) Name() string { return h.name }
func (h *stubSessionHook) OnSessionStart(ctx context.Context, e SessionEvent) error {
	if h.startFn != nil {
		return h.startFn(ctx, e)
	}
	return nil
}
func (h *stubSessionHook) OnSessionUpdate(ctx context.Context, e SessionEvent) error {
	if h.updateFn != nil {
		return h.updateFn(ctx, e)
	}
	return nil
}
func (h *stubSessionHook) OnSessionEnd(ctx context.Context, e SessionEvent) error {
	if h.endFn != nil {
		return h.endFn(ctx, e)
	}
	return nil
}

// providerHookOnly implements ProviderHook but NOT ChunkInterceptor.
type providerHookOnly struct {
	name   string
	before Decision
	after  Decision
}

func (h *providerHookOnly) Name() string { return h.name }
func (h *providerHookOnly) BeforeCall(_ context.Context, _ *ProviderRequest) Decision {
	return h.before
}
func (h *providerHookOnly) AfterCall(_ context.Context, _ *ProviderRequest, _ *ProviderResponse) Decision {
	return h.after
}

// --- nil registry tests ---

func TestNilRegistry(t *testing.T) {
	var r *Registry
	ctx := context.Background()

	if !r.IsEmpty() {
		t.Error("nil registry should be empty")
	}
	if d := r.RunBeforeProviderCall(ctx, &ProviderRequest{}); !d.Allow {
		t.Error("nil registry RunBeforeProviderCall should allow")
	}
	if d := r.RunAfterProviderCall(ctx, &ProviderRequest{}, &ProviderResponse{}); !d.Allow {
		t.Error("nil registry RunAfterProviderCall should allow")
	}
	if r.HasChunkInterceptors() {
		t.Error("nil registry should have no chunk interceptors")
	}
	if d := r.RunOnChunk(ctx, &providers.StreamChunk{}); !d.Allow {
		t.Error("nil registry RunOnChunk should allow")
	}
	if d := r.RunBeforeToolExecution(ctx, ToolRequest{}); !d.Allow {
		t.Error("nil registry RunBeforeToolExecution should allow")
	}
	if d := r.RunAfterToolExecution(ctx, ToolRequest{}, ToolResponse{}); !d.Allow {
		t.Error("nil registry RunAfterToolExecution should allow")
	}
	if err := r.RunSessionStart(ctx, SessionEvent{}); err != nil {
		t.Errorf("nil registry RunSessionStart should return nil, got %v", err)
	}
	if err := r.RunSessionUpdate(ctx, SessionEvent{}); err != nil {
		t.Errorf("nil registry RunSessionUpdate should return nil, got %v", err)
	}
	if err := r.RunSessionEnd(ctx, SessionEvent{}); err != nil {
		t.Errorf("nil registry RunSessionEnd should return nil, got %v", err)
	}
}

// --- empty registry tests ---

func TestEmptyRegistry(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	if !r.IsEmpty() {
		t.Error("empty registry should be empty")
	}
	if d := r.RunBeforeProviderCall(ctx, &ProviderRequest{}); !d.Allow {
		t.Error("empty registry RunBeforeProviderCall should allow")
	}
	if d := r.RunAfterProviderCall(ctx, &ProviderRequest{}, &ProviderResponse{}); !d.Allow {
		t.Error("empty registry RunAfterProviderCall should allow")
	}
	if d := r.RunBeforeToolExecution(ctx, ToolRequest{}); !d.Allow {
		t.Error("empty registry RunBeforeToolExecution should allow")
	}
	if d := r.RunAfterToolExecution(ctx, ToolRequest{}, ToolResponse{}); !d.Allow {
		t.Error("empty registry RunAfterToolExecution should allow")
	}
}

// --- provider hook chaining ---

func TestProviderHooks_AllAllow(t *testing.T) {
	r := NewRegistry(
		WithProviderHook(&providerHookOnly{name: "a", before: Allow, after: Allow}),
		WithProviderHook(&providerHookOnly{name: "b", before: Allow, after: Allow}),
	)
	ctx := context.Background()

	if r.IsEmpty() {
		t.Error("registry with hooks should not be empty")
	}
	if d := r.RunBeforeProviderCall(ctx, &ProviderRequest{}); !d.Allow {
		t.Error("all-allow BeforeCall should allow")
	}
	if d := r.RunAfterProviderCall(ctx, &ProviderRequest{}, &ProviderResponse{}); !d.Allow {
		t.Error("all-allow AfterCall should allow")
	}
}

func TestProviderHooks_FirstDenyWins(t *testing.T) {
	r := NewRegistry(
		WithProviderHook(&providerHookOnly{name: "denier", before: Deny("blocked"), after: Allow}),
		WithProviderHook(&providerHookOnly{name: "allower", before: Allow, after: Allow}),
	)
	ctx := context.Background()

	d := r.RunBeforeProviderCall(ctx, &ProviderRequest{})
	if d.Allow {
		t.Fatal("first deny should win")
	}
	if d.Reason != "blocked" {
		t.Errorf("expected reason 'blocked', got %q", d.Reason)
	}
}

func TestProviderHooks_SecondDenies(t *testing.T) {
	r := NewRegistry(
		WithProviderHook(&providerHookOnly{name: "first", before: Allow, after: Deny("post-denied")}),
		WithProviderHook(&providerHookOnly{name: "second", before: Allow, after: Allow}),
	)
	ctx := context.Background()

	d := r.RunAfterProviderCall(ctx, &ProviderRequest{}, &ProviderResponse{})
	if d.Allow {
		t.Fatal("AfterCall deny should propagate")
	}
	if d.Reason != "post-denied" {
		t.Errorf("expected reason 'post-denied', got %q", d.Reason)
	}
}

// TestProviderHooks_EnforcedShortCircuits pins the production contract that
// multi-guardrail chains stop at the first non-Allow decision, including the
// Enforced variant. Pack with [length, banned_words]: if length enforces
// first, banned_words must not run. Tests cannot rely on this firing in
// non-deterministic order — they assert single-firing semantics here.
func TestProviderHooks_EnforcedShortCircuits(t *testing.T) {
	called := 0
	counter := &providerHookOnly{
		name:   "counter",
		before: Allow,
		after:  Deny("should-not-be-called"),
	}
	wrap := &providerHookOnly{
		name:   "enforcing-guardrail",
		before: Allow,
		after:  Enforced("first-fired", map[string]any{"validator_type": "length"}),
	}
	// Wrap the second hook to actually count invocations
	countingSecond := &countingProviderHook{providerHookOnly: counter, count: &called}
	r := NewRegistry(WithProviderHook(wrap), WithProviderHook(countingSecond))

	d := r.RunAfterProviderCall(context.Background(), &ProviderRequest{}, &ProviderResponse{})
	if d.Allow {
		t.Fatal("Enforced decision must propagate as non-Allow")
	}
	if !d.Enforced {
		t.Error("first hook returned Enforced; decision must preserve Enforced=true")
	}
	if d.Reason != "first-fired" {
		t.Errorf("Reason = %q, want first-fired", d.Reason)
	}
	if got, _ := d.Metadata["validator_type"].(string); got != "length" {
		t.Errorf("Metadata[validator_type] = %q, want length", got)
	}
	if called != 0 {
		t.Errorf("subsequent hook ran %d times, want 0 (short-circuit broken)", called)
	}
}

// countingProviderHook wraps a providerHookOnly and increments count on
// every AfterCall invocation. Used to confirm short-circuit semantics in
// the multi-hook chain — if count != 0 after a prior Enforced fired, the
// chain isn't stopping.
type countingProviderHook struct {
	*providerHookOnly
	count *int
}

func (h *countingProviderHook) AfterCall(ctx context.Context, req *ProviderRequest, resp *ProviderResponse) Decision {
	*h.count++
	return h.providerHookOnly.AfterCall(ctx, req, resp)
}

// --- decision logging ---

// captureHookLogs redirects the package logger into a buffer for the duration
// of fn and returns everything written. Debug level so nothing under WARN is
// filtered out before the assertions can see it.
func captureHookLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { logger.SetLogger(nil) })
	fn()
	return buf.String()
}

// TestProviderHooks_EnforcedLogsInfoNotDenial pins issue #1720: an Enforced
// decision is a guardrail doing exactly its job on a blocked turn, so it must
// not be reported at WARN, and it must not be worded as a denial. The provider
// call is skipped and a canned turn substituted (before-call) or the response
// replaced (after-call) — the pipeline continues either way.
func TestProviderHooks_EnforcedLogsInfoNotDenial(t *testing.T) {
	const reason = "forbidden content found"
	tests := []struct {
		name     string
		hook     *providerHookOnly
		run      func(*Registry) Decision
		wantText string
	}{
		{
			name: "before-call",
			hook: &providerHookOnly{name: "guard", before: Enforced(reason, nil), after: Allow},
			run: func(r *Registry) Decision {
				return r.RunBeforeProviderCall(context.Background(), &ProviderRequest{})
			},
			wantText: "provider call skipped, canned turn substituted",
		},
		{
			name: "after-call",
			hook: &providerHookOnly{name: "guard", before: Allow, after: Enforced(reason, nil)},
			run: func(r *Registry) Decision {
				return r.RunAfterProviderCall(context.Background(), &ProviderRequest{}, &ProviderResponse{})
			},
			wantText: "response replaced, pipeline continues",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d Decision
			out := captureHookLogs(t, func() { d = tc.run(NewRegistry(WithProviderHook(tc.hook))) })

			if !d.Enforced {
				t.Fatalf("decision must stay Enforced, got %+v", d)
			}
			if !strings.Contains(out, reason) {
				t.Fatalf("enforced decision logged nothing about %q; output: %q", reason, out)
			}
			if strings.Contains(out, "level=WARN") {
				t.Errorf("enforced decision logged at WARN; output: %q", out)
			}
			if !strings.Contains(out, "level=INFO") {
				t.Errorf("enforced decision should log at INFO; output: %q", out)
			}
			if strings.Contains(out, "denied") {
				t.Errorf("enforced decision must not be worded as a denial; output: %q", out)
			}
			if !strings.Contains(out, tc.wantText) {
				t.Errorf("log should say %q; output: %q", tc.wantText, out)
			}
		})
	}
}

// TestProviderHooks_DenyLogsWarn is the other half of #1720: a genuine Deny
// aborts the pipeline with a HookDeniedError, so it keeps WARN and the
// "denied" wording. Without this, downgrading everything to INFO would pass.
func TestProviderHooks_DenyLogsWarn(t *testing.T) {
	const reason = "policy violation"
	tests := []struct {
		name    string
		hook    *providerHookOnly
		run     func(*Registry) Decision
		wantMsg string
	}{
		{
			name: "before-call",
			hook: &providerHookOnly{name: "denier", before: Deny(reason), after: Allow},
			run: func(r *Registry) Decision {
				return r.RunBeforeProviderCall(context.Background(), &ProviderRequest{})
			},
			wantMsg: "provider hook denied before-call",
		},
		{
			name: "after-call",
			hook: &providerHookOnly{name: "denier", before: Allow, after: Deny(reason)},
			run: func(r *Registry) Decision {
				return r.RunAfterProviderCall(context.Background(), &ProviderRequest{}, &ProviderResponse{})
			},
			wantMsg: "provider hook denied after-call",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d Decision
			out := captureHookLogs(t, func() { d = tc.run(NewRegistry(WithProviderHook(tc.hook))) })

			if d.Allow || d.Enforced {
				t.Fatalf("decision must be a hard deny, got %+v", d)
			}
			if !strings.Contains(out, tc.wantMsg) {
				t.Errorf("deny should log %q; output: %q", tc.wantMsg, out)
			}
			if !strings.Contains(out, "level=WARN") {
				t.Errorf("deny should log at WARN; output: %q", out)
			}
			if strings.Contains(out, "level=INFO") {
				t.Errorf("deny must not be downgraded to INFO; output: %q", out)
			}
			if !strings.Contains(out, reason) {
				t.Errorf("deny log should carry the reason %q; output: %q", reason, out)
			}
		})
	}
}

// --- chunk interceptor ---

func TestChunkInterceptor_Detection(t *testing.T) {
	nonStreaming := &providerHookOnly{name: "no-stream", before: Allow, after: Allow}
	streaming := &stubProviderHook{name: "stream", before: Allow, after: Allow, onChunk: Allow, streaming: true}

	r1 := NewRegistry(WithProviderHook(nonStreaming))
	if r1.HasChunkInterceptors() {
		t.Error("non-streaming hook should not register as chunk interceptor")
	}

	r2 := NewRegistry(WithProviderHook(streaming))
	if !r2.HasChunkInterceptors() {
		t.Error("streaming hook should register as chunk interceptor")
	}

	r3 := NewRegistry(WithProviderHook(nonStreaming), WithProviderHook(streaming))
	if !r3.HasChunkInterceptors() {
		t.Error("mixed hooks should have chunk interceptors")
	}
}

func TestChunkInterceptor_AllAllow(t *testing.T) {
	r := NewRegistry(
		WithProviderHook(&stubProviderHook{name: "a", onChunk: Allow}),
		WithProviderHook(&stubProviderHook{name: "b", onChunk: Allow}),
	)
	ctx := context.Background()

	d := r.RunOnChunk(ctx, &providers.StreamChunk{Delta: "hello"})
	if !d.Allow {
		t.Error("all-allow OnChunk should allow")
	}
}

func TestChunkInterceptor_DenyAborts(t *testing.T) {
	r := NewRegistry(
		WithProviderHook(&stubProviderHook{name: "denier", onChunk: Deny("too long")}),
		WithProviderHook(&stubProviderHook{name: "allower", onChunk: Allow}),
	)
	ctx := context.Background()

	d := r.RunOnChunk(ctx, &providers.StreamChunk{Delta: "x"})
	if d.Allow {
		t.Fatal("deny in OnChunk should propagate")
	}
	if d.Reason != "too long" {
		t.Errorf("expected reason 'too long', got %q", d.Reason)
	}
}

// --- tool hook chaining ---

func TestToolHooks_AllAllow(t *testing.T) {
	r := NewRegistry(
		WithToolHook(&stubToolHook{name: "a", before: Allow, after: Allow}),
		WithToolHook(&stubToolHook{name: "b", before: Allow, after: Allow}),
	)
	ctx := context.Background()

	if d := r.RunBeforeToolExecution(ctx, ToolRequest{}); !d.Allow {
		t.Error("all-allow BeforeExecution should allow")
	}
	if d := r.RunAfterToolExecution(ctx, ToolRequest{}, ToolResponse{}); !d.Allow {
		t.Error("all-allow AfterExecution should allow")
	}
}

func TestToolHooks_FirstDenyWins(t *testing.T) {
	r := NewRegistry(
		WithToolHook(&stubToolHook{name: "denier", before: Deny("forbidden"), after: Allow}),
		WithToolHook(&stubToolHook{name: "allower", before: Allow, after: Allow}),
	)
	ctx := context.Background()

	d := r.RunBeforeToolExecution(ctx, ToolRequest{Name: "dangerous"})
	if d.Allow {
		t.Fatal("deny should win")
	}
	if d.Reason != "forbidden" {
		t.Errorf("expected reason 'forbidden', got %q", d.Reason)
	}
}

func TestToolHooks_AfterDeny(t *testing.T) {
	r := NewRegistry(
		WithToolHook(&stubToolHook{name: "post-denier", before: Allow, after: Deny("bad result")}),
	)
	ctx := context.Background()

	d := r.RunAfterToolExecution(ctx, ToolRequest{}, ToolResponse{Content: "leaked data"})
	if d.Allow {
		t.Fatal("AfterExecution deny should propagate")
	}
	if d.Reason != "bad result" {
		t.Errorf("expected reason 'bad result', got %q", d.Reason)
	}
}

// --- session hook chaining ---

func TestSessionHooks_AllSucceed(t *testing.T) {
	calls := make([]string, 0)
	hook := &stubSessionHook{
		name: "tracker",
		startFn: func(_ context.Context, _ SessionEvent) error {
			calls = append(calls, "start")
			return nil
		},
		updateFn: func(_ context.Context, _ SessionEvent) error {
			calls = append(calls, "update")
			return nil
		},
		endFn: func(_ context.Context, _ SessionEvent) error {
			calls = append(calls, "end")
			return nil
		},
	}
	r := NewRegistry(WithSessionHook(hook))
	ctx := context.Background()
	event := SessionEvent{SessionID: "s1"}

	if err := r.RunSessionStart(ctx, event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.RunSessionUpdate(ctx, event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.RunSessionEnd(ctx, event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 3 || calls[0] != "start" || calls[1] != "update" || calls[2] != "end" {
		t.Errorf("expected [start update end], got %v", calls)
	}
}

func TestSessionHooks_ErrorShortCircuits(t *testing.T) {
	errBoom := errors.New("boom")
	called := false
	r := NewRegistry(
		WithSessionHook(&stubSessionHook{
			name: "failer",
			updateFn: func(_ context.Context, _ SessionEvent) error {
				return errBoom
			},
		}),
		WithSessionHook(&stubSessionHook{
			name: "never-reached",
			updateFn: func(_ context.Context, _ SessionEvent) error {
				called = true
				return nil
			},
		}),
	)
	ctx := context.Background()

	err := r.RunSessionUpdate(ctx, SessionEvent{})
	if !errors.Is(err, errBoom) {
		t.Errorf("expected errBoom, got %v", err)
	}
	if called {
		t.Error("second session hook should not have been called")
	}
}

// --- IsEmpty ---

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		name  string
		reg   *Registry
		empty bool
	}{
		{"nil", nil, true},
		{"no hooks", NewRegistry(), true},
		{"provider hook", NewRegistry(WithProviderHook(&providerHookOnly{name: "p"})), false},
		{"tool hook", NewRegistry(WithToolHook(&stubToolHook{name: "t"})), false},
		{"session hook", NewRegistry(WithSessionHook(&stubSessionHook{name: "s"})), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.reg.IsEmpty(); got != tt.empty {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.empty)
			}
		})
	}
}
