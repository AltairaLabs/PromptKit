package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
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

// maxCharsChunkInterceptor is a test ChunkInterceptor that aborts when
// accumulated content exceeds maxChars.
type maxCharsChunkInterceptor struct {
	maxChars    int
	accumulated string
}

func (h *maxCharsChunkInterceptor) Name() string { return "max_chars_test" }

func (h *maxCharsChunkInterceptor) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *maxCharsChunkInterceptor) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *maxCharsChunkInterceptor) OnChunk(
	_ context.Context, chunk *providers.StreamChunk,
) hooks.Decision {
	h.accumulated += chunk.Delta
	if len(h.accumulated) > h.maxChars {
		return hooks.Deny(fmt.Sprintf(
			"content length %d exceeds max %d", len(h.accumulated), h.maxChars,
		))
	}
	return hooks.Allow
}

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

// redactingProviderHook rewrites the last message's content in place and allows.
type redactingProviderHook struct {
	replacement string
}

func (h *redactingProviderHook) Name() string { return "redactor" }

func (h *redactingProviderHook) BeforeCall(
	_ context.Context, req *hooks.ProviderRequest,
) hooks.Decision {
	if len(req.Messages) > 0 {
		req.Messages[len(req.Messages)-1].Content = h.replacement
	}
	return hooks.Allow
}

func (h *redactingProviderHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

// redactionRecordingProvider captures the PredictionRequest the provider
// actually received, on whichever path the stage takes. Named distinctly from
// the package's other recordingProvider (composition_executor_test.go), which
// records only metadata and isn't a providers.Provider wrapper.
//
// streaming selects the path: false (the zero value) routes the stage through
// executeRound, true through executeStreamingRound. Both Predict and
// PredictStream record, so the same "provider never called" / "provider saw the
// mutated request" proofs hold on either path.
type redactionRecordingProvider struct {
	providers.Provider
	streaming bool
	mu        sync.Mutex
	requests  []providers.PredictionRequest
	calls     int
}

func (p *redactionRecordingProvider) SupportsStreaming() bool { return p.streaming }

func (p *redactionRecordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req)
	return p.Provider.Predict(ctx, req)
}

func (p *redactionRecordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	return p.Provider.PredictStream(ctx, req)
}

func (p *redactionRecordingProvider) record(req providers.PredictionRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
	p.calls++
}

func (p *redactionRecordingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *redactionRecordingProvider) lastRequest() providers.PredictionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[len(p.requests)-1]
}

// TestProviderStage_BeforeCallHook_RedactionSurvivesNormalize proves a BeforeCall
// hook's mutation of req.Messages reaches the provider even when a system message
// is present — NormalizeMessages copies the slice, so the hook must run first.
//
// Run on both round paths: the reorder had to be applied to executeRound AND
// executeStreamingRound, and streaming is the path most real providers take, so
// a unary-only test would leave the streaming half of the fix unpinned.
func TestProviderStage_BeforeCallHook_RedactionSurvivesNormalize(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(roundPathName(streaming), func(t *testing.T) {
			provider := &redactionRecordingProvider{
				Provider:  mock.NewProvider("p", "m", false),
				streaming: streaming,
			}

			reg := hooks.NewRegistry(hooks.WithProviderHook(&redactingProviderHook{
				replacement: "[REDACTED]",
			}))

			stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
				MaxTokens: 100,
			}, nil, reg)

			// A system-role message in the slice forces NormalizeMessages to
			// rebuild req.Messages as a copy — the exact condition that used to
			// drop mutations.
			input := make(chan StreamElement, 2)
			sysMsg := types.Message{Role: "system", Content: "be terse"}
			userMsg := types.Message{Role: "user", Content: "my SSN is 123-45-6789"}
			input <- NewMessageElement(&sysMsg)
			input <- NewMessageElement(&userMsg)
			close(input)

			output := make(chan StreamElement, 50)
			require.NoError(t, stage.Process(context.Background(), input, output))
			for range output { //nolint:revive // drain
			}

			require.Equal(t, 1, provider.callCount())
			got := provider.lastRequest()
			require.NotEmpty(t, got.Messages)
			assert.Equal(t, "[REDACTED]", got.Messages[len(got.Messages)-1].Content,
				"BeforeCall mutation must reach the provider")
			assert.NotContains(t, got.Messages[len(got.Messages)-1].Content, "123-45-6789")
		})
	}
}

