package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fragment chosen by variables that declare their values compiles to every
// candidate; one whose variable declares none stays a {{name}} reference.
func TestPackCompiler_DynamicFragments(t *testing.T) {
	repo := newMockRepository()
	for _, name := range []string{"tone_us_1", "tone_us_2", "tone_uk_1", "tone_uk_2"} {
		repo.fragments[name] = &Fragment{Content: "content of " + name}
	}
	cfg := fragmentConfig("chat",
		FragmentRef{Name: "tone_{{region}}_{{level}}", Required: true},
		FragmentRef{Name: "persona_{{mood}}", Required: true},
	)
	cfg.Spec.Variables = []VariableMetadata{
		{Name: "region", Validation: map[string]interface{}{"enum": []interface{}{"us", "uk"}}},
		{Name: "level", Validation: map[string]interface{}{"enum": []interface{}{1, 2}}},
		{Name: "mood"},
	}
	repo.prompts["chat"] = cfg

	pack, err := compileWithRepo(t, repo, "chat")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"tone_us_1": "content of tone_us_1", "tone_us_2": "content of tone_us_2",
		"tone_uk_1": "content of tone_uk_1", "tone_uk_2": "content of tone_uk_2",
		"persona_{{mood}}": "{{persona_{{mood}}}}",
	}, pack.Fragments)
}

// A required candidate that cannot load fails the compile, naming it.
func TestPackCompiler_DynamicFragmentMissingCandidateIsAnError(t *testing.T) {
	repo := newMockRepository()
	repo.fragments["tone_us"] = &Fragment{Content: "us"}
	cfg := fragmentConfig("chat", FragmentRef{Name: "tone_{{region}}", Required: true})
	cfg.Spec.Variables = []VariableMetadata{
		{Name: "region", Validation: map[string]interface{}{"enum": []interface{}{"us", "uk"}}},
	}
	repo.prompts["chat"] = cfg

	_, err := compileWithRepo(t, repo, "chat")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"tone_uk"`)
}
