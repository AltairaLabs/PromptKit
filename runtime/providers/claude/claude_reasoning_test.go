package claude

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractReasoning_TextAndSignature(t *testing.T) {
	content := []claudeContent{
		{Type: types.ContentTypeThinking, Text: "let me reason", Signature: "sig"},
		{Type: "text", Text: "the answer"},
	}

	rt := extractReasoning(content)
	require.NotNil(t, rt, "expected a reasoning trace")
	assert.Equal(t, "let me reason", rt.Text)
	require.Len(t, rt.Opaque, 1)
	assert.Equal(t, "claude", rt.Opaque[0].Provider)
	assert.Equal(t, "thinking_signature", rt.Opaque[0].Kind)
	assert.Equal(t, "sig", rt.Opaque[0].Data)

	// Reasoning must NOT appear as a content part.
	parts := extractContentParts(content)
	require.Len(t, parts, 1, "only the text part should remain")
	assert.Equal(t, types.ContentTypeText, parts[0].Type)
}

func TestExtractReasoning_RedactedThinking(t *testing.T) {
	rt := extractReasoning([]claudeContent{{Type: "redacted_thinking", Data: "enc"}})
	require.NotNil(t, rt)
	assert.True(t, rt.Redacted)
	require.Len(t, rt.Opaque, 1)
	assert.Equal(t, "redacted_thinking", rt.Opaque[0].Kind)
	assert.Equal(t, "enc", rt.Opaque[0].Data)
}

func TestExtractReasoning_None(t *testing.T) {
	if extractReasoning([]claudeContent{{Type: "text", Text: "hi"}}) != nil {
		t.Fatal("expected nil reasoning when none present")
	}
}
