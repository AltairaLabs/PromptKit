package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// A pipeline runs the model's tool calls when one of its stages does: a
// ProviderStage with a tool registry, or a DuplexProviderStage given
// SetToolExecution with one.
func TestStreamPipeline_RunsToolCalls(t *testing.T) {
	build := func(t *testing.T, stages ...Stage) *StreamPipeline {
		t.Helper()
		p, err := NewPipelineBuilder().Chain(stages...).Build()
		require.NoError(t, err)
		return p
	}
	prov := mock.NewProvider("m", "m", false)
	rt := mock.NewStreamingProvider("rt", "rt", false)
	cfg := &providers.StreamingInputConfig{}

	withRegistry := NewProviderStage(prov, tools.NewRegistry(), nil, nil)
	withoutRegistry := NewProviderStage(prov, nil, nil, nil)
	running := NewDuplexProviderStage(rt, cfg)
	running.SetToolExecution(tools.NewRegistry(), nil, DuplexToolConfig{}, nil)
	notRunning := NewDuplexProviderStage(rt, cfg)
	cleared := NewDuplexProviderStage(rt, cfg)
	cleared.SetToolExecution(nil, nil, DuplexToolConfig{}, nil)

	assert.True(t, build(t, withRegistry).RunsToolCalls(), "ProviderStage with a registry")
	assert.False(t, build(t, withoutRegistry).RunsToolCalls(), "ProviderStage without one")
	assert.True(t, build(t, running).RunsToolCalls(), "DuplexProviderStage given SetToolExecution")
	assert.False(t, build(t, notRunning).RunsToolCalls(), "DuplexProviderStage without it")
	assert.False(t, build(t, cleared).RunsToolCalls(), "DuplexProviderStage given no registry")
	assert.True(t, build(t, NewPassthroughStage("first"), running).RunsToolCalls(), "a later stage")
}
