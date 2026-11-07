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
