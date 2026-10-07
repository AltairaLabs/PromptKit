package prompt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt/schema"
)

// packIDPatternFromSchema reads the pack id pattern from the embedded
// PromptPack schema, so a test checks against the spec rather than a copy.
func packIDPatternFromSchema(t *testing.T) string {
	t.Helper()
	var s struct {
		Properties struct {
			ID struct {
				Pattern string `json:"pattern"`
			} `json:"id"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal([]byte(schema.GetEmbeddedSchema()), &s))
	require.NotEmpty(t, s.Properties.ID.Pattern, "embedded schema has no pack id pattern")
	return s.Properties.ID.Pattern
}
