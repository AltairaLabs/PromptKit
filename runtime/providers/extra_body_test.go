package providers

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

func captureExtraBodyLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	t.Cleanup(func() { logger.SetOutput(nil) })
	return &buf
}

func TestExtraBody(t *testing.T) {
	t.Run("absent is nil and silent", func(t *testing.T) {
		logs := captureExtraBodyLogs(t)
		assert.Nil(t, ExtraBody("p", nil))
		assert.Nil(t, ExtraBody("p", map[string]any{"keep_alive": "5m"}))
		assert.Empty(t, logs.String())
	})
	t.Run("a map is returned as is", func(t *testing.T) {
		extra := ExtraBody("p", map[string]any{
			ExtraBodyConfigKey: map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}},
		})
		assert.Equal(t, map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}}, extra)
	})
	t.Run("a non-map is ignored with a warning naming the provider", func(t *testing.T) {
		logs := captureExtraBodyLogs(t)
		assert.Nil(t, ExtraBody("vllm-qwen", map[string]any{ExtraBodyConfigKey: "enable_thinking=false"}))
		assert.Contains(t, logs.String(), "extra_body")
		assert.Contains(t, logs.String(), "vllm-qwen")
	})
	t.Run("a map JSON cannot encode is ignored with a warning", func(t *testing.T) {
		logs := captureExtraBodyLogs(t)
		assert.Nil(t, ExtraBody("p", map[string]any{ExtraBodyConfigKey: map[string]any{"f": func() {}}}))
		assert.Contains(t, logs.String(), "cannot be encoded as JSON")
	})
}

func TestApplyExtraBody_ProviderFieldsWin(t *testing.T) {
	logs := captureExtraBodyLogs(t)
	body := map[string]any{"model": "gpt-4o-mini", "messages": []string{"hi"}}

	ApplyExtraBody("apply-collision", body, map[string]any{"model": "other", "top_k": 20})

	assert.Equal(t, "gpt-4o-mini", body["model"])
	assert.Equal(t, 20, body["top_k"])
	assert.Contains(t, logs.String(), "apply-collision")
	assert.Contains(t, logs.String(), "field=model")
}

type extraBodyTestRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream,omitempty"`
}

func TestMarshalWithExtraBody(t *testing.T) {
	req := extraBodyTestRequest{Model: "qwen3", Stream: true}

	t.Run("without extra fields it is json.Marshal", func(t *testing.T) {
		got, err := MarshalWithExtraBody("p", req, nil)
		require.NoError(t, err)
		want, _ := json.Marshal(req)
		assert.Equal(t, string(want), string(got))
	})
	t.Run("adds extra fields and keeps the provider's", func(t *testing.T) {
		logs := captureExtraBodyLogs(t)
		got, err := MarshalWithExtraBody("marshal-collision", req, map[string]any{
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
			"stream":               false,
		})
		require.NoError(t, err)
		var sent map[string]any
		require.NoError(t, json.Unmarshal(got, &sent))
		assert.Equal(t, map[string]any{
			"model":                "qwen3",
			"stream":               true,
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
		}, sent)
		assert.Contains(t, logs.String(), "field=stream")
	})
	t.Run("a request that is not a JSON object is an error", func(t *testing.T) {
		_, err := MarshalWithExtraBody("p", []string{"x"}, map[string]any{"a": 1})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a JSON object")
	})
}

// The same collision recurs on every request, so it is reported once.
func TestExtraBodyCollisionWarnsOnce(t *testing.T) {
	logs := captureExtraBodyLogs(t)
	for range 3 {
		ApplyExtraBody("collision-once", map[string]any{"model": "m"}, map[string]any{"model": "x"})
	}
	assert.Equal(t, 1, bytes.Count(logs.Bytes(), []byte("collision-once")))
}
