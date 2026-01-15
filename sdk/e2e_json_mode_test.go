//go:build e2e

package sdk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// JSON Mode E2E Tests
//
// These tests verify JSON mode output functionality across all providers
// that support the JSON capability.
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_JSON
// =============================================================================

// TestE2E_JSON_BasicJSONMode tests simple JSON object mode output.
func TestE2E_JSON_BasicJSONMode(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapJSON, func(t *testing.T, provider ProviderConfig) {
		// Skip mock for real provider tests
		if provider.ID == "mock" {
			t.Skip("Skipping mock for JSON mode test")
		}

		conv := NewProviderConversation(t, provider, WithJSONMode())
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "List 3 colors. Return a JSON object with a 'colors' array containing the color names as strings.")
		require.NoError(t, err)
		require.NotNil(t, resp)

		text := resp.Text()
		assert.NotEmpty(t, text)

		// Verify it's valid JSON
		var result map[string]interface{}
		err = json.Unmarshal([]byte(text), &result)
		require.NoError(t, err, "Response should be valid JSON: %s", text)

		// Verify structure
		colors, ok := result["colors"]
		assert.True(t, ok, "JSON should have 'colors' key")

		if colorArray, ok := colors.([]interface{}); ok {
			assert.GreaterOrEqual(t, len(colorArray), 3, "Should have at least 3 colors")
		}

		t.Logf("Provider %s JSON response: %s", provider.ID, truncateJSON(text, 200))
	})
}

// TestE2E_JSON_SchemaMode tests JSON schema mode with strict structure.
func TestE2E_JSON_SchemaMode(t *testing.T) {
	EnsureTestPacks(t)

	// Define a schema for a person object
	// Note: OpenAI requires additionalProperties: false for strict mode
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "integer"},
			"email": {"type": "string"}
		},
		"required": ["name", "age", "email"],
		"additionalProperties": false
	}`)

	RunForProviders(t, CapJSON, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for JSON schema test")
		}

		// Anthropic doesn't support JSON schema mode (only json_object)
		if provider.ID == "anthropic" {
			t.Skip("Anthropic doesn't support JSON schema mode")
		}

		conv := NewProviderConversation(t, provider, WithResponseFormat(&providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			JSONSchema: schema,
			SchemaName: "person",
			Strict:     true,
		}))
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "Generate a fictional person with name, age, and email. Return only the JSON object.")
		require.NoError(t, err)
		require.NotNil(t, resp)

		text := resp.Text()
		assert.NotEmpty(t, text)

		// Verify it's valid JSON matching the schema
		var person struct {
			Name  string `json:"name"`
			Age   int    `json:"age"`
			Email string `json:"email"`
		}
		err = json.Unmarshal([]byte(text), &person)
		require.NoError(t, err, "Response should be valid JSON matching schema: %s", text)

		// Verify required fields are present and valid
		assert.NotEmpty(t, person.Name, "Name should not be empty")
		assert.Greater(t, person.Age, 0, "Age should be positive")
		assert.Contains(t, person.Email, "@", "Email should contain @")

		t.Logf("Provider %s schema response: %s", provider.ID, truncateJSON(text, 200))
	})
}

// TestE2E_JSON_StreamingJSONMode tests JSON mode with streaming responses.
func TestE2E_JSON_StreamingJSONMode(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapJSON, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for streaming JSON test")
		}

		// Only test providers that support both JSON and streaming
		if !provider.HasCapability(CapStreaming) {
			t.Skip("Provider doesn't support streaming")
		}

		conv := NewProviderConversation(t, provider, WithJSONMode())
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var chunks []StreamChunk
		var fullText strings.Builder

		for chunk := range conv.Stream(ctx, "Return a JSON object with keys 'greeting' and 'farewell', each containing a short phrase.") {
			if chunk.Error != nil {
				t.Fatalf("Stream error: %v", chunk.Error)
			}
			chunks = append(chunks, chunk)
			if chunk.Type == ChunkText {
				fullText.WriteString(chunk.Text)
			}
			if chunk.Type == ChunkDone {
				break
			}
		}

		text := fullText.String()
		assert.NotEmpty(t, text)

		// Verify the streamed content is valid JSON
		var result map[string]interface{}
		err := json.Unmarshal([]byte(text), &result)
		require.NoError(t, err, "Streamed response should be valid JSON: %s", text)

		// Verify expected keys
		_, hasGreeting := result["greeting"]
		_, hasFarewell := result["farewell"]
		assert.True(t, hasGreeting, "JSON should have 'greeting' key")
		assert.True(t, hasFarewell, "JSON should have 'farewell' key")

		t.Logf("Provider %s streamed %d chunks, JSON: %s", provider.ID, len(chunks), truncateJSON(text, 200))
	})
}

// TestE2E_JSON_ComplexObject tests JSON mode with a more complex nested structure.
func TestE2E_JSON_ComplexObject(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapJSON, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for complex JSON test")
		}

		conv := NewProviderConversation(t, provider, WithJSONMode())
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, `Return a JSON object representing a book with:
- title (string)
- author (object with 'name' and 'country' fields)
- published_year (number)
- genres (array of strings)
Make up fictional data.`)
		require.NoError(t, err)
		require.NotNil(t, resp)

		text := resp.Text()
		assert.NotEmpty(t, text)

		// Verify it's valid JSON with nested structure
		var book struct {
			Title         string   `json:"title"`
			Author        struct {
				Name    string `json:"name"`
				Country string `json:"country"`
			} `json:"author"`
			PublishedYear int      `json:"published_year"`
			Genres        []string `json:"genres"`
		}
		err = json.Unmarshal([]byte(text), &book)
		require.NoError(t, err, "Response should be valid JSON: %s", text)

		// Verify structure
		assert.NotEmpty(t, book.Title, "Title should not be empty")
		assert.NotEmpty(t, book.Author.Name, "Author name should not be empty")
		assert.Greater(t, book.PublishedYear, 0, "Published year should be positive")
		assert.NotEmpty(t, book.Genres, "Genres should not be empty")

		t.Logf("Provider %s complex JSON: %s", provider.ID, truncateJSON(text, 300))
	})
}

// TestE2E_JSON_ArrayResponse tests JSON mode returning an array.
func TestE2E_JSON_ArrayResponse(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapJSON, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for array JSON test")
		}

		conv := NewProviderConversation(t, provider, WithJSONMode())
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Note: Some providers may wrap array in object, so we ask for object with array
		resp, err := conv.Send(ctx, `Return a JSON object with an "items" array containing 3 objects, each with "id" (number) and "value" (string) fields.`)
		require.NoError(t, err)
		require.NotNil(t, resp)

		text := resp.Text()
		assert.NotEmpty(t, text)

		// Verify it's valid JSON
		var result struct {
			Items []struct {
				ID    int    `json:"id"`
				Value string `json:"value"`
			} `json:"items"`
		}
		err = json.Unmarshal([]byte(text), &result)
		require.NoError(t, err, "Response should be valid JSON: %s", text)

		// Verify array content
		assert.GreaterOrEqual(t, len(result.Items), 3, "Should have at least 3 items")

		t.Logf("Provider %s array JSON: %s", provider.ID, truncateJSON(text, 200))
	})
}

// =============================================================================
// Helpers
// =============================================================================

// truncateJSON truncates a JSON string for logging, keeping it readable.
func truncateJSON(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	// Compact the JSON for display
	var buf strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' {
			buf.WriteByte(' ')
		} else {
			buf.WriteRune(r)
		}
	}
	s = buf.String()
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
