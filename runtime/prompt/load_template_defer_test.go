package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LoadTemplate checks required variables; LoadTemplateDeferRequired leaves
// that to the caller and still reports every other load failure.
func TestLoadTemplateDeferRequired(t *testing.T) {
	repo := newMockRepository()
	repo.prompts["chat"] = &Config{Spec: Spec{
		TaskType: "chat", SystemTemplate: "Topic: {{topic}}",
		Variables: []VariableMetadata{{Name: "topic", Required: true}},
	}}
	reg := NewRegistryWithRepository(repo)

	_, err := reg.LoadTemplate("chat", nil, "")
	require.Error(t, err, "LoadTemplate keeps its required-variable check")

	tmpl, err := reg.LoadTemplateDeferRequired("chat", nil, "")
	require.NoError(t, err)
	assert.Equal(t, "Topic: {{topic}}", tmpl.RawTemplate)
	assert.Equal(t, []string{"topic"}, tmpl.RequiredVars)

	_, err = reg.LoadTemplateDeferRequired("missing", nil, "")
	require.Error(t, err, "a prompt that cannot be loaded is still an error")
}
