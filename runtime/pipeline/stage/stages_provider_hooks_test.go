package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Hook Integration Tests — ProviderStage with hooks.Registry
// =============================================================================

// denyAllProviderHook is a test hook that denies all provider calls.
type denyAllProviderHook struct {
	reason   string
	metadata map[string]any
}

func (h *denyAllProviderHook) Name() string { return "deny_all" }

func (h *denyAllProviderHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.DenyWithMetadata(h.reason, h.metadata)
}

func (h *denyAllProviderHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

// allowBeforeDenyAfterHook allows BeforeCall but denies AfterCall.
type allowBeforeDenyAfterHook struct {
	reason   string
	metadata map[string]any
}

func (h *allowBeforeDenyAfterHook) Name() string { return "deny_after" }

func (h *allowBeforeDenyAfterHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *allowBeforeDenyAfterHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.DenyWithMetadata(h.reason, h.metadata)
}

// denyToolHook blocks specific tools by name.
type denyToolHook struct {
	blockedTool string
	reason      string
}

func (h *denyToolHook) Name() string { return "deny_tool" }

func (h *denyToolHook) BeforeExecution(
	_ context.Context, req hooks.ToolRequest,
) hooks.Decision {
	if req.Name == h.blockedTool {
		return hooks.Deny(h.reason)
	}
	return hooks.Allow
}

func (h *denyToolHook) AfterExecution(
	_ context.Context, _ hooks.ToolRequest, _ hooks.ToolResponse,
) hooks.Decision {
	return hooks.Allow
}

// recordingToolHook records AfterExecution calls for assertion.
type recordingToolHook struct {
	afterCalls []hooks.ToolResponse
}

func (h *recordingToolHook) Name() string { return "recording_tool" }

func (h *recordingToolHook) BeforeExecution(
	_ context.Context, _ hooks.ToolRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *recordingToolHook) AfterExecution(
	_ context.Context, _ hooks.ToolRequest, resp hooks.ToolResponse,
) hooks.Decision {
	h.afterCalls = append(h.afterCalls, resp)
	return hooks.Allow
}

// --- Helper to run a ProviderStage end-to-end with a single user message ---

func runProviderStage(
	t *testing.T,
	stage *ProviderStage,
	userContent string,
) ([]StreamElement, error) {
	t.Helper()
	input := make(chan StreamElement, 1)
	msg := types.Message{Role: "user", Content: userContent}
	elem := NewMessageElement(&msg)
	elem.Metadata["system_prompt"] = "test"
	input <- elem
	close(input)

	output := make(chan StreamElement, 50)
	err := stage.Process(context.Background(), input, output)

	var elems []StreamElement
	for e := range output {
		elems = append(elems, e)
	}
	return elems, err
}

// =============================================================================
// BeforeCall hook tests
// =============================================================================

func TestProviderStage_BeforeCallHook_DeniesNonStreaming(t *testing.T) {
	baseProvider := mock.NewProvider("p", "m", false)
	provider := &nonStreamingProvider{Provider: baseProvider}

	reg := hooks.NewRegistry(hooks.WithProviderHook(&denyAllProviderHook{
		reason:   "blocked by policy",
		metadata: map[string]any{"rule": "test"},
	}))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	_, err := runProviderStage(t, stage, "hello")

	require.Error(t, err)
	var denied *hooks.HookDeniedError
	require.ErrorAs(t, err, &denied)
	assert.Equal(t, "provider_before", denied.HookType)
	assert.Equal(t, "blocked by policy", denied.Reason)
	assert.Equal(t, "test", denied.Metadata["rule"])
}

func TestProviderStage_BeforeCallHook_DeniesStreaming(t *testing.T) {
	provider := mock.NewProvider("p", "m", false)

	reg := hooks.NewRegistry(hooks.WithProviderHook(&denyAllProviderHook{
		reason: "streaming blocked",
	}))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	_, err := runProviderStage(t, stage, "hello")

	require.Error(t, err)
	var denied *hooks.HookDeniedError
	require.ErrorAs(t, err, &denied)
	assert.Equal(t, "provider_before", denied.HookType)
	assert.Equal(t, "streaming blocked", denied.Reason)
}

// =============================================================================
// AfterCall hook tests
// =============================================================================

func TestProviderStage_AfterCallHook_DeniesNonStreaming(t *testing.T) {
	baseProvider := mock.NewProvider("p", "m", false)
	provider := &nonStreamingProvider{Provider: baseProvider}

	reg := hooks.NewRegistry(hooks.WithProviderHook(&allowBeforeDenyAfterHook{
		reason: "response rejected",
		metadata: map[string]any{
			"validator_type": "test_validator",
			"detail":         "too long",
		},
	}))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	_, err := runProviderStage(t, stage, "hello")

	require.Error(t, err)
	var denied *hooks.HookDeniedError
	require.ErrorAs(t, err, &denied)
	assert.Equal(t, "provider_after", denied.HookType)
	assert.Equal(t, "response rejected", denied.Reason)
}

func TestProviderStage_AfterCallHook_DeniesStreaming(t *testing.T) {
	provider := mock.NewProvider("p", "m", false)

	reg := hooks.NewRegistry(hooks.WithProviderHook(&allowBeforeDenyAfterHook{
		reason: "streaming response rejected",
		metadata: map[string]any{
			"validator_type": "test_validator",
		},
	}))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	_, err := runProviderStage(t, stage, "hello")

	require.Error(t, err)
	var denied *hooks.HookDeniedError
	require.ErrorAs(t, err, &denied)
	assert.Equal(t, "provider_after", denied.HookType)
}

// =============================================================================
// AfterCall guardrail_triggered compatibility
// =============================================================================

func TestProviderStage_AfterCallHook_PopulatesValidations(t *testing.T) {
	// When a hook denial includes validator_type in metadata,
	// the response msg.Validations should be populated for compat.
	baseProvider := mock.NewProvider("p", "m", false)
	provider := &nonStreamingProvider{Provider: baseProvider}

	reg := hooks.NewRegistry(hooks.WithProviderHook(&allowBeforeDenyAfterHook{
		reason: "banned word found",
		metadata: map[string]any{
			"validator_type": "banned_words",
			"words":          []string{"badword"},
		},
	}))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	// executeRound populates the returned message's Validations
	// even though it also returns an error.
	msg, _, err := stage.executeRound(
		context.Background(),
		[]types.Message{{Role: "user", Content: "test"}},
		"system",
		nil,
		"",
		1,
		nil,
	)

	require.Error(t, err)
	require.Len(t, msg.Validations, 1)
	assert.Equal(t, "banned_words", msg.Validations[0].ValidatorType)
	assert.False(t, msg.Validations[0].Passed)
}

// =============================================================================
// Chunk interceptor hook tests
// =============================================================================

func TestProviderStage_ChunkInterceptor_AbortsStream(t *testing.T) {
	provider := mock.NewProvider("p", "m", false)

	// LengthHook implements ChunkInterceptor. Set a very low char limit
	// so the streamed content will exceed it.
	lengthHook := guardrails.NewLengthHook(5, 0) // 5 chars max

	reg := hooks.NewRegistry(hooks.WithProviderHook(lengthHook))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	_, err := runProviderStage(t, stage, "tell me a long story")

	// The stream should be aborted because accumulated content exceeds 5 chars.
	require.Error(t, err)
	var abortErr *providers.ValidationAbortError
	require.ErrorAs(t, err, &abortErr)
	assert.Contains(t, abortErr.Reason, "max_characters")
}

// =============================================================================
// Nil hook registry (zero overhead path)
// =============================================================================

func TestProviderStage_NilHookRegistry_NoOverhead(t *testing.T) {
	provider := mock.NewProvider("p", "m", false)

	// Nil hookRegistry — should work exactly as before hooks were added.
	stage := NewProviderStageWithHooks(
		provider, nil, nil, &ProviderConfig{MaxTokens: 100}, nil, nil,
	)

	elems, err := runProviderStage(t, stage, "hello")

	require.NoError(t, err)
	assert.Greater(t, len(elems), 0)
}

// =============================================================================
// Tool hook tests
// =============================================================================

func TestProviderStage_ToolHook_BlocksToolExecution(t *testing.T) {
	provider := mock.NewProvider("p", "m", false)
	toolReg := tools.NewRegistry()

	executor := &mockAsyncExecutor{
		name:    "hook-test-executor",
		status:  tools.ToolStatusComplete,
		content: []byte(`{"ok":true}`),
	}
	toolReg.RegisterExecutor(executor)
	toolReg.Register(&tools.ToolDescriptor{
		Name:        "blocked_by_hook",
		Description: "test",
		Mode:        "hook-test-executor",
		InputSchema: []byte(`{"type":"object"}`),
	})

	hookReg := hooks.NewRegistry(hooks.WithToolHook(&denyToolHook{
		blockedTool: "blocked_by_hook",
		reason:      "policy forbids this tool",
	}))

	stage := NewProviderStageWithHooks(
		provider, toolReg, nil, nil, nil, hookReg,
	)

	calls := []types.MessageToolCall{
		{ID: "c1", Name: "blocked_by_hook", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), calls)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].ToolResult.Error, "blocked by hook")
	assert.Contains(t, results[0].ToolResult.Content, "policy forbids this tool")
}

