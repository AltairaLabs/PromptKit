package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// ---------------------------------------------------------------------------
// errorProvider — a minimal Provider that always returns an error from Predict
// ---------------------------------------------------------------------------

var errProviderFault = errors.New("simulated provider fault")

type errorProvider struct {
	base.Implementation
}

func (e *errorProvider) ID() string    { return "error-provider" }
func (e *errorProvider) Model() string { return "error-model" }

func (e *errorProvider) Predict(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, errProviderFault
}

func (e *errorProvider) PredictStream(_ context.Context, _ providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, errProviderFault
}

func (e *errorProvider) SupportsStreaming() bool      { return false }
func (e *errorProvider) ShouldIncludeRawOutput() bool { return false }
func (e *errorProvider) Close() error                 { return nil }
func (e *errorProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}

// ---------------------------------------------------------------------------
// 7.1 — Provider failure propagation
// ---------------------------------------------------------------------------

func TestErrors_ProviderFailurePropagation(t *testing.T) {
	packPath := writePackFile(t, minimalPackJSON)
	ep := &errorProvider{}

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(ep),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	_, sendErr := conv.Send(context.Background(), "trigger error")
	require.Error(t, sendErr, "Send should propagate the provider error")
	assert.ErrorContains(t, sendErr, "simulated provider fault")
}

func TestErrors_ProviderFailureEmitsPipelineFailed(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	packPath := writePackFile(t, minimalPackJSON)
	ep := &errorProvider{}

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(ep),
		sdk.WithSkipSchemaValidation(),
		sdk.WithEventBus(bus),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	_, _ = conv.Send(context.Background(), "trigger error")

	// Wait for the pipeline.failed event.
	found := ec.waitForEvent(events.EventPipelineFailed, 2*time.Second)
	assert.True(t, found, "pipeline.failed event should be emitted on provider error")

	failed := ec.ofType(events.EventPipelineFailed)
	require.NotEmpty(t, failed)
	data, ok := failed[0].Data.(*events.PipelineFailedData)
	require.True(t, ok, "pipeline.failed Data should be *PipelineFailedData")
	require.NotNil(t, data.Error, "pipeline.failed should carry an error")
	assert.ErrorContains(t, data.Error, "simulated provider fault")
}

// ---------------------------------------------------------------------------
// 7.2 — Pack loading errors
// ---------------------------------------------------------------------------

func TestErrors_PackNotFound(t *testing.T) {
	provider := mock.NewProvider("mock-test", "mock-model", false)

	_, err := sdk.Open("nonexistent/path.pack.json", "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.Error(t, err, "Open should fail for nonexistent pack path")

	// The error is a *PackError naming the path, and still the sentinel.
	var packErr *sdk.PackError
	require.ErrorAs(t, err, &packErr)
	assert.Contains(t, packErr.Path, "nonexistent/path.pack.json")
	assert.ErrorIs(t, err, sdk.ErrPackNotFound)
}

func TestErrors_MalformedPackIsPackError(t *testing.T) {
	packPath := writePackFile(t, `{"id": "broken",`)

	_, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(mock.NewProvider("mock-test", "mock-model", false)),
		sdk.WithSkipSchemaValidation(),
	)
	var packErr *sdk.PackError
	require.ErrorAs(t, err, &packErr)
	assert.Equal(t, packPath, packErr.Path)
	assert.ErrorContains(t, err, "failed to load pack")

	_, err = sdk.ValidatePack(packPath, true)
	require.ErrorAs(t, err, &packErr, "ValidatePack reports a load failure as a *PackError")
}

// ---------------------------------------------------------------------------
// 7.1b — Provider HTTP failures are *sdk.ProviderError
// ---------------------------------------------------------------------------

// httpErrorProvider fails every call the way a provider API answering 429 does.
type httpErrorProvider struct {
	errorProvider
}

