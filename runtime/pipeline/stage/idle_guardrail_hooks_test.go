package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// slowGuardrailHook blocks for delay before enforcing, like a guardrail whose
// classifier call takes its time. It never denies outright — every path in
// this file cares about the ENFORCED message reaching the caller, not about
// exercising HookDeniedError.
type slowGuardrailHook struct {
	delay   time.Duration
	message string
}

func (h *slowGuardrailHook) Name() string { return "slow-guardrail" }

func (h *slowGuardrailHook) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
	select {
	case <-time.After(h.delay):
	case <-ctx.Done():
		return hooks.Deny(ctx.Err().Error())
	}
	req.Replacement = h.message
	return hooks.Enforced("blocked", map[string]any{"validator_type": "slow_guardrail"})
}

func (h *slowGuardrailHook) AfterCall(
	ctx context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	select {
	case <-time.After(h.delay):
	case <-ctx.Done():
		return hooks.Deny(ctx.Err().Error())
	}
	resp.Message.Content = h.message
	return hooks.Enforced("blocked", map[string]any{"validator_type": "slow_guardrail"})
}

// stageWithSlowGuardrail builds a provider stage whose only hook is a
// slowGuardrailHook, mirroring stageWithSlowTool's shape in
// idle_tool_execution_test.go.
func stageWithSlowGuardrail(delay time.Duration, message string) *ProviderStage {
	registry := hooks.NewRegistry(hooks.WithProviderHook(&slowGuardrailHook{delay: delay, message: message}))
	return NewProviderStageWithHooks(mock.NewProvider("test", "model", false), nil, nil, nil, nil, registry)
}

// TestRunBeforeCallHooks_SlowGuardrailDoesNotTripIdleTimeout is the
// regression for #2064 round 1's finding 3: before the `defer
// keepIdleAlive(ctx)()` fix, a guardrail hook slower than IdleTimeout raced
// its own idle-timeout cancellation. The classifier itself was already
// bounded (GuardrailHookAdapter's own ~30s timeout), but the SHARED pipeline
// context could still expire mid-evaluation and take the correctly-built
// enforced message down with it on the way out. This test proves the timer
// no longer fires while the hook is in flight, deterministically and in
// milliseconds rather than racing two real 30-second clocks.
//
// Verified to fail without the fix: reverting the `defer
// keepIdleAlive(ctx)()` line in runBeforeCallHooks makes this test fail with
// idleCtx.Err() == context.Canceled (idle timeout fired at ~50ms while the
// 200ms hook was still running).
func TestRunBeforeCallHooks_SlowGuardrailDoesNotTripIdleTimeout(t *testing.T) {
	const (
		idle      = 50 * time.Millisecond
		hookDelay = 200 * time.Millisecond // 4x the idle window
	)

	idleCtx, cancel, reset := withIdleTimeout(context.Background(), idle)
	defer cancel()
	ctx := contextWithIdleReset(idleCtx, reset, idle)

	stage := stageWithSlowGuardrail(hookDelay, "held open before")

	blocked, handled, err := stage.runBeforeCallHooks(ctx, nil, "", 1, nil)

	require.NoError(t, err, "an enforcing guardrail must not abort the pipeline")
	assert.True(t, handled)
	assert.Equal(t, "held open before", blocked.Content,
		"the guardrail's message must reach the caller, not be dropped by a raced idle timeout")
	assert.NoError(t, idleCtx.Err(), "idle timer fired while the guardrail hook was running")
}

// TestRunAfterCallHooks_SlowGuardrailDoesNotTripIdleTimeout is the AfterCall
// (output-direction) half of the test above.
//
// Verified to fail without the fix: reverting the `defer
// keepIdleAlive(ctx)()` line in runAfterCallHooks makes this test fail with
// idleCtx.Err() == context.Canceled.
func TestRunAfterCallHooks_SlowGuardrailDoesNotTripIdleTimeout(t *testing.T) {
	const (
		idle      = 50 * time.Millisecond
		hookDelay = 200 * time.Millisecond // 4x the idle window
	)

	idleCtx, cancel, reset := withIdleTimeout(context.Background(), idle)
	defer cancel()
	ctx := contextWithIdleReset(idleCtx, reset, idle)

	stage := stageWithSlowGuardrail(hookDelay, "held open after")

	responseMsg := &types.Message{Content: "original"}
	toolCalls := []types.MessageToolCall{}
	err := stage.runAfterCallHooks(ctx, &afterCallParams{
		responseMsg: responseMsg,
		toolCalls:   &toolCalls,
		round:       1,
	})

	require.NoError(t, err, "an enforcing guardrail must not abort the pipeline")
	assert.Equal(t, "held open after", responseMsg.Content,
		"the guardrail's message must reach the caller, not be dropped by a raced idle timeout")
	assert.NoError(t, idleCtx.Err(), "idle timer fired while the guardrail hook was running")
}
