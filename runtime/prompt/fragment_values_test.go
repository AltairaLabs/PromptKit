package prompt

import (
	"errors"
	"io/fs"
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

// A value holding a placeholder is not expanded by another variable, and is
// refused in a path. An empty value that leaves a path segment empty names no
// file: an optional fragment renders empty, a required one is not found.
func TestFragmentValues_NestedAndEmpty(t *testing.T) {
	repo := newMockRepository()
	repo.prompts["nested"] = fragmentConfig("nested", FragmentRef{Name: "persona", Path: "p/{{region}}.md"})
	repo.prompts["empty"] = fragmentConfig("empty", FragmentRef{Name: "persona", Path: "{{region}}/persona.md"})
	repo.prompts["required"] = fragmentConfig("required",
		FragmentRef{Name: "persona", Path: "{{region}}/persona.md", Required: true})
	reg := NewRegistryWithRepository(repo)

	for i := 0; i < 20; i++ { // map order once let the inner variable fill in
		_, err := reg.LoadTemplateDeferRequired("nested",
			map[string]string{"region": "{{x}}", "x": "../../etc/passwd"}, "")
		require.True(t, errors.Is(err, ErrUnsafeFragmentValue), "run %d: %v", i, err)
	}
	tmpl, err := reg.LoadTemplateDeferRequired("empty", map[string]string{"region": ""}, "")
	require.NoError(t, err, "an optional fragment whose path names no file renders empty")
	assert.Equal(t, "", tmpl.FragmentVars["persona"])
	_, err = reg.LoadTemplateDeferRequired("required", map[string]string{"region": ""}, "")
	require.True(t, errors.Is(err, fs.ErrNotExist), "%v", err)

	fr := NewFragmentResolverWithRepository(repo)
	assert.Equal(t, "{{x}}", fr.resolveVariables("{{region}}", map[string]string{"region": "{{x}}", "x": "y"}))
}

// A segment split by "\" is checked like one split by "/", and a filled
// segment of dots and spaces only is refused: ".." from two "." values, and
// ". " or "..." which Windows reads as a climb or the current directory.
func TestFillFragmentPath_SeparatorsAndDotSegments(t *testing.T) {
	cases := []struct {
		path string
		vars map[string]string
	}{
		{`{{a}}{{b}}\secret.yaml`, map[string]string{"a": ".", "b": "."}},
		{`{{a}}.\x.yaml`, map[string]string{"a": "."}},
		{`p/{{a}}{{b}}/x.yaml`, map[string]string{"a": ".", "b": " "}},
		{`p/{{a}}/x.yaml`, map[string]string{"a": "..."}},
	}
	for _, c := range cases {
		_, err := fillFragmentPath(FragmentRef{Name: "f", Path: c.path}, c.vars)
		assert.True(t, errors.Is(err, ErrUnsafeFragmentValue), "%s %v: %v", c.path, c.vars, err)
	}
	got, err := fillFragmentPath(FragmentRef{Name: "f", Path: `p\{{a}}.yaml`}, map[string]string{"a": "us"})
	require.NoError(t, err)
	assert.Equal(t, "p/us.yaml", got)
}

// The compiler fills candidate paths as the runtime does: a candidate the
// runtime would refuse leaves the reference, an empty path segment is a
// missing candidate, and a name-only key may hold "/".
func TestPackCompiler_CandidatesMatchTheRuntime(t *testing.T) {
	repo := &pathOnlyRepo{mockRepository: newMockRepository(), byPath: map[string]string{"p/us.md": "US"}}
	refuse := fragmentConfig("refuse", FragmentRef{Name: "f_{{a}}{{b}}", Path: "{{a}}{{b}}/x.md"})
	refuse.Spec.Variables = []VariableMetadata{
		{Name: "a", Validation: map[string]interface{}{"enum": []interface{}{"."}}},
		{Name: "b", Validation: map[string]interface{}{"enum": []interface{}{"."}}},
	}
	repo.prompts["refuse"] = refuse
	pack, err := compileWithRepo(t, repo, "refuse")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"f_{{a}}{{b}}": "{{f_{{a}}{{b}}}}"}, pack.Fragments)

	empty := fragmentConfig("empty", FragmentRef{Name: "f_{{r}}", Path: "p/{{r}}.md"})
	empty.Spec.Variables = []VariableMetadata{
		{Name: "r", Validation: map[string]interface{}{"enum": []interface{}{"us", ""}}},
	}
	repo.prompts["empty"] = empty
	pack, err = compileWithRepo(t, repo, "empty")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"f_us": "US", "f_": ""}, pack.Fragments)

	slash := fragmentConfig("slash", FragmentRef{Name: "{{k}}", Required: true})
	slash.Spec.Variables = []VariableMetadata{
		{Name: "k", Validation: map[string]interface{}{"enum": []interface{}{"support/formal"}}},
	}
	repo.fragments["support/formal"] = &Fragment{Content: "Formal."}
	repo.prompts["slash"] = slash
	pack, err = compileWithRepo(t, repo, "slash")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"support/formal": "Formal."}, pack.Fragments)
}