// roundPathName labels a subtest by the ProviderStage round path a provider's
// SupportsStreaming() selects.
func roundPathName(streaming bool) string {
	if streaming {
		return "streaming"
	}
	return "unary"
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
		roundRef{round: 1},
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

	// Use a simple chunk interceptor that aborts when content exceeds 5 chars.
	interceptor := &maxCharsChunkInterceptor{maxChars: 5}

	reg := hooks.NewRegistry(hooks.WithProviderHook(interceptor))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	_, err := runProviderStage(t, stage, "tell me a long story")

	// The stream should be aborted because accumulated content exceeds 5 chars.
	require.Error(t, err)
	var abortErr *providers.ValidationAbortError
	require.ErrorAs(t, err, &abortErr)
	assert.Contains(t, abortErr.Reason, "exceeds max")
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

	results, err := stage.executeToolCalls(context.Background(), calls, roundRef{round: 1})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].ToolResult.Error, "blocked by hook")
	assert.Contains(t, results[0].ToolResult.GetTextContent(), "policy forbids this tool")
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

	results, err := stage.executeToolCalls(context.Background(), calls, roundRef{round: 1})

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
// Tool hook metadata threading tests
// =============================================================================

// denyToolHookWithMetadata blocks tools and includes metadata in the decision.
type denyToolHookWithMetadata struct {
	blockedTool string
	reason      string
	metadata    map[string]any
}

func (h *denyToolHookWithMetadata) Name() string { return "deny_tool_meta" }

func (h *denyToolHookWithMetadata) BeforeExecution(
	_ context.Context, req hooks.ToolRequest,
) hooks.Decision {
	if req.Name == h.blockedTool {
		return hooks.DenyWithMetadata(h.reason, h.metadata)
	}
	return hooks.Allow
}

func (h *denyToolHookWithMetadata) AfterExecution(
	_ context.Context, _ hooks.ToolRequest, _ hooks.ToolResponse,
) hooks.Decision {
	return hooks.Allow
}

// allowToolHookWithMetadata allows execution but includes metadata in the decision.
type allowToolHookWithMetadata struct {
	metadata map[string]any
}

func (h *allowToolHookWithMetadata) Name() string { return "allow_tool_meta" }

func (h *allowToolHookWithMetadata) BeforeExecution(
	_ context.Context, _ hooks.ToolRequest,
) hooks.Decision {
	return hooks.Decision{Allow: true, Metadata: h.metadata}
}

func (h *allowToolHookWithMetadata) AfterExecution(
	_ context.Context, _ hooks.ToolRequest, _ hooks.ToolResponse,
) hooks.Decision {
	return hooks.Allow
}

