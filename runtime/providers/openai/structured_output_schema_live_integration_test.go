//go:build integration

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The OpenAI half of issue #2055. The defect is the same shape and the same
// omission, one layer over: BuildTooling runs ensureStrictSchema over every
// TOOL schema, but buildResponseFormat posts the caller's RESPONSE schema
// verbatim with strict: rf.Strict — and the composition executor sets
// Strict: true on every step that declares an output_schema
// (runtime/pipeline/stage/composition_executor.go).
//
// OpenAI strict mode enforces two rules, not one:
//   - additionalProperties: false on every object, recursively
//   - every property listed in required
//
// ensureStrictSchema already implements both. Nothing calls it on this path.
//
// Run:
//
//	OPENAI_API_KEY=... go test -tags integration ./runtime/providers/openai/ \
//	    -run 'TestOpenAI_ResponseFormatSchema' -v

// strictSchemaMissingAP omits additionalProperties on the object nested inside
// the array — the shape reported in #2055.
const strictSchemaMissingAP = `{
  "type": "object",
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {"name": {"type": "string"}},
        "required": ["name"]
      }
    }
  },
  "required": ["items"],
  "additionalProperties": false
}`

// strictSchemaPartialRequired lists only one of two properties in required.
// Anthropic accepts this (verified live); OpenAI strict mode does not — which
// is why the normalization has to be per-provider.
const strictSchemaPartialRequired = `{
  "type": "object",
  "properties": {"a": {"type": "string"}, "b": {"type": "string"}},
  "required": ["a"],
  "additionalProperties": false
}`

func TestOpenAI_ResponseFormatSchema_NestedObject_Live(t *testing.T) {
	key := liveOpenAIKey(t)

	t.Run("the API rejects the schema as written", func(t *testing.T) {
		status, body := postRawResponseFormatSchema(t, key, strictSchemaMissingAP)
		skipIfThrottled(t, status, body)
		require.Equal(t, http.StatusBadRequest, status,
			"premise gone: OpenAI strict mode now accepts a nested object without "+
				"additionalProperties. body=%s", body)
		assert.Contains(t, body, "additionalProperties",
			"expected the rejection to name the rule; body=%s", body)
	})

	t.Run("the adapter must send a schema the API accepts", func(t *testing.T) {
		resp := predictWithStrictSchema(t, strictSchemaMissingAP, "List two fruit names.")
		assert.NotEmpty(t, resp, "the turn must complete and return the model's JSON")
	})
}

func TestOpenAI_ResponseFormatSchema_PartialRequired_Live(t *testing.T) {
	key := liveOpenAIKey(t)

	t.Run("the API rejects a partial required list", func(t *testing.T) {
		status, body := postRawResponseFormatSchema(t, key, strictSchemaPartialRequired)
		skipIfThrottled(t, status, body)
		require.Equal(t, http.StatusBadRequest, status,
			"premise gone: OpenAI strict mode no longer requires every property "+
				"in required. body=%s", body)
		assert.Contains(t, body, "required",
			"expected the rejection to name the rule; body=%s", body)
	})

	t.Run("the adapter must send a schema the API accepts", func(t *testing.T) {
		resp := predictWithStrictSchema(t, strictSchemaPartialRequired,
			"Give me values for a and b.")
		assert.NotEmpty(t, resp, "the turn must complete and return the model's JSON")
	})
}

func liveOpenAIKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	return key
}

func liveOpenAISchemaModel() string {
	if m := os.Getenv("OPENAI_MODEL"); m != "" {
		return m
	}
	return "gpt-4o-mini"
}

// skipIfThrottled distinguishes "the request never reached schema validation"
// from "the API accepted the broken schema". Both an exhausted balance and a
// rate limit are refused before validation runs, so a 429 is evidence of
// nothing either way — and must not read as a pass.
func skipIfThrottled(t *testing.T, status int, body string) {
	t.Helper()
	if status != http.StatusTooManyRequests {
		return
	}
	t.Skipf("OpenAI returned 429 (%s), so the schema was never validated. body=%s",
		throttleReason(body), body)
}

// throttleReason names which kind of 429 arrived, because "no credit" and
// "too many requests in the last minute" call for different responses from
// whoever reads the skip.
func throttleReason(body string) string {
	if strings.Contains(body, "insufficient_quota") ||
		strings.Contains(body, "credit_balance_exhausted") {
		return "account balance exhausted"
	}
	return "rate limited"
}

// skipIfThrottledErr is the same judgement for an error returned through the
// provider, which reports the status in its message after exhausting retries.
func skipIfThrottledErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "429") || strings.Contains(msg, "insufficient_quota") {
		t.Skipf("OpenAI returned 429 (%s), so the schema was never validated: %v",
			throttleReason(msg), err)
	}
}

