package stage

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A TemplateStage or PromptAssemblyStage without a TurnState does nothing: the
// system prompt is never loaded or rendered and the provider gets an empty one.
// Build must refuse such a pipeline rather than let it run.
func TestBuild_RejectsPromptStagesWithoutTurnState(t *testing.T) {
	cases := []struct {
		name  string
		stage Stage
	}{
		{"template stage", NewTemplateStage()},
		{"template stage with emitter", NewTemplateStageWithEmitter(nil)},
		{"template stage with nil turn state", NewTemplateStageWithTurnState(nil, nil)},
		{"prompt assembly stage", NewPromptAssemblyStage(nil, "chat", nil)},
		{"prompt assembly stage with nil turn state", NewPromptAssemblyStageWithTurnState(nil, "chat", nil, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPipelineBuilder().Chain(tc.stage).Build()
			require.Error(t, err)
			assert.Nil(t, p)
			assert.True(t, errors.Is(err, ErrTurnStateRequired), "got %v", err)
			assert.Contains(t, err.Error(), tc.stage.Name())
		})
	}
}

// The supported wiring builds, and the assembled prompt is rendered onto the
// shared TurnState when it runs.
func TestBuild_AcceptsPromptStagesWithTurnState(t *testing.T) {
	ts := NewTurnState()
	p, err := NewPipelineBuilder().Chain(
		NewPromptAssemblyStageWithTurnState(nil, "chat", nil, ts),
		NewTemplateStageWithTurnState(nil, ts),
	).Build()
	require.NoError(t, err)

	_, err = p.ExecuteSync(context.Background(), userElement("Hello!"))
	require.NoError(t, err)
	assert.Equal(t, "You are a helpful AI assistant.", ts.SystemPrompt)
}
