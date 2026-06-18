package agentkb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

func TestRenderExample_Builtin(t *testing.T) {
	out, err := RenderExample(templates.NewLoader(""), "quick-start")
	require.NoError(t, err)
	assert.Contains(t, out, "# quick-start")
	assert.Contains(t, out, "config.arena.yaml")
}

func TestRenderExample_Unknown(t *testing.T) {
	_, err := RenderExample(templates.NewLoader(""), "does-not-exist")
	require.Error(t, err)
}
