//go:build integration

package gemini

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

// The Gemini half of issue #2055's question: does the same caller schema work
// here? It does, and only because sanitizeGeminiSchema strips the keyword
// Gemini rejects.
//
// The two providers enforce opposite rules on the identical schema —
// Anthropic REQUIRES additionalProperties: false on every object, Gemini
// REJECTS the keyword outright — which is the argument for normalizing in the
// adapter rather than asking callers to write a portable schema. There isn't
// one.
//
// Run:
//
//	GEMINI_API_KEY=... go test -tags integration ./runtime/providers/gemini/ \
//	    -run TestGemini_ResponseSchema_PortableSchema_Live -v

// portableSchemaWithAdditionalProperties is the same schema the Claude live
// guard uses: additionalProperties at the top level, absent on the object
// nested in the array.
const portableSchemaWithAdditionalProperties = `{
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

func TestGemini_ResponseSchema_PortableSchema_Live(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	t.Run("the API rejects additionalProperties", func(t *testing.T) {
		status, body := postRawResponseSchema(t, key, portableSchemaWithAdditionalProperties)
		require.Equal(t, http.StatusBadRequest, status,
			"premise gone: Gemini now tolerates additionalProperties, so the "+
				"sanitizer below is no longer load-bearing. body=%s", body)
		assert.Contains(t, body, "additionalProperties",
			"expected the rejection to name the keyword; body=%s", body)
	})

	t.Run("the adapter strips it and the turn completes", func(t *testing.T) {
		p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
			ID: "gemini-schema-live", Type: "gemini", Model: liveGeminiSchemaModel(),
			BaseURL:  "https://generativelanguage.googleapis.com/v1beta",
			Defaults: providers.ProviderDefaults{MaxTokens: 1024},
		})
		require.NoError(t, err)
		defer func() { _ = p.Close() }()

		resp, err := p.Predict(context.Background(), providers.PredictionRequest{
			Messages:  []types.Message{{Role: "user", Content: "List two fruit names."}},
			MaxTokens: 1024,
			ResponseFormat: &providers.ResponseFormat{
				Type:       providers.ResponseFormatJSONSchema,
				JSONSchema: json.RawMessage(portableSchemaWithAdditionalProperties),
			},
		})
		require.NoError(t, err,
			"the adapter must strip what Gemini rejects — the caller's schema is "+
				"written for a pack, not for one vendor")
		assert.NotEmpty(t, resp.Content)
	})
}

func liveGeminiSchemaModel() string {
	if m := os.Getenv("GEMINI_MODEL"); m != "" {
		return m
	}
	return "gemini-2.5-flash"
}

// postRawResponseSchema is the oracle: the schema goes to generateContent with
// no adapter in the path.
func postRawResponseSchema(t *testing.T, key, schema string) (int, string) {
	t.Helper()

	payload := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "List two fruit names."}}},
		},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"responseSchema":   json.RawMessage(schema),
		},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	url := "https://generativelanguage.googleapis.com/v1beta/models/" +
		liveGeminiSchemaModel() + ":generateContent?key=" + key
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url,
		bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, strings.TrimSpace(string(raw))
}

// TestGemini_ToolSchema_PortableSchema_Live is the input-schema half.
//
// Function-declaration parameters go through the same OpenAPI-subset parser as
// responseSchema, and were never sanitized. A pack tool schema carries
// additionalProperties — it has to, for OpenAI and Anthropic strict mode — so
// the portable schema was the one Gemini refused.
func TestGemini_ToolSchema_PortableSchema_Live(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	toolSchema := `{
	  "type": "object",
	  "properties": {
	    "city": {"type": "string"},
	    "opts": {"type": "object", "properties": {"units": {"type": "string"}}, "additionalProperties": false}
	  },
	  "required": ["city"],
	  "additionalProperties": false
	}`

	t.Run("the API rejects the parameters as written", func(t *testing.T) {
		status, body := postRawFunctionDeclaration(t, key, toolSchema)
		require.Equal(t, http.StatusBadRequest, status,
			"premise gone: Gemini now tolerates additionalProperties in tool "+
				"parameters. body=%s", body)
		assert.Contains(t, body, "additionalProperties",
			"expected the rejection to name the keyword; body=%s", body)
	})

	t.Run("the adapter strips it and the tool call happens", func(t *testing.T) {
		p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
			ID: "gemini-tool-schema-live", Type: "gemini", Model: liveGeminiSchemaModel(),
			BaseURL:  "https://generativelanguage.googleapis.com/v1beta",
			Defaults: providers.ProviderDefaults{MaxTokens: 1024},
		})
		require.NoError(t, err)
		defer func() { _ = p.Close() }()

		tp, ok := p.(*ToolProvider)
		require.True(t, ok, "gemini spec must build a ToolProvider, got %T", p)

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
		require.NoError(t, err,
			"a pack tool schema must not 400 the call — the adapter has to strip "+
				"what Gemini rejects (#2055)")
		require.NotEmpty(t, calls,
			"the model did not call the tool, so nothing is proven; content=%.80q",
			resp.Content)
	})
}

// postRawFunctionDeclaration is the oracle for the tool path.
func postRawFunctionDeclaration(t *testing.T, key, schema string) (int, string) {
	t.Helper()

	payload := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "Weather in Bristol?"}}},
		},
		"tools": []map[string]any{{
			"functionDeclarations": []map[string]any{{
				"name":        "get_weather",
				"description": "Get the current weather for a city",
				"parameters":  json.RawMessage(schema),
			}},
		}},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	url := "https://generativelanguage.googleapis.com/v1beta/models/" +
		liveGeminiSchemaModel() + ":generateContent?key=" + key
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url,
		bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, strings.TrimSpace(string(raw))
}
