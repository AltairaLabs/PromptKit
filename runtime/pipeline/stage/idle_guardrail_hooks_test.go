package stage

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// slowGuardrailHook is a guardrail whose classifier call outlasts an idle
// window. It opens a window on the probe when it starts and closes it before
// it returns, recording whether the window held: whether something kept the
// idle timer alive while the hook ran. It never denies outright — every path
// in this file cares about the ENFORCED message reaching the caller, not about
// exercising HookDeniedError.
type slowGuardrailHook struct {
	probe   *idleProbe
	message string
	held    atomic.Bool
}

func (h *slowGuardrailHook) Name() string { return "slow-guardrail" }

func (h *slowGuardrailHook) run() {
	h.probe.openWindow()
	h.held.Store(h.probe.closeWindow())
}

func (h *slowGuardrailHook) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	h.run()
	req.Replacement = h.message
	return hooks.Enforced("blocked", map[string]any{"validator_type": "slow_guardrail"})
}

func (h *slowGuardrailHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	h.run()
	resp.Message.Content = h.message
	return hooks.Enforced("blocked", map[string]any{"validator_type": "slow_guardrail"})
}

// stageWithSlowGuardrail builds a provider stage whose only hook is hook,
// mirroring stageWithSlowTool's shape in idle_tool_execution_test.go.
func stageWithSlowGuardrail(hook *slowGuardrailHook) *ProviderStage {
	registry := hooks.NewRegistry(hooks.WithProviderHook(hook))
	return NewProviderStageWithHooks(mock.NewProvider("test", "model", false), nil, nil, nil, nil, registry)
}

// TestRunBeforeCallHooks_SlowGuardrailDoesNotTripIdleTimeout is the
// regression for #2064 round 1's finding 3: before the `defer
// keepIdleAlive(ctx)()` fix, a guardrail hook slower than IdleTimeout raced
// its own idle-timeout cancellation. The classifier itself was already
// bounded (GuardrailHookAdapter's own ~30s timeout), but the SHARED pipeline
// context could still expire mid-evaluation and take the correctly-built
// enforced message down with it on the way out.
//
// The hook holds an idle window open (see idleProbe) instead of racing a real
// timer, so the test does not depend on how fast the runner is. Reverting the
// `defer keepIdleAlive(ctx)()` line in runBeforeCallHooks makes the window
// lapse and the test fail.
func TestRunBeforeCallHooks_SlowGuardrailDoesNotTripIdleTimeout(t *testing.T) {
	probe := newIdleProbe()
	hook := &slowGuardrailHook{probe: probe, message: "held open before"}
	stage := stageWithSlowGuardrail(hook)

	blocked, handled, err := stage.runBeforeCallHooks(probe.context(context.Background()), nil, "", 1, nil)

	require.NoError(t, err, "an enforcing guardrail must not abort the pipeline")
	assert.True(t, handled)
	assert.Equal(t, "held open before", blocked.Content,
		"the guardrail's message must reach the caller")
	assert.True(t, hook.held.Load(), "the idle window lapsed while the guardrail hook was running")
}

// TestRunAfterCallHooks_SlowGuardrailDoesNotTripIdleTimeout is the AfterCall
// (output-direction) half of the test above. Reverting the `defer
// keepIdleAlive(ctx)()` line in runAfterCallHooks makes it fail.
func TestRunAfterCallHooks_SlowGuardrailDoesNotTripIdleTimeout(t *testing.T) {
	probe := newIdleProbe()
	hook := &slowGuardrailHook{probe: probe, message: "held open after"}
	stage := stageWithSlowGuardrail(hook)

	responseMsg := &types.Message{Content: "original"}
	toolCalls := []types.MessageToolCall{}
	err := stage.runAfterCallHooks(probe.context(context.Background()), &afterCallParams{
		responseMsg: responseMsg,
		toolCalls:   &toolCalls,
		round:       1,
	})

	require.NoError(t, err, "an enforcing guardrail must not abort the pipeline")
	assert.Equal(t, "held open after", responseMsg.Content,
		"the guardrail's message must reach the caller")
	assert.True(t, hook.held.Load(), "the idle window lapsed while the guardrail hook was running")
}
