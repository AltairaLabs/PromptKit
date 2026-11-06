package sdk

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

func TestWrapTurnError_HookDenial(t *testing.T) {
	denied := &hooks.HookDeniedError{
		HookName: "provider_hook", HookType: "provider_after", Reason: "pii found",
		Metadata: map[string]any{"validator_type": "pii_filter"},
	}
	err := wrapTurnError(fmt.Errorf("stage failed: %w", denied), "openai")

	vErr, ok := AsValidationError(err)
	require.True(t, ok)
	assert.Equal(t, "pii_filter", vErr.ValidatorType, "the guardrail's own type wins over the hook point")
	assert.Equal(t, "pii found", vErr.Message)
	assert.Equal(t, denied.Metadata, vErr.Details)
	var got *hooks.HookDeniedError
	assert.ErrorAs(t, err, &got, "the runtime denial stays reachable")
}

func TestWrapTurnError_HookDenialWithoutValidatorType(t *testing.T) {
	err := wrapTurnError(&hooks.HookDeniedError{HookType: "provider_before", Reason: "no"}, "")
	vErr, ok := AsValidationError(err)
	require.True(t, ok)
	assert.Equal(t, "provider_before", vErr.ValidatorType)
}

func TestWrapTurnError_StreamAbort(t *testing.T) {
	abort := &providers.ValidationAbortError{Reason: "banned word"}
	err := wrapTurnError(abort, "")

	vErr, ok := AsValidationError(err)
	require.True(t, ok)
	assert.Equal(t, chunkHookType, vErr.ValidatorType)
	assert.Equal(t, "banned word", vErr.Message)
	var got *providers.ValidationAbortError
	assert.ErrorAs(t, err, &got)
}

func TestWrapTurnError_Transport(t *testing.T) {
	cause := errors.New("connection reset")
	err := wrapTurnError(&providers.ProviderTransportError{Cause: cause}, "claude")

	var pErr *ProviderError
	require.ErrorAs(t, err, &pErr)
	assert.Equal(t, "claude", pErr.Provider, "falls back to the conversation's provider")
	assert.Zero(t, pErr.StatusCode)
	assert.ErrorIs(t, err, cause)
	assert.True(t, providers.IsTransient(err))
}

func TestWrapTurnError_LeavesOtherErrorsAlone(t *testing.T) {
	assert.NoError(t, wrapTurnError(nil, "x"))

	plain := errors.New("boom")
	assert.Same(t, plain, wrapTurnError(plain, "x"))

	already := &ProviderError{Provider: "p", Message: "m", Cause: &providers.ProviderHTTPError{StatusCode: 500}}
	assert.Same(t, already, wrapTurnError(already, "x"), "no double wrapping")
}

func TestPackLoadError(t *testing.T) {
	cause := fmt.Errorf("%w: /abs/x.json", ErrPackNotFound)
	err := packLoadError("x.json", cause)

	var pErr *PackError
	require.ErrorAs(t, err, &pErr)
	assert.Equal(t, "x.json", pErr.Path)
	assert.ErrorIs(t, err, ErrPackNotFound)
	assert.Same(t, err, packLoadError("other.json", err), "no double wrapping")
}

// A failing approved handler gives a *ToolError naming the tool, while the
// model is still told only the handler's own error.
func TestResolveTool_HandlerFailureIsToolError(t *testing.T) {
	conv := newTestConversation()
	conv.pendingStore = sdktools.NewMemoryPendingStore()
	defer func() { _ = conv.pendingStore.(sdktools.Closer).Close() }()
	conv.resolvedStore = sdktools.NewResolvedStore()

	cause := errors.New("refund API down")
	addPending(t, conv, "call-1", "process_refund", map[string]any{"amount": 5},
		func(map[string]any) (any, error) { return nil, cause })

	resolution, err := conv.ResolveTool(context.Background(), "call-1")
	require.NoError(t, err)

	var tErr *ToolError
	require.ErrorAs(t, resolution.Error, &tErr)
	assert.Equal(t, "process_refund", tErr.ToolName)
	assert.ErrorIs(t, resolution.Error, cause)
	assert.Equal(t, "refund API down", toolResultErrorText(resolution.Error))
	assert.Equal(t, "Tool error: refund API down", toolErrorContent(resolution))
}

func TestToolResultErrorText(t *testing.T) {
	assert.Equal(t, "plain", toolResultErrorText(errors.New("plain")))
	assert.Equal(t, "cause", toolResultErrorText(&ToolError{ToolName: "t", Cause: errors.New("cause")}))
}
