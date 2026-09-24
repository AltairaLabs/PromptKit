package guardrails

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const funcTestTimeout = 20 * time.Millisecond

// hangUntilReleased blocks until the test ends, ignoring ctx — the worst case
// a func guardrail can present.
func hangUntilReleased(t *testing.T) <-chan struct{} {
	t.Helper()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	return release
}

func buildTimedFunc(t *testing.T, spec Spec, d time.Duration) hooks.ProviderHook {
	t.Helper()
	hook, err := spec.build(evals.NewEvalTypeRegistry())
	require.NoError(t, err)
	settable, ok := hook.(TimeoutSettable)
	require.True(t, ok, "a func guardrail must accept the host's guardrail timeout")
	settable.SetEvalTimeout(d)
	return hook
}

// An output func that never returns must not hold the turn: the guardrail
// fails closed, replaces the un-judged response and records why.
func TestFuncGuardrail_OutputTimeoutFailsClosed(t *testing.T) {
	release := hangUntilReleased(t)
	hook := buildTimedFunc(t, OutputFunc("slow-output",
		func(_ context.Context, _ *hooks.OutputRequest) hooks.Decision {
			<-release
			return hooks.Allow
		}), funcTestTimeout)

	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "un-judged reply"}}
	start := time.Now()
	d := hook.AfterCall(context.Background(), nil, resp)

	assert.Less(t, time.Since(start), time.Second)
	assert.False(t, d.Allow)
	assert.True(t, d.Enforced)
	assert.Equal(t, "timeout", d.Metadata["reason"])
	assert.Equal(t, prompt.DefaultBlockedMessage, resp.Message.Content,
		"the un-judged response must not ship")
}

func TestFuncGuardrail_InputTimeoutFailsClosed(t *testing.T) {
	release := hangUntilReleased(t)
	hook := buildTimedFunc(t, InputFunc("slow-input",
		func(_ context.Context, _ *hooks.InputRequest) hooks.Decision {
			<-release
			return hooks.Allow
		}), funcTestTimeout)

	req := userReq("hello")
	d := hook.BeforeCall(context.Background(), req)

	assert.False(t, d.Allow)
	assert.Equal(t, "timeout", d.Metadata["reason"])
	assert.Equal(t, prompt.DefaultBlockedMessage, req.Replacement)
}

// A func that answers in time keeps its own decision and its in-place rewrite.
func TestFuncGuardrail_AnswerInTimeKeepsItsRewrite(t *testing.T) {
	hook := buildTimedFunc(t, OutputFunc("redact",
		func(_ context.Context, out *hooks.OutputRequest) hooks.Decision {
			out.Message.Content = "[redacted]"
			return hooks.Enforced("redacted", nil)
		}), time.Second)

	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "secret"}}
	d := hook.AfterCall(context.Background(), nil, resp)

	assert.True(t, d.Enforced)
	assert.Equal(t, "[redacted]", resp.Message.Content)
}

// The func sees a deadline, so one that honors ctx can stop early.
func TestFuncGuardrail_FuncSeesTheDeadline(t *testing.T) {
	var sawDeadline bool
	hook := buildTimedFunc(t, InputFunc("ctx-aware",
		func(ctx context.Context, _ *hooks.InputRequest) hooks.Decision {
			_, sawDeadline = ctx.Deadline()
			return hooks.Allow
		}), time.Second)

	d := hook.BeforeCall(context.Background(), userReq("hello"))

	assert.True(t, d.Allow)
	assert.True(t, sawDeadline)
}
