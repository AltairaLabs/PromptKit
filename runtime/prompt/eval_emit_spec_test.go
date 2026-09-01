package prompt_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt/schema"
)

// TestEveryEvalFormEmitsAValidPack covers two divergences that only showed on
// disk, both fixed by making evals.EvalDef the generated type.
//
//   - `params` lacked omitempty, so an eval without params emitted
//     "params": null. The schema requires an object, so EVERY pack containing
//     an eval emitted a document that failed validation.
//   - `threshold` used {passed, min_score, max_score} where $defs/Eval defines
//     {operator, value} with additionalProperties:false. A spec-authored
//     threshold loaded as all-nil and emitted as {}; a promptkit one emitted a
//     document the schema rejected.
//
// Neither was caught because nothing in promptkit READS a threshold — an eval
// never states a pass/fail, only an assertion or guardrail coerces one — so the
// field existed solely to be serialized, and only a round trip through the real
// schema could see it.
func TestEveryEvalFormEmitsAValidPack(t *testing.T) {
	for _, tc := range []struct {
		name  string
		eval  string
		wants string
	}{
		{
			"no params",
			`{"id":"tone","type":"llm_judge","trigger":"every_turn"}`,
			`[{"id":"tone","trigger":"every_turn","type":"llm_judge"}]`,
		},
		{
			"spec threshold vocabulary",
			`{"id":"tone","type":"llm_judge","trigger":"every_turn",` +
				`"threshold":{"operator":"gte","value":0.8}}`,
			`[{"id":"tone","threshold":{"operator":"gte","value":0.8},` +
				`"trigger":"every_turn","type":"llm_judge"}]`,
		},
		{
			"with params",
			`{"id":"tone","type":"contains","trigger":"every_turn","params":{"text":"hi"}}`,
			`[{"id":"tone","params":{"text":"hi"},"trigger":"every_turn","type":"contains"}]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `{"id":"p","name":"P","version":"1.0.0","description":"d",
			  "template_engine":{"version":"v1","syntax":"{{variable}}"},
			  "prompts":{"c":{"id":"c","name":"C","version":"1.0.0",
			    "system_template":"hi","evals":[` + tc.eval + `]}}}`

			var p prompt.Pack
			require.NoError(t, json.Unmarshal([]byte(src), &p))

			out, err := prompt.NewPackCompiler(nil).MarshalPack(&p)
			require.NoError(t, err)

			var doc map[string]any
			require.NoError(t, json.Unmarshal(out, &doc))
			emitted, err := json.Marshal(
				doc["prompts"].(map[string]any)["c"].(map[string]any)["evals"])
			require.NoError(t, err)
			require.Equal(t, tc.wants, string(emitted))

			result, err := schema.ValidateJSONAgainstLoader(out,
				gojsonschema.NewStringLoader(schema.GetEmbeddedSchema()))
			require.NoError(t, err)
			if !result.Valid {
				for _, e := range result.Errors {
					t.Errorf("emitted pack violates the spec: %s", e)
				}
			}
		})
	}
}
