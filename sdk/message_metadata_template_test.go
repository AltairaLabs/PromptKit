package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMessageMetadataTemplate_ReachesTheWire checks the whole path: a
// ProviderSpec's MessageMetadataTemplate renders every message's metadata into
// the request the provider sends, on Send and on Stream, while the stored
// history keeps the user's own text.
func TestMessageMetadataTemplate_ReachesTheWire(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	srv := newRecordingOpenAIServer(t)
	packPath := createTestPackFile(t, messageMetadataTestPack)
	conv, err := Open(packPath, "chat", WithSkipSchemaValidation(), WithLLMProvider(ProviderSpec{
		ID: "agent", Type: "openai", Model: "gpt-4o-mini", BaseURL: srv.URL,
		MessageMetadataTemplate: "<context>Selected: {{selection}}</context>",
	}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	ctx := context.Background()

	_, err = conv.Send(ctx, "make it red", WithMessageMetadata(map[string]any{"selection": "circle-1"}))
	require.NoError(t, err)
	for chunk := range conv.Stream(ctx, "and bigger", WithMessageMetadata(map[string]any{"selection": []string{"a", "b"}})) {
		require.NoError(t, chunk.Error)
	}

	raw, err := json.Marshal(srv.lastBody()["messages"])
	require.NoError(t, err)
	var wire []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	require.NoError(t, json.Unmarshal(raw, &wire))
	var userTexts [][]string
	for _, m := range wire {
		if m.Role != "user" {
			continue
		}
		var parts []struct {
			Text string `json:"text"`
		}
		require.NoError(t, json.Unmarshal(m.Content, &parts))
		var texts []string
		for _, p := range parts {
			texts = append(texts, p.Text)
		}
		userTexts = append(userTexts, texts)
	}
	assert.Equal(t, [][]string{
		{"make it red", "\n\n<context>Selected: circle-1</context>"},
		{"and bigger", "\n\n" + `<context>Selected: ["a","b"]</context>`},
	}, userTexts, "every user message carries its own rendered metadata, the earlier turn's included")

	for _, m := range conv.Messages(ctx) {
		if m.Role == "user" {
			assert.NotContains(t, m.GetContent(), "<context>", "the stored message must keep the user's own text")
		}
	}
}
