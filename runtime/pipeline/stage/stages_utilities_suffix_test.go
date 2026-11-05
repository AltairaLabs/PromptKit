package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

func renderWithSuffix(t *testing.T, suffix func() string) string {
	t.Helper()
	ts := &TurnState{Template: &prompt.Template{RawTemplate: "base {{name}}"}, Variables: map[string]string{"name": "x"}}
	s := NewTemplateStageWithTurnState(nil, ts)
	if suffix != nil {
		s.WithSystemPromptSuffix(suffix)
	}
	require.NoError(t, s.renderTurnPrompt())
	return ts.SystemPrompt
}

func TestTemplateStage_SystemPromptSuffix(t *testing.T) {
	assert.Equal(t, "base x\n\nextra", renderWithSuffix(t, func() string { return "extra" }))
	assert.Equal(t, "base x", renderWithSuffix(t, func() string { return "" }), "empty suffix appends nothing")
	assert.Equal(t, "base x", renderWithSuffix(t, nil))
}
