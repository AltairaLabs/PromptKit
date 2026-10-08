package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sdk.Open and PackTemplate.Open run the same load-time provider gates, so the
// same pack and options fail the same way through either (#2202). Before,
// templates skipped the check-provider and requirements gates and failed at
// the first request instead.

func loadGatePack(requires, validators string) string {
	return `{
	"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
	"id": "load-gates", "name": "load-gates", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"requires": {"providers": [` + requires + `]},
	"prompts": {"chat": {"id": "chat", "name": "chat", "version": "1.0.0",
		"system_template": "chat", "validators": [` + validators + `]}}
}`
}

func TestLoadGates_SameThroughOpenAndTemplate(t *testing.T) {
	cases := []struct {
		name    string
		pack    string
		wantMsg string
	}{
		{
			name: "a check names a key the pack does not declare",
			pack: loadGatePack(`{"key": "grader", "role": "llm", "required": true}`,
				`{"type": "toxicity", "enabled": true, "params": {"provider": "graderr"}}`),
			wantMsg: `check "toxicity" names provider "graderr", which the pack does not declare in requires`,
		},
		{
			name: "a check's key is declared and unbound",
			pack: loadGatePack(`{"key": "grader", "role": "llm", "required": true}`,
				`{"type": "toxicity", "enabled": true, "params": {"provider": "grader"}}`),
			wantMsg: `check "toxicity": provider "grader"`,
		},
		{
			name:    "a required provider is unbound",
			pack:    loadGatePack(`{"key": "embedder", "role": "embedding", "required": true}`, ""),
			wantMsg: "pack requires providers that are not configured",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			packPath := createTestPackFile(t, tc.pack)

			_, openErr := Open(packPath, "chat", WithProvider(newRefProvider("agent")))
			require.Error(t, openErr)
			assert.Contains(t, openErr.Error(), tc.wantMsg)

			tmpl, err := LoadTemplate(packPath)
			require.NoError(t, err)
			_, tmplErr := tmpl.Open("chat", WithProvider(newRefProvider("agent")))
			require.Error(t, tmplErr, "the template path must refuse the same pack")
			assert.Equal(t, openErr.Error(), tmplErr.Error())
		})
	}
}
