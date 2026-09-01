package prompt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt/schema"
)

// RFC 0013's tool half shipped in PromptPack v1.6.0 and could be CARRIED in a
// compiled pack but not WRITTEN by an author: nothing on the authoring path had
// a field for it. A declaration nobody can declare is the same as no
// declaration, so these cover the route end to end.

// TestAuthoredActionScopeReachesTheEmittedPack walks the whole path a tool takes
// — authored YAML, parsed, compiled, emitted — and validates the result against
// the real schema rather than just reading the struct back.
func TestAuthoredActionScopeReachesTheEmittedPack(t *testing.T) {
	toolYAML := []byte(`{
	  "apiVersion": "promptkit.io/v1alpha1",
	  "kind": "Tool",
	  "spec": {
	    "name": "issue_refund",
	    "description": "Issues a refund against an order.",
	    "input_schema": {"type":"object","properties":{"order_id":{"type":"string"}}},
	    "action_scope": {
	      "effect": "external",
	      "reversibility": "irreversible",
	      "data_classes": ["pii", "financial"]
	    },
	    "extensions": {"acme:blast_radius": "high"}
	  }
	}`)

	tool, err := parseToolData(toolYAML)
	require.NoError(t, err)
	require.NotNil(t, tool)

	require.NotNil(t, tool.ActionScope, "action_scope must survive parsing")
	require.Equal(t, "external", tool.ActionScope.Effect)
	require.Equal(t, "irreversible", tool.ActionScope.Reversibility)
	require.Equal(t, []string{"pii", "financial"}, tool.ActionScope.DataClasses)
	require.Equal(t, "high", tool.Extensions["acme:blast_radius"])

	// And out the other side, into a document the spec accepts. $defs/Tool is
	// additionalProperties:false, so an emitted tool carrying a property the
	// schema does not define fails here rather than silently shipping.
	pack := &Pack{Pack: packspec.Pack{
		ID: "p", Name: "P", Version: "1.0.0", Description: "d",
		TemplateEngine: &TemplateEngineInfo{Version: "v1", Syntax: "{{variable}}"},
		Prompts: map[string]*PackPrompt{
			"chat": {ID: "chat", Name: "Chat", Version: "1.0.0", SystemTemplate: "hi"},
		},
		Tools: map[string]*PackTool{"issue_refund": tool},
	}}

	out, err := NewPackCompiler(nil).MarshalPack(pack)
	require.NoError(t, err)

	result, err := schema.ValidateJSONAgainstLoader(out,
		gojsonschema.NewStringLoader(schema.GetEmbeddedSchema()))
	require.NoError(t, err)
	if !result.Valid {
		for _, e := range result.Errors {
			t.Errorf("emitted pack violates the spec: %s", e)
		}
	}

	var doc map[string]any
	require.NoError(t, json.Unmarshal(out, &doc))
	emitted, err := json.Marshal(
		doc["tools"].(map[string]any)["issue_refund"].(map[string]any)["action_scope"])
	require.NoError(t, err)
	require.JSONEq(t,
		`{"effect":"external","reversibility":"irreversible","data_classes":["pii","financial"]}`,
		string(emitted))
}

// TestToolWithoutActionScopeEmitsNothing — absence must stay absence. An empty
// `"action_scope": {}` would read as "declared, with no consequence", which is a
// different and worse claim than saying nothing.
func TestToolWithoutActionScopeEmitsNothing(t *testing.T) {
	tool, err := parseToolData([]byte(`{
	  "apiVersion": "promptkit.io/v1alpha1",
	  "kind": "Tool",
	  "spec": {"name": "lookup", "description": "Reads an order."}
	}`))
	require.NoError(t, err)
	require.Nil(t, tool.ActionScope)

	out, err := json.Marshal(tool)
	require.NoError(t, err)
	require.NotContains(t, string(out), "action_scope")
}

// TestAuthoredGovernanceReachesTheEmittedPack — metadata.governance had no
// authoring route: it could be carried in a compiled pack and never written.
// WithMetadata is that route, and this walks it to a validated document rather
// than reading the struct back.
func TestAuthoredGovernanceReachesTheEmittedPack(t *testing.T) {
	var o compileOptions
	WithMetadata(&Metadata{
		Domain: "billing",
		Governance: &packspec.Governance{
			IntendedPurpose:      "Triage inbound billing questions.",
			AutonomyLevel:        "acts_with_approval",
			AccountableOwner:     "billing-platform-team",
			RequiresAIDisclosure: packspec.Ptr(true),
			ApprovedEnvironments: []string{"staging", "production"},
		},
	})(&o)
	require.NotNil(t, o.metadata)

	pack := &Pack{Pack: packspec.Pack{
		ID: "p", Name: "P", Version: "1.0.0", Description: "d",
		TemplateEngine: &TemplateEngineInfo{Version: "v1", Syntax: "{{variable}}"},
		Prompts: map[string]*PackPrompt{
			"chat": {ID: "chat", Name: "Chat", Version: "1.0.0", SystemTemplate: "hi"},
		},
		Metadata: o.metadata,
	}}

	out, err := NewPackCompiler(nil).MarshalPack(pack)
	require.NoError(t, err)

	result, err := schema.ValidateJSONAgainstLoader(out,
		gojsonschema.NewStringLoader(schema.GetEmbeddedSchema()))
	require.NoError(t, err)
	if !result.Valid {
		for _, e := range result.Errors {
			t.Errorf("emitted pack violates the spec: %s", e)
		}
	}

	// metadata is additionalProperties:true, so validation alone would accept a
	// misspelled key. Read the values back to prove they are the spec's.
	var doc map[string]any
	require.NoError(t, json.Unmarshal(out, &doc))
	gov := doc["metadata"].(map[string]any)["governance"].(map[string]any)
	require.Equal(t, "acts_with_approval", gov["autonomy_level"])
	require.Equal(t, "billing-platform-team", gov["accountable_owner"])
	require.Equal(t, true, gov["requires_ai_disclosure"])
	require.Equal(t, []any{"staging", "production"}, gov["approved_environments"])
}
