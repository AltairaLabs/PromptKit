package render

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestRenderMessageContent_IncludesReasoning(t *testing.T) {
	msg := types.Message{
		Role:      "assistant",
		Content:   "the answer",
		Reasoning: &types.ReasoningTrace{Text: "the chain of thought"},
	}
	out := string(renderMessageContent(msg))
	assert.Contains(t, out, "the answer", "spoken content present")
	assert.Contains(t, out, "Reasoning", "reasoning section present")
	assert.Contains(t, out, "the chain of thought")
	assert.Contains(t, out, "<details", "reasoning rendered as a collapsible section")
}

func TestRenderMessageContent_EscapesReasoning(t *testing.T) {
	msg := types.Message{
		Role:      "assistant",
		Reasoning: &types.ReasoningTrace{Text: "<script>alert(1)</script>"},
	}
	out := string(renderMessageContent(msg))
	assert.NotContains(t, out, "<script>", "reasoning text must be HTML-escaped")
}

func TestRenderMessageContent_NoReasoning(t *testing.T) {
	out := string(renderMessageContent(types.Message{Role: "assistant", Content: "x"}))
	assert.False(t, strings.Contains(out, "💭 Reasoning"), "no reasoning section when absent")
}
