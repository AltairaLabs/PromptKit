package sdk

import (
	"runtime"
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
	type opener func(path, prompt string, opts ...Option) (*Conversation, error)
	fromTemplate := func(duplex bool) opener {
		return func(path, prompt string, opts ...Option) (*Conversation, error) {
			tmpl, err := LoadTemplate(path)
			if err != nil {
				return nil, err
			}
			if duplex {
				return tmpl.OpenDuplex(prompt, opts...)
			}
			return tmpl.Open(prompt, opts...)
		}
	}
	pairs := []struct {
		name         string
		open, opened opener
	}{
		{"Open", Open, fromTemplate(false)},
		{"OpenDuplex", OpenDuplex, fromTemplate(true)},
	}
	for _, tc := range cases {
		for _, pair := range pairs {
			t.Run(tc.name+"/"+pair.name, func(t *testing.T) {
				packPath := createTestPackFile(t, tc.pack)

				_, openErr := pair.open(packPath, "chat", WithProvider(newRefProvider("agent")))
				require.Error(t, openErr)
				assert.Contains(t, openErr.Error(), tc.wantMsg)

				_, tmplErr := pair.opened(packPath, "chat", WithProvider(newRefProvider("agent")))
				require.Error(t, tmplErr, "the template path must refuse the same pack")
				assert.Equal(t, openErr.Error(), tmplErr.Error())
			})
		}
	}
}

// With more than one fault, both paths report the same one first: validator
// conversion before the provider gates.
func TestLoadGates_SameFirstFaultThroughOpenAndTemplate(t *testing.T) {
	packPath := createTestPackFile(t, loadGatePack(`{"key": "grader", "role": "llm", "required": true}`,
		`{"type": "toxicity", "enabled": true, "params": {"provider": "grader"}},
		 {"type": "no_such_check", "enabled": true}`))

	_, openErr := Open(packPath, "chat", WithProvider(newRefProvider("agent")))
	require.Error(t, openErr)
	tmpl, err := LoadTemplate(packPath)
	require.NoError(t, err)
	_, tmplErr := tmpl.Open("chat", WithProvider(newRefProvider("agent")))
	require.Error(t, tmplErr)
	assert.Equal(t, openErr.Error(), tmplErr.Error())
}

// A refused Open leaves nothing running: the gates run before the
// conversation, and its pending store, exist.
func TestLoadGates_RefusedOpenLeaksNoGoroutines(t *testing.T) {
	packPath := createTestPackFile(t, loadGatePack(`{"key": "embedder", "role": "embedding", "required": true}`, ""))
	tmpl, err := LoadTemplate(packPath)
	require.NoError(t, err)

	before := runtime.NumGoroutine()
	const attempts = 50
	for range attempts {
		_, err := tmpl.Open("chat", WithProvider(newRefProvider("agent")))
		require.Error(t, err)
		_, err = Open(packPath, "chat", WithProvider(newRefProvider("agent")))
		require.Error(t, err)
	}
	assert.Less(t, runtime.NumGoroutine()-before, attempts, "a goroutine per refused Open leaked")
}
