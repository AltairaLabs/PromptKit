//go:build e2e

package sdk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// WithJSONInput E2E Tests
//
// These verify that a structured JSON input bound via WithJSONInput reaches the
// model's rendered prompt across all real providers — the input half of the
// function-style pattern. Output-side structured JSON is covered by
// e2e_json_mode_test.go.
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_JSONInput
// =============================================================================

// echoPackJSON returns a pack whose system prompt is driven entirely by a bound
// template variable, so the model's reply proves the binding took effect.
const echoPackJSON = `{
  "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
  "id": "e2e-json-input",
  "name": "E2E JSON Input Pack",
  "version": "1.0.0",
  "description": "Pack for WithJSONInput e2e tests",
  "template_engine": {"version": "v1", "syntax": "{{variable}}"},
  "prompts": {
    "echo": {
      "id": "echo",
      "name": "Echo",
      "version": "1.0.0",
      "system_template": "You are an echo service. The secret code is {{secret_code}}. When the user asks for the code, reply with ONLY the secret code and nothing else.",
      "variables": [
        {"name": "secret_code", "type": "string", "required": true, "description": "The code to echo"}
      ]
    }
  }
}`

// newEchoConversation opens the echo pack against a real provider.
func newEchoConversation(t *testing.T, provider ProviderConfig) *Conversation {
	t.Helper()
	dir := t.TempDir()
	packPath := filepath.Join(dir, "echo.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(echoPackJSON), 0o644))

	conv, err := Open(packPath, "echo",
		WithModel(provider.DefaultModel),
		WithEventBus(CreateCostTrackingEventBus()),
	)
	require.NoError(t, err, "Failed to open echo conversation with provider %s", provider.ID)
	return conv
}

// TestE2E_JSONInput_BoundVariableReachesModel verifies that a top-level field of
// the JSON input is bound to the prompt's {{secret_code}} variable and reaches
// the model, across every JSON-capable provider.
func TestE2E_JSONInput_BoundVariableReachesModel(t *testing.T) {
	const secret = "ZEBRA-4271"

	RunForProviders(t, CapJSON, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for JSON input binding test")
		}

		conv := newEchoConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Non-empty message asks for the code; WithJSONInput binds it to {{secret_code}}.
		resp, err := conv.Send(ctx, "What is the code?",
			WithJSONInput(map[string]any{"secret_code": secret}))
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Contains(t, resp.Text(), secret,
			"provider %s should echo the bound secret_code", provider.ID)
		t.Logf("Provider %s echoed: %s", provider.ID, strings.TrimSpace(resp.Text()))
	})
}

// TestE2E_JSONInput_EmptyMessageFallbackReachesModel verifies the one-shot
// function form: an empty message with WithJSONInput sends the JSON as the user
// turn and binds {{secret_code}}, and the model still produces the value.
func TestE2E_JSONInput_EmptyMessageFallbackReachesModel(t *testing.T) {
	const secret = "MAGENTA-9930"

	RunForProviders(t, CapJSON, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for JSON input fallback test")
		}

		conv := newEchoConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "", WithJSONInput(map[string]any{"secret_code": secret}))
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Contains(t, resp.Text(), secret,
			"provider %s should echo the bound secret_code from the fallback turn", provider.ID)
		t.Logf("Provider %s echoed: %s", provider.ID, strings.TrimSpace(resp.Text()))
	})
}
