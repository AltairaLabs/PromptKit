package integration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func writeRuntimeConfig(t *testing.T, spec string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.yaml")
	doc := "apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: RuntimeConfig\nmetadata:\n  name: rc\nspec:\n" + spec
	require.NoError(t, os.WriteFile(path, []byte(doc), 0o600))
	return path
}

// TestRuntimeConfigErrors_NameTheirSection opens a conversation with a runtime
// config that fails validation in the logging section and in another one.
// The caller gets a *config.ValidationError through sdk.Open in both cases,
// with Section set only for logging, and the matching message prefix.
func TestRuntimeConfigErrors_NameTheirSection(t *testing.T) {
	cases := []struct {
		name, spec, section, prefix, field string
	}{
		{"logging", "  logging:\n    defaultLevel: loud\n", "logging", "logging config validation error: ", "defaultLevel"},
		{"state_store", "  state_store:\n    type: floppy\n", "", "config validation error: ", "state_store.type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sdk.Open(writePackFile(t, minimalPackJSON), "chat",
				sdk.WithSkipSchemaValidation(),
				sdk.WithRuntimeConfig(writeRuntimeConfig(t, tc.spec)))
			require.Error(t, err)
			var verr *pkgconfig.ValidationError
			require.True(t, errors.As(err, &verr), "Open's error is not a config.ValidationError: %v", err)
			assert.Equal(t, tc.section, verr.Section)
			assert.Equal(t, tc.field, verr.Field)
			assert.True(t, strings.HasPrefix(verr.Error(), tc.prefix), "message %q", verr.Error())
		})
	}
}

// brokenWorkflowPackJSON has findings in several states.
const brokenWorkflowPackJSON = `{
	"id": "broken-workflow", "version": "1.0.0", "description": "workflow with several findings",
	"workflow": {
		"version": 1, "entry": "alpha",
		"states": {
			"zeta":  {"prompt_task": "missing_z", "on_event": {"Go": "nowhere_z", "Back": "alpha"}},
			"alpha": {"prompt_task": "missing_a", "on_event": {"Next": "zeta", "Skip": "nowhere_a"}},
			"mid":   {"prompt_task": "chat", "on_event": {"Loop": "mid", "Out": "nowhere_m"}}
		}
	},
	"prompts": {"chat": {"id": "chat", "name": "Chat", "system_template": "hi"}}
}`

// TestWorkflowFindings_SameOrderEveryRun validates a pack with several
// workflow findings many times, through the runtime pack validator and
// through OpenWorkflow. Every run reports the same findings in the same
// order, sorted by state.
func TestWorkflowFindings_SameOrderEveryRun(t *testing.T) {
	path := writePackFile(t, brokenWorkflowPackJSON)

	var first []string
	var firstOpenErr string
	for i := 0; i < 25; i++ {
		pack, err := prompt.LoadPack(path)
		require.NoError(t, err)
		res := pack.ValidateDetailed()
		var findings []string
		for _, e := range res.Errors {
			if strings.Contains(e, "nowhere") || strings.Contains(e, "missing_") || strings.Contains(e, "state") {
				findings = append(findings, e)
			}
		}
		_, openErr := sdk.OpenWorkflow(path, sdk.WithSkipSchemaValidation())
		require.Error(t, openErr, "OpenWorkflow accepted a broken workflow")
		if i == 0 {
			first, firstOpenErr = findings, openErr.Error()
			require.GreaterOrEqual(t, len(first), 3, "expected findings in several states: %v", res.Errors)
			continue
		}
		require.Equal(t, first, findings, "run %d reported workflow findings in another order", i)
		require.Equal(t, firstOpenErr, openErr.Error(), "run %d: OpenWorkflow's error changed", i)
	}
	// Sorted by state name: alpha's findings before mid's before zeta's.
	joined := strings.Join(first, "\n")
	a, m, z := strings.Index(joined, "nowhere_a"), strings.Index(joined, "nowhere_m"), strings.Index(joined, "nowhere_z")
	require.True(t, a >= 0 && m >= 0 && z >= 0, "findings: %v", first)
	assert.True(t, a < m && m < z, "findings are not in state order: %v", first)
}

// TestCapabilityMatching_ProviderConfigList selects providers by the
// capabilities list a provider config declares, as a host does.
func TestCapabilityMatching_ProviderConfigList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.provider.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: p
spec:
  id: p
  type: openai
  model: gpt-4o
  capabilities: [text, vision, tools]
`), 0o600))
	p, err := pkgconfig.LoadProvider(path)
	require.NoError(t, err)

	assert.True(t, providers.HasCapabilities(p.Capabilities, []string{"vision", "tools"}))
	assert.False(t, providers.HasCapabilities(p.Capabilities, []string{"vision", "audio"}))
	assert.Equal(t, []string{"audio"}, providers.MissingCapabilities(p.Capabilities, []string{"vision", "audio"}))
	assert.True(t, providers.HasCapabilities(p.Capabilities, nil), "nothing required is always met")
}
