package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReasoningFromParts_ThoughtAndSignature(t *testing.T) {
	parts := []geminiPart{
		{Text: "let me think", Thought: true, ThoughtSignature: "sig"},
		{Text: "the answer"},
	}
	rt := reasoningFromParts(parts)
	require.NotNil(t, rt)
	assert.Equal(t, "let me think", rt.Text)
	require.Len(t, rt.Opaque, 1)
	assert.Equal(t, "gemini", rt.Opaque[0].Provider)
	assert.Equal(t, "thought_signature", rt.Opaque[0].Kind)
	assert.Equal(t, "sig", rt.Opaque[0].Data)
}

func TestReasoningFromParts_None(t *testing.T) {
	// Plain text and a signed functionCall part carry no *thought* text → nil.
	parts := []geminiPart{
		{Text: "hello"},
		{FunctionCall: &geminiPartFuncCall{Name: "f"}, ThoughtSignature: "sig"},
	}
	if reasoningFromParts(parts) != nil {
		t.Fatal("expected nil reasoning when no thought parts")
	}
}
