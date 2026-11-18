package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// The Responses API takes a forced function as {"type":"function","name":...}
// and only the modes "none", "auto" and "required" as strings. A function name
// sent as a bare string 400s the request ("Invalid value: 'lookup_order'.
// Supported values are: 'none', 'auto', and 'required'.").
func TestResponses_ToolChoiceOnTheWire(t *testing.T) {
	descs := []*providers.ToolDescriptor{desc("lookup_order", `{"type":"object","properties":{}}`)}
	cases := []struct {
		choice string
		want   string // "" means tool_choice is omitted
	}{
		{"lookup_order", `{"type":"function","name":"lookup_order"}`},
		{"required", `"required"`},
		{"none", `"none"`},
		{"auto", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run("choice="+tc.choice, func(t *testing.T) {
			body := sentRequest(t, openAISpec("responses"), descs, tc.choice)
			got, present := body["tool_choice"]
			if tc.want == "" {
				assert.False(t, present, "tool_choice must be omitted, got %s", got)
				return
			}
			assert.JSONEq(t, tc.want, string(got))
		})
	}
}