func TestProviderStage_ToolHook_DenyMetadataThreaded(t *testing.T) {
	provider := mock.NewProvider("p", "m", false)
	toolReg := tools.NewRegistry()

	executor := &mockAsyncExecutor{
		name:    "meta-deny-executor",
		status:  tools.ToolStatusComplete,
		content: []byte(`{"ok":true}`),
	}
	toolReg.RegisterExecutor(executor)
	toolReg.Register(&tools.ToolDescriptor{
		Name:        "consent_tool",
		Description: "test",
		Mode:        "meta-deny-executor",
		InputSchema: []byte(`{"type":"object"}`),
	})

	hookReg := hooks.NewRegistry(hooks.WithToolHook(&denyToolHookWithMetadata{
		blockedTool: "consent_tool",
		reason:      "user declined consent",
		metadata:    map[string]any{"consent_status": "denied"},
	}))

	stage := NewProviderStageWithHooks(
		provider, toolReg, nil, nil, nil, hookReg,
	)

	calls := []types.MessageToolCall{
		{ID: "c1", Name: "consent_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), calls, roundRef{round: 1})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].ToolResult.Error, "blocked by hook")
	assert.NotNil(t, results[0].Meta, "expected Meta to be populated from hook decision")
	assert.Equal(t, "denied", results[0].Meta["consent_status"])
}

func TestProviderStage_ToolHook_AllowMetadataThreaded(t *testing.T) {
	provider := mock.NewProvider("p", "m", false)
	toolReg := tools.NewRegistry()

	executor := &mockAsyncExecutor{
		name:    "meta-allow-executor",
		status:  tools.ToolStatusComplete,
		content: []byte(`{"ok":true}`),
	}
	toolReg.RegisterExecutor(executor)
	toolReg.Register(&tools.ToolDescriptor{
		Name:        "consent_tool",
		Description: "test",
		Mode:        "meta-allow-executor",
		InputSchema: []byte(`{"type":"object"}`),
	})

	hookReg := hooks.NewRegistry(hooks.WithToolHook(&allowToolHookWithMetadata{
		metadata: map[string]any{"consent_status": "granted"},
	}))

	stage := NewProviderStageWithHooks(
		provider, toolReg, nil, nil, nil, hookReg,
	)

	calls := []types.MessageToolCall{
		{ID: "c1", Name: "consent_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), calls, roundRef{round: 1})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Empty(t, results[0].ToolResult.Error)
	assert.NotNil(t, results[0].Meta, "expected Meta to be populated from hook decision")
	assert.Equal(t, "granted", results[0].Meta["consent_status"])
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
// Built-in guardrail hooks via NewGuardrailHook
// =============================================================================

func TestProviderStage_BannedWordsGuardrail_NonStreaming(t *testing.T) {
	baseProvider := mock.NewProvider("p", "m", false)
	provider := &nonStreamingProvider{Provider: baseProvider}

	// The mock provider returns "Mock response from p model m"
	// Ban the word "Mock" so the guardrail enforces content replacement.
	bw, err := guardrails.NewGuardrailHook("banned_words", map[string]any{
		"words": []any{"Mock"},
	})
	require.NoError(t, err)
	reg := hooks.NewRegistry(hooks.WithProviderHook(bw))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	elems, runErr := runProviderStage(t, stage, "hello")

	// Guardrails now enforce (replace content) and continue the pipeline
	require.NoError(t, runErr)

	// Find the assistant message and verify it was replaced
	var assistantMsg *types.Message
	for i := range elems {
		if elems[i].Message != nil && elems[i].Message.Role == "assistant" {
			assistantMsg = elems[i].Message
		}
	}
	require.NotNil(t, assistantMsg, "expected an assistant message")
	assert.Contains(t, assistantMsg.Content, "content policy",
		"content should be replaced with blocked message")
	require.NotEmpty(t, assistantMsg.Validations)
	assert.Equal(t, "banned_words", assistantMsg.Validations[0].ValidatorType)
	assert.False(t, assistantMsg.Validations[0].Passed)
}

func TestProviderStage_GuardrailEmitsValidationEvent(t *testing.T) {
	baseProvider := mock.NewProvider("p", "m", false)
	provider := &nonStreamingProvider{Provider: baseProvider}

	bw, err := guardrails.NewGuardrailHook("banned_words", map[string]any{
		"words": []any{"Mock"},
	})
	require.NoError(t, err)
	reg := hooks.NewRegistry(hooks.WithProviderHook(bw))

	bus := events.NewEventBus()
	defer bus.Close()
	emitter := events.NewEmitter(bus, "run1", "sess1", "conv1")

	var received []*events.Event
	var mu sync.Mutex
	bus.Subscribe(events.EventValidationFailed, func(e *events.Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, emitter, reg)

	_, runErr := runProviderStage(t, stage, "hello")
	require.NoError(t, runErr)

	// Give the async event bus time to deliver
	bus.Close()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 1, "expected one validation.failed event")
	data, ok := received[0].Data.(*events.ValidationEventData)
	require.True(t, ok)
	assert.Equal(t, "banned_words", data.ValidatorName)
	assert.True(t, data.Enforced)
	assert.Len(t, data.Violations, 1)
}

// =============================================================================
// BeforeCall Enforced tests — canned turn, no provider call
// =============================================================================

// enforceBeforeCallHook enforces on BeforeCall, supplying canned text.
type enforceBeforeCallHook struct {
	reason      string
	replacement string
	metadata    map[string]any
}

func (h *enforceBeforeCallHook) Name() string { return "enforce_before" }

func (h *enforceBeforeCallHook) BeforeCall(
	_ context.Context, req *hooks.ProviderRequest,
) hooks.Decision {
	req.Replacement = h.replacement
	return hooks.Enforced(h.reason, h.metadata)
}

func (h *enforceBeforeCallHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

// assistantMessages extracts assistant messages from emitted stream elements.
func assistantMessages(elems []StreamElement) []types.Message {
	var out []types.Message
	for _, e := range elems {
		if e.Message != nil && e.Message.Role == "assistant" {
			out = append(out, *e.Message)
		}
	}
	return out
}

func TestProviderStage_BeforeCallHook_EnforcedReturnsCannedTurnNonStreaming(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}

	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{
		reason:      "input blocked",
		replacement: "I can't help with that.",
		metadata:    map[string]any{"validator_type": "test_input"},
	}))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	elems, err := runProviderStage(t, stage, "something disallowed")

	require.NoError(t, err, "enforced pre-call must not error")
	assert.Equal(t, 0, provider.callCount(), "provider must not be called")

	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, "I can't help with that.", msgs[0].Content)
}

// TestProviderStage_BeforeCallHook_EnforcedReturnsCannedTurnStreaming is the
// streaming counterpart of the test above. The call counter is the load-bearing
// assertion: without it, moving the d.Enforced branch below
// startStreamingRequest would still return the canned text while spending real
// tokens on every blocked turn. Streaming is the default path — mock.NewProvider
// reports SupportsStreaming() == true, as most real providers do.
func TestProviderStage_BeforeCallHook_EnforcedReturnsCannedTurnStreaming(t *testing.T) {
	provider := &redactionRecordingProvider{
		Provider:  mock.NewProvider("p", "m", false),
		streaming: true,
	}

	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{
		reason:      "input blocked",
		replacement: "I can't help with that.",
	}))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	elems, err := runProviderStage(t, stage, "something disallowed")

	require.NoError(t, err, "enforced pre-call must not error")
	assert.Equal(t, 0, provider.callCount(),
		"provider must not be called on the streaming path either — zero tokens")

	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, "I can't help with that.", msgs[0].Content)
	assert.Equal(t, types.FinishReasonSafety, msgs[0].FinishReason)
}