func (e *httpErrorProvider) httpErr() error {
	return &providers.ProviderHTTPError{
		StatusCode: 429, URL: "https://api.example.com/v1/chat", Body: "rate limited", Provider: "example",
	}
}

func (e *httpErrorProvider) Predict(context.Context, providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, e.httpErr()
}

func (e *httpErrorProvider) PredictStream(context.Context, providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, e.httpErr()
}

func TestErrors_ProviderHTTPFailureIsProviderError(t *testing.T) {
	conv, err := sdk.Open(writePackFile(t, minimalPackJSON), "chat",
		sdk.WithProvider(&httpErrorProvider{}),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	assertProviderError := func(t *testing.T, err error) {
		t.Helper()
		var pErr *sdk.ProviderError
		require.ErrorAs(t, err, &pErr)
		assert.Equal(t, "example", pErr.Provider)
		assert.Equal(t, 429, pErr.StatusCode)
		assert.Contains(t, pErr.Error(), "rate limited")
		var httpErr *providers.ProviderHTTPError
		assert.ErrorAs(t, err, &httpErr, "the runtime error stays reachable")
		assert.True(t, providers.IsTransient(err), "a 429 is still classified as transient")
	}

	_, sendErr := conv.Send(context.Background(), "hello")
	assertProviderError(t, sendErr)

	var streamErr error
	for chunk := range conv.Stream(context.Background(), "hello") {
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
	}
	assertProviderError(t, streamErr)
}

func TestErrors_PromptNotFound(t *testing.T) {
	packPath := writePackFile(t, minimalPackJSON)
	provider := mock.NewProvider("mock-test", "mock-model", false)

	_, err := sdk.Open(packPath, "nonexistent-prompt",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.Error(t, err, "Open should fail for nonexistent prompt")
	assert.ErrorContains(t, err, "not found in pack")
}

// ---------------------------------------------------------------------------
// 7.4 — Tool execution failure
// ---------------------------------------------------------------------------

// toolCallPackJSON defines a pack with a tool, used with ToolProvider to
// simulate the provider calling a tool that returns an error.
const toolCallPackJSON = `{
	"id": "integration-test-toolerr",
	"version": "1.0.0",
	"description": "Pack for tool error tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant with tools."
		}
	},
	"tools": {
		"failing_tool": {
			"name": "failing_tool",
			"description": "A tool that always fails",
			"parameters": {
				"type": "object",
				"properties": {
					"input": {"type": "string"}
				}
			}
		}
	}
}`

func TestErrors_ToolExecutionFailure(t *testing.T) {
	// The mock ToolProvider, when it encounters a pack with tools and no
	// specific mock config for tool_calls, returns a plain text response.
	// The tool error path is tested at the SDK unit level. Here we verify
	// that a conversation with a tool handler that returns an error still
	// works end-to-end — the handler is registered but only invoked if the
	// provider actually requests a tool call. With the basic mock provider
	// (no tool call simulation), Send succeeds and the handler is never called.
	packPath := writePackFile(t, toolCallPackJSON)
	provider := mock.NewProvider("mock-test", "mock-model", false)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	// Register a tool handler that returns an error.
	conv.OnTool("failing_tool", func(args map[string]any) (any, error) {
		return nil, errors.New("tool execution failed")
	})

	// Send should succeed — mock provider returns text, not a tool call.
	resp, err := conv.Send(context.Background(), "Use the failing tool")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())
}

// ---------------------------------------------------------------------------
// 7.5 — Resume without state store
// ---------------------------------------------------------------------------

func TestErrors_ResumeWithoutStateStore(t *testing.T) {
	packPath := writePackFile(t, minimalPackJSON)
	provider := mock.NewProvider("mock-test", "mock-model", false)

	_, err := sdk.Resume("some-conversation-id", packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.Error(t, err, "Resume without state store should fail")
	assert.ErrorIs(t, err, sdk.ErrNoStateStore)
}
