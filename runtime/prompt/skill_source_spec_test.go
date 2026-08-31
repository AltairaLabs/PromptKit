package prompt_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/prompt/schema"
)

// emitSkills round-trips a pack carrying one skill source and returns both the
// emitted skills array and whether the whole document validates.
func emitSkills(t *testing.T, skill string) (string, *schema.ValidationResult) {
	t.Helper()

	src := `{"id":"p","name":"P","version":"1.0.0","description":"d",
	  "template_engine":{"version":"v1","syntax":"{{variable}}"},
	  "prompts":{"chat":{"id":"chat","name":"Chat","description":"d",
	     "version":"1.0.0","system_template":"hi"}},
	  "skills":[` + skill + `]}`

	var p prompt.Pack
	require.NoError(t, json.Unmarshal([]byte(src), &p))

	out, err := prompt.NewPackCompiler(nil).MarshalPack(&p)
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(out, &doc))
	emitted, err := json.Marshal(doc["skills"])
	require.NoError(t, err)

	loader := gojsonschema.NewStringLoader(schema.GetEmbeddedSchema())
	result, err := schema.ValidateJSONAgainstLoader(out, loader)
	require.NoError(t, err)

	return string(emitted), result
}

// TestEverySkillSourceFormEmitsAValidPack covers all three branches of
// $defs/SkillSource plus the legacy `dir` alias.
//
// `dir` is not in the spec, and $defs/SkillPathSource is additionalProperties
// false with `path` required — so a pack loaded from `dir` was written back
// failing validation three ways at once (unknown property, missing required
// property, and therefore matching no branch of the oneOf). It loaded fine and
// ran fine; only the emitted document was invalid, which is why nothing caught
// it. `dir` is now folded into `path` on load and never serializes.
func TestEverySkillSourceFormEmitsAValidPack(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    string
		wants string
	}{
		{"bare string shorthand", `"./skills"`, `[{"path":"./skills"}]`},
		{"path object", `{"path":"./skills"}`, `[{"path":"./skills"}]`},
		{"legacy dir alias", `{"dir":"./skills"}`, `[{"path":"./skills"}]`},
		{
			"inline skill",
			`{"name":"billing","description":"d","instructions":"i"}`,
			`[{"description":"d","instructions":"i","name":"billing"}]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			emitted, result := emitSkills(t, tc.in)

			require.Equal(t, tc.wants, emitted,
				"emitted skills must use the spec's vocabulary")

			if !result.Valid {
				for _, e := range result.Errors {
					t.Errorf("emitted pack violates the spec: %s", e)
				}
			}
		})
	}
}

// TestDirAliasStillResolves — folding `dir` into `path` must not break the packs
// that use it. Both spellings still reach the loader through EffectiveDir.
func TestDirAliasStillResolves(t *testing.T) {
	for _, in := range []string{`{"dir":"./skills"}`, `{"path":"./skills"}`, `"./skills"`} {
		var s prompt.SkillSourceConfig
		require.NoError(t, json.Unmarshal([]byte(in), &s))
		require.Equal(t, "./skills", s.EffectiveDir(), in)
	}
}

// TestDirDoesNotOverridePath — a pack setting both is contradictory, and the
// spec's spelling is the one that wins. Folding must not silently reverse that.
func TestDirDoesNotOverridePath(t *testing.T) {
	var s prompt.SkillSourceConfig
	require.NoError(t, json.Unmarshal([]byte(`{"dir":"./old","path":"./new"}`), &s))
	require.Equal(t, "./new", s.Path, "path is the spec's spelling and must win")
}