// TestProviderStage_BeforeCallHook_EnforcedFallsBackToDefaultMessage proves the
// canned text falls back to prompt.DefaultBlockedMessage when the hook sets no
// Replacement and no metadata replacement.
func TestProviderStage_BeforeCallHook_EnforcedFallsBackToDefaultMessage(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}

	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{
		reason: "blocked, no text supplied",
	}))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	elems, err := runProviderStage(t, stage, "hi")

	require.NoError(t, err)
	assert.Equal(t, 0, provider.callCount())
	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, prompt.DefaultBlockedMessage, msgs[0].Content)
}

// TestProviderStage_BeforeCallHook_EnforcedViaMetadataReplacement covers exec
// hooks, which can only return JSON metadata — no Go field to write.
func TestProviderStage_BeforeCallHook_EnforcedViaMetadataReplacement(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}

	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{
		reason:   "blocked by subprocess",
		metadata: map[string]any{"replacement": "Blocked by policy."},
	}))

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	elems, err := runProviderStage(t, stage, "hi")

	require.NoError(t, err)
	assert.Equal(t, 0, provider.callCount())
	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Blocked by policy.", msgs[0].Content)
}

// passthroughRecorderStage records every element it sees and forwards it, so a
// test can prove a stage downstream of ProviderStage still ran.
type passthroughRecorderStage struct {
	mu   sync.Mutex
	seen []types.Message
}

func (s *passthroughRecorderStage) Name() string { return "recorder" }

// Type is required by the Stage interface (stage.go:49-56) alongside Name and
// Process. Transform is the 1:1 forwarding shape.
func (s *passthroughRecorderStage) Type() StageType { return StageTypeTransform }

