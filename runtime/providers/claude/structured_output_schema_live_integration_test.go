//go:build integration

package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Live guards for issue #2055: outputConfigFor posts the caller's schema to
// Anthropic verbatim, and Anthropic enforces rules on that schema that no
// other provider enforces. The adapter is the only layer that knows which
// provider it is talking to, so the adapter has to satisfy them.
//
// Each test is paired with an oracle that posts the same schema to the API
// directly. Without it a green adapter test proves nothing — it could mean the
// adapter normalized the schema, or it could mean the server stopped caring.
// See feedback: a live test is an oracle only if the server REJECTS the broken
// input.
//
// Run:
//
//	ANTHROPIC_API_KEY=... go test -tags integration ./runtime/providers/claude/ \
//	    -run 'TestClaude_OutputConfigSchema' -v

// schemaNestedObjectMissingAP is the reported shape: the top-level object sets
// additionalProperties, the object nested inside the array's items does not.
// This is why #2055 failed on one agent out of fourteen — the other thirteen
// were flat.
const schemaNestedObjectMissingAP = `{
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

// schemaWithNumericConstraints is the second rejection class in the same
// verbatim pass-through: Anthropic's structured outputs do not support
// numerical or string constraints. The Python and TypeScript SDKs strip these
// and validate them client-side; a Go adapter gets no such help.
const schemaWithNumericConstraints = `{
  "type": "object",
  "properties": {
    "name": {"type": "string", "minLength": 2, "maxLength": 10},
    "n": {"type": "integer", "minimum": 1, "maximum": 5}
  },
  "required": ["name", "n"],
  "additionalProperties": false
}`

// TestClaude_OutputConfigSchema_NestedObject_Live proves the defect and pins
// the fix: a caller's schema with a nested object that omits
// additionalProperties must still produce a completed turn.
func TestClaude_OutputConfigSchema_NestedObject_Live(t *testing.T) {
	key := liveClaudeKey(t)

	t.Run("the API rejects the schema as written", func(t *testing.T) {
		status, body := postRawOutputConfigSchema(t, key, schemaNestedObjectMissingAP)
		require.Equal(t, http.StatusBadRequest, status,
			"premise gone: Anthropic now accepts a nested object without "+
				"additionalProperties, so the adapter test below is vacuous. body=%s", body)
		assert.Contains(t, body, "additionalProperties",
			"expected the rejection to name the rule; body=%s", body)
	})

	t.Run("the adapter must send a schema the API accepts", func(t *testing.T) {
		resp := predictWithSchema(t, schemaNestedObjectMissingAP,
			"List two fruit names.")
		assert.NotEmpty(t, resp, "the turn must complete and return the model's JSON")
	})
}

// TestClaude_OutputConfigSchema_Constraints_Live covers the constraints
// Anthropic documents as unsupported. Same cause, same fix site, different
// error message.
func TestClaude_OutputConfigSchema_Constraints_Live(t *testing.T) {
	key := liveClaudeKey(t)

	t.Run("the API rejects the constraints", func(t *testing.T) {
		status, body := postRawOutputConfigSchema(t, key, schemaWithNumericConstraints)
		require.Equal(t, http.StatusBadRequest, status,
			"premise gone: Anthropic now accepts numeric constraints. body=%s", body)
		assert.Contains(t, body, "not supported",
			"expected the rejection to name the unsupported keywords; body=%s", body)
	})

	t.Run("the adapter must send a schema the API accepts", func(t *testing.T) {
		resp := predictWithSchema(t, schemaWithNumericConstraints,
			"Give me a short name and a number between 1 and 5.")
		assert.NotEmpty(t, resp, "the turn must complete and return the model's JSON")
	})
}

// TestClaude_ToolSchema_Strict_Live is the input-schema half.
//
// Strict tool use is Anthropic's native guarantee that tool_use.input
// validates against the schema, and it enforces the same rules structured
// outputs do. So the same caller schema that broke output_config breaks a
// strict tool definition — and the adapter has to satisfy both.
func TestClaude_ToolSchema_Strict_Live(t *testing.T) {
	key := liveClaudeKey(t)

	// A schema that fails BOTH rules: an object nested in the arguments with
	// no additionalProperties, and a numeric constraint the grammar rejects.
	toolSchema := json.RawMessage(`{
	  "type": "object",
	  "properties": {
	    "city": {"type": "string"},
	    "opts": {"type": "object", "properties": {"units": {"type": "string"}}},
	    "days": {"type": "integer", "minimum": 1, "maximum": 5}
	  },
	  "required": ["city"]
	}`)

	t.Run("the API rejects the schema as written", func(t *testing.T) {
		status, body := postRawStrictTool(t, key, string(toolSchema))
		require.Equal(t, http.StatusBadRequest, status,
			"premise gone: strict tool use now accepts this schema. body=%s", body)
		// Either rule is enough to lose the request; the API reports the
		// first one it hits, which is additionalProperties.
		assert.True(t,
			strings.Contains(body, "additionalProperties") || strings.Contains(body, "not supported"),
			"expected a rejection naming one of the schema rules; body=%s", body)
	})

	t.Run("the adapter must send a tool the API accepts", func(t *testing.T) {
		p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
			ID: "claude-tool-schema-live", Type: "claude", Model: liveClaudeModel(),
			BaseURL:  "https://api.anthropic.com/v1",
			Defaults: providers.ProviderDefaults{MaxTokens: 1024},
		})
		require.NoError(t, err)
		defer func() { _ = p.Close() }()

		tp, ok := p.(*ToolProvider)
		require.True(t, ok, "claude spec must build a ToolProvider, got %T", p)

		tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
			Name:        "get_weather",
			Description: "Get the weather forecast for a city",
			InputSchema: toolSchema,
		}})
		require.NoError(t, err)

		resp, calls, err := tp.PredictWithTools(context.Background(),
			providers.PredictionRequest{
				Messages: []types.Message{{
					Role:    "user",
					Content: "What's the weather in Bristol for the next 3 days? Use the tool.",
				}},
				MaxTokens: 1024,
			}, tools, "auto")
		require.NoError(t, err,
			"a caller's tool schema must not 400 the call under strict tool use (#2055)")
		require.NotEmpty(t, calls,
			"the model did not call the tool, so the strict path is untested; content=%.80q",
			resp.Content)

		// The guarantee strict mode buys: the arguments validate against the
		// schema the caller wrote, with no client-side repair.
		var args map[string]any
		require.NoError(t, json.Unmarshal(calls[0].Args, &args))
		assert.Contains(t, args, "city",
			"strict mode must produce arguments matching the schema; got %s", calls[0].Args)
	})
}

// TestClaude_SchemaAdaptation_UnsupportedKeywords_Live keeps the denylist
// honest.
//
// unsupportedKeywords is measured, not documented — Anthropic's docs say
// string constraints are unsupported while minLength and pattern are in fact
// accepted. A list like that rots silently in both directions, so this probes
// every entry against the live API: an entry the API now accepts fails here
// and should come out of the list.
func TestClaude_SchemaAdaptation_UnsupportedKeywords_Live(t *testing.T) {
	key := liveClaudeKey(t)

	// One representative schema per keyword, each valid apart from the
	// keyword under test.
	probes := map[string]string{
		"minimum":           `{"type":"object","properties":{"v":{"type":"number","minimum":1}},"additionalProperties":false}`,
		"maximum":           `{"type":"object","properties":{"v":{"type":"number","maximum":9}},"additionalProperties":false}`,
		"exclusiveMinimum":  `{"type":"object","properties":{"v":{"type":"number","exclusiveMinimum":0}},"additionalProperties":false}`,
		"exclusiveMaximum":  `{"type":"object","properties":{"v":{"type":"number","exclusiveMaximum":9}},"additionalProperties":false}`,
		"multipleOf":        `{"type":"object","properties":{"v":{"type":"number","multipleOf":2}},"additionalProperties":false}`,
		"maxItems":          `{"type":"object","properties":{"v":{"type":"array","items":{"type":"string"},"maxItems":3}},"additionalProperties":false}`,
		"uniqueItems":       `{"type":"object","properties":{"v":{"type":"array","items":{"type":"string"},"uniqueItems":true}},"additionalProperties":false}`,
		"contains":          `{"type":"object","properties":{"v":{"type":"array","items":{"type":"string"},"contains":{"type":"string"}}},"additionalProperties":false}`,
		"prefixItems":       `{"type":"object","properties":{"v":{"type":"array","prefixItems":[{"type":"string"}],"items":{"type":"number"}}},"additionalProperties":false}`,
		"minProperties":     `{"type":"object","properties":{"v":{"type":"string"}},"minProperties":1,"additionalProperties":false}`,
		"maxProperties":     `{"type":"object","properties":{"v":{"type":"string"}},"maxProperties":2,"additionalProperties":false}`,
		"patternProperties": `{"type":"object","properties":{"v":{"type":"string"}},"patternProperties":{"^a":{"type":"string"}},"additionalProperties":false}`,
		"propertyNames":     `{"type":"object","properties":{"v":{"type":"string"}},"propertyNames":{"pattern":"^v"},"additionalProperties":false}`,
		"dependentRequired": `{"type":"object","properties":{"v":{"type":"string"},"w":{"type":"string"}},"dependentRequired":{"v":["w"]},"additionalProperties":false}`,
		"not":               `{"type":"object","properties":{"v":{"not":{"type":"number"}}},"additionalProperties":false}`,
	}

	require.Len(t, probes, len(unsupportedKeywords),
		"every entry in unsupportedKeywords needs a probe, or the list is "+
			"asserting something nobody measured")

	for keyword, schema := range probes {
		require.True(t, unsupportedKeywords[keyword],
			"probe %q is not in the denylist", keyword)

		t.Run(keyword, func(t *testing.T) {
			status, body := postRawOutputConfigSchema(t, key, schema)
			assert.Equalf(t, http.StatusBadRequest, status,
				"Anthropic now ACCEPTS %q — take it out of unsupportedKeywords "+
					"rather than keep stripping a constraint the caller can have. body=%s",
				keyword, body)

			adapted := adaptSchemaForClaude(json.RawMessage(schema))
			okStatus, okBody := postRawOutputConfigSchema(t, key, string(adapted))

			if keyword == "not" {
				// "anything except X" has no equivalent in the accepted
				// subset, and removing it would leave a node with no type at
				// all. So the adapter keeps it and the request fails with an
				// error that names the cause, rather than one that says
				// "Empty schema" about a node the adapter emptied.
				assert.Equal(t, http.StatusBadRequest, okStatus)
				assert.Contains(t, okBody, "not",
					"the rejection must still name the keyword; body=%s", okBody)
				return
			}

			assert.Equalf(t, http.StatusOK, okStatus,
				"the adapted schema for %q must be accepted. body=%s", keyword, okBody)
		})
	}
}

// liveClaudeKey returns the API key or skips.
func liveClaudeKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		key = os.Getenv("CLAUDE_API_KEY")
	}
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	return key
}

func liveClaudeModel() string {
	if m := os.Getenv("CLAUDE_MODEL"); m != "" {
		return m
	}
	return "claude-sonnet-5"
}

// predictWithSchema runs a real turn through the configured provider — via
// CreateProviderFromSpec, since config-reached behavior is what breaks — and
// returns the response content.
func predictWithSchema(t *testing.T, schema, prompt string) string {
	t.Helper()

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "claude-schema-live", Type: "claude", Model: liveClaudeModel(),
		BaseURL:  "https://api.anthropic.com/v1",
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
		},
	})
	require.NoError(t, err,
		"a caller's schema must not 400 the call — the adapter has to satisfy "+
			"Anthropic's schema rules, because the caller cannot know them (#2055)")
	return resp.Content
}

// postRawOutputConfigSchema is the oracle: it posts the schema to the Messages
// API with no adapter in the path, so a passing adapter test cannot be a
// server that stopped enforcing the rule.
func postRawOutputConfigSchema(t *testing.T, key, schema string) (int, string) {
	t.Helper()

	return postRawMessages(t, key, map[string]any{
		"model":      liveClaudeModel(),
		"max_tokens": 256,
		"messages": []map[string]any{
			{"role": "user", "content": "List two fruit names."},
		},
		"output_config": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"schema": json.RawMessage(schema),
			},
		},
	})
}

// postRawStrictTool is the same oracle for the tool path.
func postRawStrictTool(t *testing.T, key, schema string) (int, string) {
	t.Helper()

	return postRawMessages(t, key, map[string]any{
		"model":      liveClaudeModel(),
		"max_tokens": 256,
		"messages": []map[string]any{
			{"role": "user", "content": "What's the weather in Bristol?"},
		},
		"tools": []map[string]any{{
			"name":         "get_weather",
			"description":  "Get the weather forecast for a city",
			"strict":       true,
			"input_schema": json.RawMessage(schema),
		}},
	})
}

func postRawMessages(t *testing.T, key string, payload map[string]any) (int, string) {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, strings.TrimSpace(string(raw))
}
