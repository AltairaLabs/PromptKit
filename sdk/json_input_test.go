package sdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyJSONInput applies WithJSONInput(v) to a fresh sendConfig and returns it.
func applyJSONInput(t *testing.T, v any) *sendConfig {
	t.Helper()
	cfg := &sendConfig{}
	require.NoError(t, WithJSONInput(v)(cfg))
	return cfg
}

func TestWithJSONInput_TopLevelStringPassesThrough(t *testing.T) {
	cfg := applyJSONInput(t, map[string]any{"topic": "batteries"})
	assert.Equal(t, "batteries", cfg.jsonInputVars["topic"])
}

func TestWithJSONInput_NonStringFieldsAreJSONEncoded(t *testing.T) {
	cfg := applyJSONInput(t, map[string]any{
		"count":   3,
		"enabled": true,
		"nested":  map[string]any{"a": 1},
		"list":    []any{1, 2},
	})
	assert.Equal(t, "3", cfg.jsonInputVars["count"])
	assert.Equal(t, "true", cfg.jsonInputVars["enabled"])
	assert.JSONEq(t, `{"a":1}`, cfg.jsonInputVars["nested"])
	assert.JSONEq(t, `[1,2]`, cfg.jsonInputVars["list"])
}

func TestWithJSONInput_WholeObjectBoundToInput(t *testing.T) {
	cfg := applyJSONInput(t, map[string]any{"topic": "x", "n": 1})
	assert.JSONEq(t, `{"topic":"x","n":1}`, cfg.jsonInputVars["input"])
}

func TestWithJSONInput_RealInputFieldShadowsWholeObject(t *testing.T) {
	cfg := applyJSONInput(t, map[string]any{"input": "user-data", "topic": "x"})
	assert.Equal(t, "user-data", cfg.jsonInputVars["input"])
}

func TestWithJSONInput_NonObjectBindsOnlyInput(t *testing.T) {
	cfg := applyJSONInput(t, []any{"a", "b"})
	assert.JSONEq(t, `["a","b"]`, cfg.jsonInputVars["input"])
	assert.Len(t, cfg.jsonInputVars, 1)
}

func TestWithJSONInput_StructInput(t *testing.T) {
	type req struct {
		Topic string `json:"topic"`
		Count int    `json:"count"`
	}
	cfg := applyJSONInput(t, req{Topic: "solar", Count: 5})
	assert.Equal(t, "solar", cfg.jsonInputVars["topic"])
	assert.Equal(t, "5", cfg.jsonInputVars["count"])
}

func TestWithJSONInput_RawMessageInput(t *testing.T) {
	cfg := applyJSONInput(t, json.RawMessage(`{"topic":"wind"}`))
	assert.Equal(t, "wind", cfg.jsonInputVars["topic"])
}

func TestWithJSONInput_RawIsSetForFallbackTurn(t *testing.T) {
	cfg := applyJSONInput(t, map[string]any{"topic": "x"})
	assert.JSONEq(t, `{"topic":"x"}`, string(cfg.jsonInputRaw))
}

func TestWithJSONInput_UnmarshalableReturnsError(t *testing.T) {
	cfg := &sendConfig{}
	err := WithJSONInput(make(chan int))(cfg)
	assert.Error(t, err)
}