func (s *passthroughRecorderStage) Process(
	ctx context.Context, input <-chan StreamElement, output chan<- StreamElement,
) error {
	defer close(output)
	for elem := range input {
		if elem.Message != nil {
			s.mu.Lock()
			s.seen = append(s.seen, *elem.Message)
			s.mu.Unlock()
		}
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *passthroughRecorderStage) messages() []types.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]types.Message(nil), s.seen...)
}

// TestProviderStage_EnforcedPreCall_DownstreamStagesStillRun proves that an
// enforcing input guardrail skips only the provider work inside ProviderStage:
// the canned turn is emitted, the stage returns normally, and the next stage in
// the pipeline receives it. Guardrails belong to one stage — they must not
// abort the pipeline.
func TestProviderStage_EnforcedPreCall_DownstreamStagesStillRun(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}

	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{
		reason:      "input blocked",
		replacement: "I can't help with that.",
		metadata:    map[string]any{"validator_type": "test_input"},
	}))

	provStage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)
	recorder := &passthroughRecorderStage{}

	pipe, err := NewPipelineBuilder().Chain(provStage, recorder).Build()
	require.NoError(t, err)

	userMsg := types.Message{Role: "user", Content: "something disallowed"}
	_, err = pipe.ExecuteSync(context.Background(), NewMessageElement(&userMsg))
	require.NoError(t, err, "enforcement must not fail the pipeline")

	assert.Equal(t, 0, provider.callCount(), "provider must not be called")

	seen := recorder.messages()
	var blocked *types.Message
	for i := range seen {
		if seen[i].Role == "assistant" {
			blocked = &seen[i]
		}
	}
	require.NotNil(t, blocked, "downstream stage must receive the canned assistant turn")
	assert.Equal(t, "I can't help with that.", blocked.Content)
	assert.Equal(t, types.FinishReasonSafety, blocked.FinishReason,
		"blocked turn must be marked so downstream stages can detect the skip")
}

// =============================================================================
// AfterCall Enforced tests — enforcement must drop tool calls and stop the loop
// =============================================================================

// enforceAfterCallHook enforces on AfterCall, replacing the response content.
type enforceAfterCallHook struct {
	replacement string
}

func (h *enforceAfterCallHook) Name() string { return "enforce_after" }

func (h *enforceAfterCallHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforceAfterCallHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	resp.Message.Content = h.replacement
	return hooks.Enforced("output blocked", map[string]any{
		"validator_type": "test_output",
	})
}

// toolCallingProvider always returns one tool call plus content, so we can
// prove an enforced guardrail drops the call instead of executing it.
type toolCallingProvider struct {
	providers.Provider
	mu    sync.Mutex
	calls int
}

func (p *toolCallingProvider) SupportsStreaming() bool { return false }

func (p *toolCallingProvider) Predict(
	_ context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return providers.PredictionResponse{
		Content: "let me look that up",
		ToolCalls: []types.MessageToolCall{
			{ID: "call_1", Name: "search", Args: json.RawMessage(`{"q":"x"}`)},
		},
	}, nil
}

func (p *toolCallingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// TestProviderStage_AfterCallHook_EnforcedDropsToolCallsAndStops proves an
// enforced output guardrail stops the round loop: the tool is not executed, the loop
// does not run a second round, and the recorded message carries no tool calls
// (an assistant message with tool calls but no tool results is a provider
// protocol error on the following turn).
func TestProviderStage_AfterCallHook_EnforcedDropsToolCallsAndStops(t *testing.T) {
	provider := &toolCallingProvider{Provider: mock.NewProvider("p", "m", false)}

	reg := hooks.NewRegistry(hooks.WithProviderHook(
		&enforceAfterCallHook{replacement: "I can't help with that."},
	))

	// Deliberately no tool registry. executeToolCalls errors with "tool
	// registry not configured but tool calls present" if the loop tries to
	// run the dropped calls, so a leak shows up as a hard error.
	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	elems, err := runProviderStage(t, stage, "search for x")

	require.NoError(t, err, "enforced response must not attempt tool execution")
	assert.Equal(t, 1, provider.callCount(), "loop must not run a second round")

	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, "I can't help with that.", msgs[0].Content)
	assert.Empty(t, msgs[0].ToolCalls, "enforced response must not retain tool calls")
	assert.Equal(t, types.FinishReasonSafety, msgs[0].FinishReason,
		"enforced response must be marked like a policy-dropped tool call")
}

