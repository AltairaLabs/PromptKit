package mock

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests cover the config coercion helpers and the NewToolProvider
// wiring branches that translate YAML additionalConfig (where integers arrive
// as float64 and booleans may arrive as the string "true") into the
// StreamingProvider's simulation knobs. This logic lived behind the
// _interactive.go coverage exemption yet decides which duplex failure-mode
// simulation a scenario actually runs.

func TestGetIntFromConfig(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		key    string
		want   int
	}{
		{"int value", map[string]interface{}{"k": 5}, "k", 5},
		{"float64 value (YAML)", map[string]interface{}{"k": float64(3)}, "k", 3},
		{"int64 value", map[string]interface{}{"k": int64(7)}, "k", 7},
		{"missing key", map[string]interface{}{}, "k", 0},
		{"wrong type string", map[string]interface{}{"k": "nope"}, "k", 0},
		{"wrong type bool", map[string]interface{}{"k": true}, "k", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, getIntFromConfig(tc.config, tc.key))
		})
	}
}

func TestGetBoolFromConfig(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		key    string
		want   bool
	}{
		{"bool true", map[string]interface{}{"k": true}, "k", true},
		{"bool false", map[string]interface{}{"k": false}, "k", false},
		{"string true", map[string]interface{}{"k": "true"}, "k", true},
		{"string false", map[string]interface{}{"k": "false"}, "k", false},
		{"string other", map[string]interface{}{"k": "yes"}, "k", false},
		{"missing key", map[string]interface{}{}, "k", false},
		{"wrong type int", map[string]interface{}{"k": 1}, "k", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, getBoolFromConfig(tc.config, tc.key))
		})
	}
}

func TestNewToolProvider_EmptyConfig(t *testing.T) {
	// Non-nil but minimal config: no mock_config, no auto_respond.
	provider := NewToolProvider("id", "model", false, map[string]interface{}{})
	require.NotNil(t, provider)
	assert.False(t, provider.autoRespond)
	assert.Nil(t, provider.repo)
}

func TestNewToolProvider_AutoRespondBool(t *testing.T) {
	provider := NewToolProvider("id", "model", false, map[string]interface{}{
		"auto_respond":  true,
		"response_text": "custom text",
	})
	require.NotNil(t, provider)
	assert.True(t, provider.autoRespond)
	assert.Equal(t, "custom text", provider.responseText)
}

func TestNewToolProvider_AutoRespondStringDefaultsText(t *testing.T) {
	// auto_respond as the YAML string "true", no response_text -> default text.
	provider := NewToolProvider("id", "model", false, map[string]interface{}{
		"auto_respond": "true",
	})
	require.NotNil(t, provider)
	assert.True(t, provider.autoRespond)
	assert.Equal(t, DefaultMockStreamingResponse, provider.responseText)
}

func TestNewToolProvider_InterruptAndCloseSimulation(t *testing.T) {
	// YAML delivers integers as float64; close_no_response as a real bool.
	provider := NewToolProvider("id", "model", false, map[string]interface{}{
		"interrupt_on_turn": float64(2),
		"close_after_turns": float64(3),
		"close_no_response": true,
	})
	require.NotNil(t, provider)
	assert.Equal(t, 2, provider.interruptOnTurn)
	assert.Equal(t, 3, provider.closeAfterTurns)
	assert.True(t, provider.closeNoResponse)
}

func TestNewToolProvider_InvalidMockConfigFallsBack(t *testing.T) {
	// A bad mock_config path must not fail construction — it falls back to a
	// repository-less StreamingProvider.
	provider := NewToolProvider("id", "model", false, map[string]interface{}{
		"mock_config": "/definitely/not/here.yaml",
	})
	require.NotNil(t, provider)
	assert.Nil(t, provider.repo, "failed config load must leave no scripted repo wired")
}

func TestNewToolProvider_ValidMockConfigWiresScenario(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mock-responses.yaml")
	yamlBody := []byte(`
defaultResponse: "default"
scenarios:
  duplex-scn:
    turns:
      1: "hello from turn one"
`)
	require.NoError(t, os.WriteFile(cfgPath, yamlBody, 0o600))

	provider := NewToolProvider("id", "model", false, map[string]interface{}{
		"mock_config":     cfgPath,
		"auto_respond":    true,
		"duplex_scenario": "duplex-scn",
	})
	require.NotNil(t, provider)
	assert.True(t, provider.autoRespond)
	require.NotNil(t, provider.repo, "valid mock_config + auto_respond must wire the scripted repo")
	assert.Equal(t, "duplex-scn", provider.defaultScenarioID)
	assert.Equal(t, dir, provider.fixtureBaseDir)
}
