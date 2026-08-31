package prompt_test

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"

	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/prompt/schema"
)

// TestPackToJSONValidatesAgainstEmbeddedSchema builds a Pack in Go, serializes
// it exactly as the compiler does, and validates the result against the embedded
// PromptPack schema.
//
// This is the guard that was missing. Every other check looked at one side or
// the other: the parity tests compare struct tags to the schema, and
// TestPackJSONRoundTrip unmarshals then re-marshals JSON — so a field with no
// json tag round-trips as an equally-empty value on both sides and the test
// passes. Nothing asserted that what promptkit WRITES is a valid pack.
//
// It caught three real bugs when added (fixed in the same commit). Metadata,
// ModelTestResultRef and ModelOverride carried yaml tags and no json tags, so
// encoding/json fell back to Go field names and emitted:
//
//	"tested_models":   [{"Provider": …, "Model": …}]   -> 'provider' is required
//	"model_overrides": {"gpt-4": {"SystemTemplate": …}} -> additional properties
//	"metadata":        {"Domain": …, "Language": …}     -> silently dropped
//
// The metadata case is why this needs to be an emit test rather than a stricter
// parity test: metadata is additionalProperties:true, so "Domain" validates
// happily as an unknown property while "domain" is absent. Only asserting on
// the emitted document catches it — and it is caught here by reading the value
// back out, not by validation.
//
// Populate every field you add to a pack type here. An unpopulated field is not
// covered: omitempty means it never reaches the document.
func TestPackToJSONValidatesAgainstEmbeddedSchema(t *testing.T) {
	pack := &prompt.Pack{
		ID:          "emit-pack",
		Name:        "Emit Pack",
		Version:     "1.0.0",
		Description: "Serialized by promptkit and validated against the spec",
		TemplateEngine: &prompt.TemplateEngineInfo{
			Version: "v1",
			Syntax:  "{{variable}}",
		},
		Metadata: &prompt.Metadata{
			Domain:   "finance",
			Language: "en",
			Tags:     []string{"support"},
			CostEstimate: &prompt.CostEstimate{
				MinCostUSD: packspec.Ptr(0.01),
				MaxCostUSD: packspec.Ptr(0.10),
				AvgCostUSD: packspec.Ptr(0.04),
			},
			// RFC 0013. Emitted here so the governance a pack declares is
			// proved to validate against the real schema, not just to compile.
			Governance: &packspec.Governance{
				AutonomyLevel:        "acts_with_approval",
				AccountableOwner:     "risk@example.com",
				RequiresAIDisclosure: packspec.Ptr(true),
			},
		},
		Prompts: map[string]*prompt.PackPrompt{
			"chat": {
				ID:             "chat",
				Name:           "Chat",
				Version:        "1.0.0",
				Description:    "a prompt",
				SystemTemplate: "You are helpful.",
				TestedModels: []prompt.ModelTestResultRef{{
					Provider:     "openai",
					Model:        "gpt-4",
					Date:         "2026-01-01",
					SuccessRate:  packspec.Ptr(0.98),
					AvgTokens:    packspec.Ptr(120.0),
					AvgCost:      packspec.Ptr(0.02),
					AvgLatencyMs: packspec.Ptr(850.0),
					Notes:        "nightly run",
				}},
				ModelOverrides: map[string]prompt.ModelOverride{
					"gpt-4": {
						SystemTemplate:       "You are helpful.",
						SystemTemplateSuffix: " Be brief.",
					},
				},
			},
		},
	}

	// Serialize through the production path, not json.Marshal directly.
	data, err := prompt.NewPackCompiler(nil).MarshalPack(pack)
	require.NoError(t, err)

	loader := gojsonschema.NewStringLoader(schema.GetEmbeddedSchema())
	result, err := schema.ValidateJSONAgainstLoader(data, loader)
	require.NoError(t, err)

	if !result.Valid {
		for _, e := range result.Errors {
			t.Errorf("emitted pack violates the spec: %s", e.Error())
		}
		t.Fatalf("MarshalPack produced a document that fails PromptPack validation:\n%s", data)
	}

	// Validation alone is not enough for metadata: it is
	// additionalProperties:true, so a wrong key name is accepted as an unknown
	// property rather than rejected. Read the value back to prove the key is the
	// spec's and the value survived.
	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	meta, ok := back["metadata"].(map[string]any)
	require.True(t, ok, "metadata must serialize as an object")
	require.Equal(t, "finance", meta["domain"],
		"metadata.domain must serialize under its spec key — a missing json tag makes "+
			"encoding/json emit \"Domain\", which validates as an unknown property and "+
			"silently loses the value")
}
