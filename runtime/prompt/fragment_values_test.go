package prompt

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFragmentValues_UnsafeValuesAreRejected(t *testing.T) {
	repo := newMockRepository()
	repo.fragments["persona_us"] = &Fragment{Content: "US"}
	cfg := fragmentConfig("chat", FragmentRef{Name: "persona_{{region}}", Path: "p/{{region}}.md"})
	repo.prompts["chat"] = cfg
	reg := NewRegistryWithRepository(repo)

	for _, v := range []string{"../x", "a/b", `a\b`, "..", "/etc/passwd"} {
		_, err := reg.LoadTemplateDeferRequired("chat", map[string]string{"region": v}, "")
		assert.True(t, errors.Is(err, ErrUnsafeFragmentValue), "value %q: %v", v, err)
	}
	// A variable no fragment uses is not a path.
	_, err := reg.LoadTemplateDeferRequired("chat", map[string]string{"region": "us", "note": "a/b"}, "")
	require.NoError(t, err)
}

func TestEnumValues_AreEnforced(t *testing.T) {
	repo := newMockRepository()
	cfg := fragmentConfig("chat")
	cfg.Spec.SystemTemplate = "Tier {{tier}}"
	cfg.Spec.Variables = []VariableMetadata{
		{Name: "tier", Validation: map[string]interface{}{"enum": []interface{}{"gold", "silver"}}},
		{Name: "free"},
	}
	repo.prompts["chat"] = cfg
	reg := NewRegistryWithRepository(repo)

	_, err := reg.LoadTemplateDeferRequired("chat", map[string]string{"tier": "gold", "free": "x"}, "")
	require.NoError(t, err)
	_, err = reg.LoadTemplateDeferRequired("chat", map[string]string{}, "")
	require.NoError(t, err, "an unset variable is not checked")
	_, err = reg.LoadTemplate("chat", map[string]string{"tier": "bronze"}, "")
	require.True(t, errors.Is(err, ErrVariableNotInEnum), "%v", err)
	assert.Contains(t, err.Error(), "gold, silver")
}

// A value holding a placeholder is not expanded by another variable, and an
// empty value cannot make a relative path absolute.
func TestFragmentValues_NestedAndEmpty(t *testing.T) {
	repo := newMockRepository()
	repo.prompts["nested"] = fragmentConfig("nested", FragmentRef{Name: "{{region}}"})
	repo.prompts["empty"] = fragmentConfig("empty", FragmentRef{Name: "persona", Path: "{{region}}/persona.md"})
	repo.prompts["named"] = fragmentConfig("named", FragmentRef{Name: "persona_{{variant}}"})
	reg := NewRegistryWithRepository(repo)

	for i := 0; i < 20; i++ { // map order once let the inner variable fill in
		_, err := reg.LoadTemplateDeferRequired("nested",
			map[string]string{"region": "{{x}}", "x": "../../etc/passwd"}, "")
		require.True(t, errors.Is(err, ErrUnsafeFragmentValue), "run %d: %v", i, err)
	}
	_, err := reg.LoadTemplateDeferRequired("empty", map[string]string{"region": ""}, "")
	require.True(t, errors.Is(err, ErrUnsafeFragmentValue), "%v", err)
	_, err = reg.LoadTemplateDeferRequired("named", map[string]string{"variant": ""}, "")
	require.NoError(t, err, "an empty value is a name's to take")

	fr := NewFragmentResolverWithRepository(repo)
	assert.Equal(t, "{{x}}", fr.resolveVariables("{{region}}", map[string]string{"region": "{{x}}", "x": "y"}))
}
