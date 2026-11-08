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
