package openai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func sampleTools() []openAITool {
	return []openAITool{{
		Type:     "function",
		Function: openAIToolFunction{Name: "approve_refund", Description: "approve"},
	}}
}

func newParallelToolCallsProvider(cfg map[string]any) *ToolProvider {
	return NewToolProvider(
		"test", "gpt-4o-mini", "https://api.openai.com/v1",
		providers.ProviderDefaults{MaxTokens: 64}, false, cfg, nil,
	)
}

func TestGetParallelToolCalls(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]any
		want *bool
	}{
		{"absent", map[string]any{}, nil},
		{"nil config", nil, nil},
		{"false", map[string]any{"parallel_tool_calls": false}, boolPtr(false)},
		{"true", map[string]any{"parallel_tool_calls": true}, boolPtr(true)},
		{"wrong type ignored", map[string]any{"parallel_tool_calls": "false"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getParallelToolCalls(tt.cfg)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tt.want, *got)
		})
	}
}

func boolPtr(b bool) *bool { return &b }

// Configured plus tools declared: the parameter must reach the wire, otherwise
// the whole feature is inert (the bug reported in #1736).
func TestBuildToolRequest_ParallelToolCallsSentWithTools(t *testing.T) {
	p := newParallelToolCallsProvider(map[string]any{"parallel_tool_calls": false})
	p.apiMode = APIModeCompletions

	got := p.buildToolRequest(context.Background(), providers.PredictionRequest{}, sampleTools(), "")

	v, ok := got["parallel_tool_calls"]
	require.True(t, ok, "parallel_tool_calls must be serialized when configured")
	assert.Equal(t, false, v)
}

// Configured but no tools to declare: OpenAI rejects the pair with
// "400 'parallel_tool_calls' is only allowed when 'tools' are specified".
// That combination is reachable now that an emptied tool set still routes down
// the tool-aware path (#1735), so this guard is load-bearing, not theoretical.
func TestBuildToolRequest_ParallelToolCallsOmittedWithoutTools(t *testing.T) {
	p := newParallelToolCallsProvider(map[string]any{"parallel_tool_calls": false})
	p.apiMode = APIModeCompletions

	got := p.buildToolRequest(context.Background(), providers.PredictionRequest{}, nil, "")

	_, ok := got["parallel_tool_calls"]
	assert.False(t, ok,
		"must be omitted when no tools are declared, or OpenAI 400s the request")
}

func TestBuildToolRequest_ParallelToolCallsAbsentWhenUnconfigured(t *testing.T) {
	p := newParallelToolCallsProvider(nil)
	p.apiMode = APIModeCompletions

	got := p.buildToolRequest(context.Background(), providers.PredictionRequest{}, sampleTools(), "")

	_, ok := got["parallel_tool_calls"]
	assert.False(t, ok, "unset config must not put the key on the wire at all")
}

// Responses mode shares the same additional_config but a separate builder.
// Without this the same YAML silently does nothing depending on api_mode.
func TestBuildResponsesRequest_ParallelToolCalls(t *testing.T) {
	p := newParallelToolCallsProvider(map[string]any{"parallel_tool_calls": false})

	withTools := p.buildResponsesRequest(providers.PredictionRequest{}, sampleTools(), "")
	v, ok := withTools["parallel_tool_calls"]
	require.True(t, ok, "Responses path must honor the same config key")
	assert.Equal(t, false, v)

	withoutTools := p.buildResponsesRequest(providers.PredictionRequest{}, nil, "")
	_, ok = withoutTools["parallel_tool_calls"]
	assert.False(t, ok, "same no-tools guard applies on the Responses path")
}