// toolCallingStreamProvider is the streaming counterpart of
// toolCallingProvider: SupportsStreaming reports true so the stage takes the
// executeStreamingRound path, and PredictStream always emits one tool call
// plus content, so the same enforcement-drops-tool-calls proof can run
// against the streaming round loop.
type toolCallingStreamProvider struct {
	providers.Provider
	mu    sync.Mutex
	calls int
}

func (p *toolCallingStreamProvider) SupportsStreaming() bool { return true }

func (p *toolCallingStreamProvider) PredictStream(
	_ context.Context, _ providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()

	finish := "tool_calls"
	ch := make(chan providers.StreamChunk, 1)
	ch <- providers.StreamChunk{
		Content: "let me look that up",
		Delta:   "let me look that up",
		ToolCalls: []types.MessageToolCall{
			{ID: "call_1", Name: "search", Args: json.RawMessage(`{"q":"x"}`)},
		},
		FinishReason: &finish,
	}
	close(ch)
	return ch, nil
}

func (p *toolCallingStreamProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// TestProviderStage_AfterCallHook_EnforcedDropsToolCallsAndStops_Streaming is
// the streaming counterpart of the test above: it proves the identical fix
// applied to executeStreamingRound also drops tool calls and stops the loop,
// and stamps the same skip indicators (FinishReasonSafety) that
// blockedMessage uses on the BeforeCall path.
func TestProviderStage_AfterCallHook_EnforcedDropsToolCallsAndStops_Streaming(t *testing.T) {
	provider := &toolCallingStreamProvider{Provider: mock.NewProvider("p", "m", false)}

	reg := hooks.NewRegistry(hooks.WithProviderHook(
		&enforceAfterCallHook{replacement: "I can't help with that."},
	))

	// Deliberately no tool registry — a dropped-call leak surfaces as a hard
	// error the same way it does on the non-streaming path.
	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	elems, err := runProviderStage(t, stage, "search for x")

	require.NoError(t, err, "enforced response must not attempt tool execution")
	assert.Equal(t, 1, provider.callCount(), "loop must not run a second round")

	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, "I can't help with that.", msgs[0].Content)
	assert.Empty(t, msgs[0].ToolCalls, "enforced response must not retain tool calls")
	assert.Equal(t, types.FinishReasonSafety, msgs[0].FinishReason,
		"enforced response must be marked like a policy-dropped tool call")
}

// =============================================================================
// Direction tagging — pre-call firing observability parity (Task 4)
// =============================================================================

// collectValidationFailures subscribes to validation.failed and returns a
// getter that closes the bus (flushing async delivery) and returns the data.
func collectValidationFailures(
	t *testing.T, bus *events.EventBus,
) func() []*events.ValidationEventData {
	t.Helper()
	var mu sync.Mutex
	var received []*events.Event
	bus.Subscribe(events.EventValidationFailed, func(e *events.Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})
	return func() []*events.ValidationEventData {
		bus.Close() // flush: the bus dispatches on a worker pool
		mu.Lock()
		defer mu.Unlock()
		out := make([]*events.ValidationEventData, 0, len(received))
		for _, e := range received {
			if d, ok := e.Data.(*events.ValidationEventData); ok {
				out = append(out, d)
			}
		}
		return out
	}
}

// TestProviderStage_BeforeCallHook_EnforcedRecordsFiring proves a pre-call
// guardrail firing is observable: stamped on the canned message's Validations
// (the record that seeds EvalContext.PriorResults for the guardrail_triggered
// assertion) and emitted as a validation event tagged direction=input.
func TestProviderStage_BeforeCallHook_EnforcedRecordsFiring(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}

	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{
		reason:      "pii detected",
		replacement: "I can't help with that.",
		metadata:    map[string]any{"validator_type": "pii_leakage"},
	}))

	elems, getEvents := runStageCollectingValidations(t, provider, reg, "my SSN is 123-45-6789")
	assert.Equal(t, 0, provider.callCount())

	// Validation record lands on the canned assistant message.
	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Validations, 1)
	assert.Equal(t, "pii_leakage", msgs[0].Validations[0].ValidatorType)
	assert.False(t, msgs[0].Validations[0].Passed)
	assert.Equal(t, "input", msgs[0].Validations[0].Details["direction"])

	// Event emitted, tagged as an input firing.
	got := getEvents()
	require.Len(t, got, 1)
	assert.Equal(t, "pii_leakage", got[0].ValidatorType)
	assert.Equal(t, "input", got[0].Direction)
	assert.True(t, got[0].Enforced)
	assert.Contains(t, got[0].Violations, "pii detected")
}