func TestProviderStage_ToolHook_AfterExecution_Records(t *testing.T) {
	provider := mock.NewProvider("p", "m", false)
	toolReg := tools.NewRegistry()

	executor := &mockAsyncExecutor{
		name:    "recording-executor",
		status:  tools.ToolStatusComplete,
		content: []byte(`{"value":42}`),
	}
	toolReg.RegisterExecutor(executor)
	toolReg.Register(&tools.ToolDescriptor{
		Name:        "observed_tool",
		Description: "test",
		Mode:        "recording-executor",
		InputSchema: []byte(`{"type":"object"}`),
	})

	recorder := &recordingToolHook{}
	hookReg := hooks.NewRegistry(hooks.WithToolHook(recorder))

	stage := NewProviderStageWithHooks(
		provider, toolReg, nil, nil, nil, hookReg,
	)

	calls := []types.MessageToolCall{
		{ID: "c1", Name: "observed_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), calls)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Empty(t, results[0].ToolResult.Error)

	// AfterExecution should have been called
	require.Len(t, recorder.afterCalls, 1)
	assert.Equal(t, "observed_tool", recorder.afterCalls[0].Name)
	assert.Equal(t, "c1", recorder.afterCalls[0].CallID)
	assert.Greater(t, recorder.afterCalls[0].LatencyMs, int64(-1))
}

// =============================================================================
// Constructor chaining
// =============================================================================

func TestNewProviderStageWithHooks_ChainsFromExisting(t *testing.T) {
	provider := mock.NewProvider("p", "m", false)

	// NewProviderStage → NewProviderStageWithEmitter → NewProviderStageWithHooks
	s1 := NewProviderStage(provider, nil, nil, nil)
	assert.Nil(t, s1.hookRegistry)
	assert.Nil(t, s1.emitter)

	reg := hooks.NewRegistry()
	s2 := NewProviderStageWithHooks(provider, nil, nil, nil, nil, reg)
	assert.NotNil(t, s2.hookRegistry)
}

// =============================================================================
// Built-in guardrail hooks (BannedWordsHook) as provider hook
// =============================================================================

func TestProviderStage_BannedWordsGuardrail_NonStreaming(t *testing.T) {
	baseProvider := mock.NewProvider("p", "m", false)
	provider := &nonStreamingProvider{Provider: baseProvider}

	// The mock provider returns "Mock response from p model m"
	// Ban the word "Mock" so the guardrail rejects it.
	bw := guardrails.NewBannedWordsHook([]string{"Mock"})
	reg := hooks.NewRegistry(hooks.WithProviderHook(bw))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	_, err := runProviderStage(t, stage, "hello")

	require.Error(t, err)
	var denied *hooks.HookDeniedError
	require.ErrorAs(t, err, &denied)
	assert.Equal(t, "provider_after", denied.HookType)
	assert.Contains(t, denied.Reason, "banned")
}