func predictWithStrictSchema(t *testing.T, schema, prompt string) string {
	t.Helper()

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "openai-schema-live", Type: "openai", Model: liveOpenAISchemaModel(),
		BaseURL:  "https://api.openai.com/v1",
		Defaults: providers.ProviderDefaults{MaxTokens: 1024},
	})
	require.NoError(t, err)
	defer func() { _ = p.Close() }()

	resp, err := p.Predict(context.Background(), providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: prompt}},
		MaxTokens: 1024,
		ResponseFormat: &providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			JSONSchema: json.RawMessage(schema),
			Strict:     true, // what composition_executor.go sets
		},
	})
	skipIfThrottledErr(t, err)
	require.NoError(t, err,
		"a caller's schema must not 400 the call — ensureStrictSchema exists for "+
			"exactly this and the response-format path never calls it (#2055)")
	return resp.Content
}

// postRawResponseFormatSchema is the oracle: no adapter in the path.
func postRawResponseFormatSchema(t *testing.T, key, schema string) (int, string) {
	t.Helper()

	payload := map[string]any{
		"model": liveOpenAISchemaModel(),
		"messages": []map[string]any{
			{"role": "user", "content": "List two fruit names."},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "response_schema",
				"strict": true,
				"schema": json.RawMessage(schema),
			},
		},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("content-type", "application/json")

	return postRaw(t, req, body)
}

// TestOpenAI_ToolSchema_Strict_Live is the input-schema half. ensureStrictSchema
// has always run over tool schemas, but its private recursion only descended
// through "properties" and a map-valued "items" — so an object under $defs or
// an anyOf branch still reached the API non-compliant.
func TestOpenAI_ToolSchema_Strict_Live(t *testing.T) {
	key := liveOpenAIKey(t)

	toolSchema := `{
	  "type": "object",
	  "properties": {
	    "city": {"type": "string"},
	    "opts": {"$ref": "#/$defs/opts"}
	  },
	  "required": ["city"],
	  "$defs": {"opts": {"type": "object", "properties": {"units": {"type": "string"}}}}
	}`

	t.Run("the API rejects the schema as written", func(t *testing.T) {
		status, body := postRawStrictTool(t, key, toolSchema)
		skipIfThrottled(t, status, body)
		require.Equal(t, http.StatusBadRequest, status,
			"premise gone: strict mode now accepts an object under $defs without "+
				"additionalProperties. body=%s", body)
		assert.Contains(t, body, "additionalProperties",
			"expected the rejection to name the rule; body=%s", body)
	})

	t.Run("the adapter must send a tool the API accepts", func(t *testing.T) {
		p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
			ID: "openai-tool-schema-live", Type: "openai", Model: liveOpenAISchemaModel(),
			BaseURL:  "https://api.openai.com/v1",
			Defaults: providers.ProviderDefaults{MaxTokens: 1024},
		})
		require.NoError(t, err)
		defer func() { _ = p.Close() }()

		tp, ok := p.(*ToolProvider)
		require.True(t, ok, "openai spec must build a ToolProvider, got %T", p)

		tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
			Name:        "get_weather",
			Description: "Get the current weather for a city",
			InputSchema: json.RawMessage(toolSchema),
		}})
		require.NoError(t, err)

		resp, calls, err := tp.PredictWithTools(context.Background(),
			providers.PredictionRequest{
				Messages: []types.Message{{
					Role:    "user",
					Content: "What's the weather in Bristol? Use the tool.",
				}},
				MaxTokens: 1024,
			}, tools, "auto")
		skipIfThrottledErr(t, err)
		require.NoError(t, err,
			"a caller's tool schema must not 400 the call under strict mode (#2055)")
		require.NotEmpty(t, calls,
			"the model did not call the tool, so nothing is proven; content=%.80q",
			resp.Content)
	})
}

// postRawStrictTool is the oracle for the tool path.
// postRaw sends one request and retries a 429.
//
// The provider path retries internally, so without this the oracle is the only
// call in the pair with no retry: an intermittent throttle turns a real
// verification into a skip while the adapter assertion beside it passes. This
// is not tolerance of a bad schema — the assertion still demands a 400.
func postRaw(t *testing.T, req *http.Request, body []byte) (int, string) {
	t.Helper()

	const attempts = 4
	var status int
	var text string
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * 2 * time.Second)
			req = req.Clone(context.Background())
			req.Body = io.NopCloser(bytes.NewReader(body))
		}

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		raw, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.NoError(t, err)

		status, text = resp.StatusCode, strings.TrimSpace(string(raw))
		if status != http.StatusTooManyRequests {
			return status, text
		}
		t.Logf("attempt %d: 429 (%s), retrying", i+1, throttleReason(text))
	}
	return status, text
}

func postRawStrictTool(t *testing.T, key, schema string) (int, string) {
	t.Helper()

	payload := map[string]any{
		"model": liveOpenAISchemaModel(),
		"messages": []map[string]any{
			{"role": "user", "content": "What's the weather in Bristol?"},
		},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Get the current weather for a city",
				"strict":      true,
				"parameters":  json.RawMessage(schema),
			},
		}},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("content-type", "application/json")

	return postRaw(t, req, body)
}