// TestProviderStage_AfterCallHook_EnforcedTaggedOutput pins that the existing
// output path reports direction=output through the shared helper.
func TestProviderStage_AfterCallHook_EnforcedTaggedOutput(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}

	reg := hooks.NewRegistry(hooks.WithProviderHook(
		&enforceAfterCallHook{replacement: "I can't help with that."},
	))

	_, getEvents := runStageCollectingValidations(t, provider, reg, "hi")

	got := getEvents()
	require.Len(t, got, 1)
	assert.Equal(t, "output", got[0].Direction)
}

// runStageCollectingValidations runs one turn through a ProviderStage wired to
// reg, and returns the emitted elements plus a getter for the validation.failed
// events. The getter closes the bus to flush async delivery, so call it after
// asserting on the elements.
func runStageCollectingValidations(
	t *testing.T, provider providers.Provider, reg *hooks.Registry, userText string,
) ([]StreamElement, func() []*events.ValidationEventData) {
	t.Helper()

	bus := events.NewEventBus()
	getEvents := collectValidationFailures(t, bus)
	emitter := events.NewEmitter(bus, "run1", "sess1", "conv1")

	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, emitter, reg)

	elems, err := runProviderStage(t, stage, userText)
	require.NoError(t, err)
	return elems, getEvents
}

// TestProviderStage_InputGuardrail_EvaluatesOncePerToolLoop proves the real
// input guardrail inspects user input on the first round only. Round 2 of a
// tool loop ends in a tool-result message, so the guardrail must not re-fire —
// it would score the wrong content and rebill an LLM-judged check every round.
func TestProviderStage_InputGuardrail_EvaluatesOncePerToolLoop(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{
		"max_characters": 5,
		"direction":      "input",
	})
	require.NoError(t, err)

	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}
	reg := hooks.NewRegistry(hooks.WithProviderHook(guard))
	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	// Round 1: the turn's user message is last and exceeds max_characters, so
	// the guardrail fires — the provider is never called.
	round1 := []types.Message{
		{Role: "user", Content: "search for x"},
	}
	msg1, hasTools1, err := stage.executeRound(
		context.Background(), round1, "sys", nil, "", roundRef{round: 1}, nil)
	require.NoError(t, err)
	assert.False(t, hasTools1)
	assert.Equal(t, types.FinishReasonSafety, msg1.FinishReason,
		"round 1 must be blocked by the input guardrail")
	assert.Equal(t, 0, provider.callCount(), "blocked round must not call the provider")

	// Round 2: the assistant asked for a tool and the result came back, so the
	// tail is a tool message — not new user input. The guardrail must stand down
	// even though the tool result is also longer than max_characters.
	round2 := []types.Message{
		{Role: "user", Content: "search for x"},
		{Role: "assistant", Content: "looking that up"},
		{Role: "tool", Content: `{"result":"a much longer tool payload"}`},
	}
	msg2, _, err := stage.executeRound(
		context.Background(), round2, "sys", nil, "", roundRef{round: 2}, nil)
	require.NoError(t, err)
	// Asserting on FinishReason here would be vacuous — mock.Provider never sets
	// one, so NotEqual(FinishReasonSafety) holds however the round went. A
	// re-fire would record a validation, so an empty Validations slice is the
	// assertion that can actually fail.
	assert.Empty(t, msg2.Validations,
		"round 2 must not be blocked — its tail is a tool result, not user input")
	assert.Equal(t, 1, provider.callCount(), "round 2 must reach the provider")
}
