package sdk

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// validToolPolicyPack is a schema-valid pack whose chat prompt carries policy
// as its tool_policy.
func validToolPolicyPack(t *testing.T, policy string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "p.pack.json")
	body := `{
      "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
      "id": "p", "name": "p", "version": "1.0.0",
      "template_engine": {"version": "v1", "syntax": "{{variable}}"},
      "prompts": {"chat": {"id": "chat", "name": "c", "version": "1.0.0",
        "system_template": "You help.", "tools": ["lookup_order"], "tool_policy": ` + policy + `}},
      "tools": {"lookup_order": {"name": "lookup_order", "description": "d",
        "parameters": {"type": "object", "properties": {"id": {"type": "string"}}}}}
    }`
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

// max_calls_per_minute is a host setting, not pack vocabulary. With schema
// validation the pack is rejected for it; with validation skipped it is
// ignored, and the load says so by name rather than dropping it silently.
func TestPackToolPolicy_UndefinedKeyIsRejectedOrWarned(t *testing.T) {
	conv, err := Open(validToolPolicyPack(t, `{"max_rounds": 5}`), "chat", WithProvider(newAlwaysCallProvider()))
	require.NoError(t, err, "the pack is valid without the key")
	_ = conv.Close()

	_, err = Open(validToolPolicyPack(t, `{"max_rounds": 5, "max_calls_per_minute": 1}`), "chat",
		WithProvider(newAlwaysCallProvider()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_calls_per_minute is not allowed")

	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })
	conv, err = Open(validToolPolicyPack(t, `{"max_rounds": 5, "max_calls_per_minute": 1, "max_cost": 2}`), "chat",
		WithProvider(newAlwaysCallProvider()), WithSkipSchemaValidation())
	require.NoError(t, err)
	_ = conv.Close()

	var warned []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, "tool_policy key is not in the PromptPack spec") {
			warned = append(warned, line)
		}
	}
	require.Len(t, warned, 2, "one warning per ignored key: %s", buf.String())
	assert.Contains(t, warned[0], `"key":"max_calls_per_minute"`)
	assert.Contains(t, warned[1], `"key":"max_cost"`)
	assert.Contains(t, warned[0], `"prompt":"chat"`)
	assert.NotContains(t, buf.String(), `"key":"max_rounds"`, "spec keys are not reported")
}
