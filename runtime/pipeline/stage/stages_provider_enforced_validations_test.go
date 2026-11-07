package stage

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A streamed round whose chunk interceptor fired already carries that firing
// when the AfterCall chain runs. The hook sees a copy of the message, so its
// Validations start with the same entries; folding the copy's validations back
// in appended every one of them a second time.
func TestRunAfterCallHooks_EnforcedKeepsEachValidationOnce(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(
		&enforceAfterCallHook{replacement: "redacted"},
	))
	s := NewProviderStageWithHooks(mock.NewProvider("p", "m", false), nil, nil,
		&ProviderConfig{}, nil, reg)

	chunkFiring := types.ValidationResult{ValidatorType: "streaming_check"}
	msg := types.Message{Role: roleAssistant, Content: "original",
		Validations: []types.ValidationResult{chunkFiring}}
	var toolCalls []types.MessageToolCall

	require.NoError(t, s.runAfterCallHooks(context.Background(), &afterCallParams{
		responseMsg: &msg, toolCalls: &toolCalls,
	}))

	var got []string
	for _, v := range msg.Validations {
		got = append(got, v.ValidatorType)
	}
	assert.Equal(t, []string{"streaming_check", "test_output"}, got)
	assert.Equal(t, "redacted", msg.Content)
}

// A hook that records its own validation on the response it was handed still
// has that validation folded into the message.
func TestApplyEnforcedResponse_KeepsValidationsTheHookAdded(t *testing.T) {
	prior := types.ValidationResult{ValidatorType: "prior"}
	msg := types.Message{Validations: []types.ValidationResult{prior}}
	resp := &hooks.ProviderResponse{Message: msg}
	resp.Message.Validations = append(resp.Message.Validations,
		types.ValidationResult{ValidatorType: "hook_added"})
	var toolCalls []types.MessageToolCall

	applyEnforcedResponse(&msg, &toolCalls, resp, hooks.Enforced("x", nil), 1)

	var got []string
	for _, v := range msg.Validations {
		got = append(got, v.ValidatorType)
	}
	assert.Equal(t, []string{"prior", "hook_added"}, got)
}

// appendingEnforceHook records its own validation on the response it is handed,
// then enforces.
type appendingEnforceHook struct{}

func (appendingEnforceHook) Name() string { return "appending" }

func (appendingEnforceHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (appendingEnforceHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	resp.Message.Validations = append(resp.Message.Validations, types.ValidationResult{ValidatorType: "hook_added"})
	return hooks.Enforced("x", map[string]any{"validator_type": "firing"})
}

// The hook's copy of the message must not share the Validations backing
// array: with spare capacity, the hook's append and the firing's append wrote
// the same slot, so the firing was kept twice and the hook's entry lost.
func TestRunAfterCallHooks_HookValidationsDoNotAliasTheMessage(t *testing.T) {
	s := NewProviderStageWithHooks(mock.NewProvider("p", "m", false), nil, nil,
		&ProviderConfig{}, nil, hooks.NewRegistry(hooks.WithProviderHook(appendingEnforceHook{})))
	vs := make([]types.ValidationResult, 1, 8)
	vs[0] = types.ValidationResult{ValidatorType: "prior"}
	msg := types.Message{Role: roleAssistant, Validations: vs}
	var toolCalls []types.MessageToolCall

	require.NoError(t, s.runAfterCallHooks(context.Background(), &afterCallParams{
		responseMsg: &msg, toolCalls: &toolCalls,
	}))
	var got []string
	for _, v := range msg.Validations {
		got = append(got, v.ValidatorType)
	}
	assert.Equal(t, []string{"prior", "firing", "hook_added"}, got)
}
