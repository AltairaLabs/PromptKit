package prompt_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt/schema"
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
		// The shorthand round-trips VERBATIM rather than being expanded into
		// a path object: the generated type keeps the scalar form, so a pack
		// comes back as its author wrote it.
		{"bare string shorthand", `"./skills"`, `["./skills"]`},
		{"path object", `{"path":"./skills"}`, `[{"path":"./skills"}]`},

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

// TestSkillPathResolvesEverySpecForm — the generated type keeps the
// bare-string shorthand in Shorthand rather than Path, so a reader that wants
// "where does this skill live" has to consult both. SkillPath is that reader.
func TestSkillPathResolvesEverySpecForm(t *testing.T) {
	for _, in := range []string{`{"path":"./skills"}`, `"./skills"`} {
		var s prompt.SkillSourceConfig
		require.NoError(t, json.Unmarshal([]byte(in), &s))
		require.Equal(t, "./skills", prompt.SkillPath(&s), in)
	}
	require.Empty(t, prompt.SkillPath(nil))
}

// TestDirAliasIsGone replaces TestDirAliasStillResolves.
//
// `dir` was a promptkit-only alias for `path`. $defs/SkillPathSource is
// additionalProperties:false and requires `path`, so a pack authored with `dir`
// was written back failing validation three ways at once, and no shipped pack
// used it. It is not accepted any more, and this pins that: a pack using `dir`
// resolves to no path rather than silently half-working.
func TestDirAliasIsGone(t *testing.T) {
	var s prompt.SkillSourceConfig
	require.NoError(t, json.Unmarshal([]byte(`{"dir":"./skills"}`), &s))
	require.Empty(t, prompt.SkillPath(&s),
		"`dir` is not a spec property and must not resolve to a path")

	// The spec spelling still works, so the replacement is a rename not a loss.
	require.NoError(t, json.Unmarshal([]byte(`{"path":"./skills"}`), &s))
	require.Equal(t, "./skills", prompt.SkillPath(&s))
}

// TestValidatorEnabledDefaultsToTrue pins a bug that adopting the generated
// type fixed by accident, which is exactly the kind that comes back.
//
// $defs/Validator.enabled carries "default": true. The hand-written struct typed
// it as a plain bool, so a compiled pack that omitted the key unmarshalled to
// false — and sdk/sdk.go took its address, handing guardrails an explicit
// "disabled". A validator the author never switched off was silently inactive.
//
// *bool distinguishes absent from false, and absent means enabled.
func TestValidatorEnabledDefaultsToTrue(t *testing.T) {
	var omitted prompt.Validator
	require.NoError(t, json.Unmarshal([]byte(`{"type":"max_length"}`), &omitted))
	require.Nil(t, omitted.Enabled, "absent must stay absent, not become false")

	var explicit prompt.Validator
	require.NoError(t, json.Unmarshal([]byte(`{"type":"max_length","enabled":false}`), &explicit))
	require.NotNil(t, explicit.Enabled)
	require.False(t, *explicit.Enabled,
		"an explicit false must survive, or disabling a validator would stop working")
}
