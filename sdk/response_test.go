package sdk

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestResponseText(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		r := &Response{}
		assert.Equal(t, "", r.Text())
	})

	t.Run("with content", func(t *testing.T) {
		msg := &types.Message{Role: "assistant"}
		msg.AddTextPart("Hello, world!")
		r := &Response{message: msg}
		assert.Equal(t, "Hello, world!", r.Text())
	})
}

func TestResponseMessage(t *testing.T) {
	msg := &types.Message{Role: "assistant"}
	r := &Response{message: msg}
	assert.Same(t, msg, r.Message())
}

func TestResponseParts(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		r := &Response{}
		assert.Nil(t, r.Parts())
	})

	t.Run("with parts", func(t *testing.T) {
		hello := "Hello"
		world := "World"
		msg := &types.Message{
			Role: "assistant",
			Parts: []types.ContentPart{
				{Type: "text", Text: &hello},
				{Type: "text", Text: &world},
			},
		}
		r := &Response{message: msg}
		assert.Len(t, r.Parts(), 2)
	})
}

func TestResponseHasMedia(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		r := &Response{}
		assert.False(t, r.HasMedia())
	})

	t.Run("text only", func(t *testing.T) {
		msg := &types.Message{Role: "assistant"}
		msg.AddTextPart("Just text")
		r := &Response{message: msg}
		assert.False(t, r.HasMedia())
	})
}

func TestResponseToolCalls(t *testing.T) {
	calls := []types.MessageToolCall{
		{ID: "call1", Name: "get_weather"},
		{ID: "call2", Name: "search"},
	}
	r := &Response{toolCalls: calls}
	assert.Equal(t, calls, r.ToolCalls())
}

func TestResponseValidations(t *testing.T) {
	vals := []types.ValidationResult{
		{ValidatorType: "max_length", Passed: true},
		{ValidatorType: "banned_words", Passed: false},
	}
	r := &Response{validations: vals}
	assert.Equal(t, vals, r.Validations())
}

func TestResponseTokensUsed(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		r := &Response{}
		assert.Equal(t, 0, r.TokensUsed())
	})

	t.Run("nil cost info", func(t *testing.T) {
		r := &Response{message: &types.Message{}}
		assert.Equal(t, 0, r.TokensUsed())
	})

	t.Run("with cost info", func(t *testing.T) {
		r := &Response{
			message: &types.Message{
				CostInfo: &types.CostInfo{
					InputTokens:  100,
					OutputTokens: 50,
				},
			},
		}
		assert.Equal(t, 150, r.TokensUsed())
	})
}

func TestResponseInputTokens(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		r := &Response{}
		assert.Equal(t, 0, r.InputTokens())
	})

	t.Run("nil cost info", func(t *testing.T) {
		r := &Response{message: &types.Message{}}
		assert.Equal(t, 0, r.InputTokens())
	})

	t.Run("with cost info", func(t *testing.T) {
		r := &Response{
			message: &types.Message{
				CostInfo: &types.CostInfo{InputTokens: 100},
			},
		}
		assert.Equal(t, 100, r.InputTokens())
	})
}

func TestResponseOutputTokens(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		r := &Response{}
		assert.Equal(t, 0, r.OutputTokens())
	})

	t.Run("nil cost info", func(t *testing.T) {
		r := &Response{message: &types.Message{}}
		assert.Equal(t, 0, r.OutputTokens())
	})

	t.Run("with cost info", func(t *testing.T) {
		r := &Response{
			message: &types.Message{
				CostInfo: &types.CostInfo{OutputTokens: 50},
			},
		}
		assert.Equal(t, 50, r.OutputTokens())
	})
}

func TestResponseCost(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		r := &Response{}
		assert.Equal(t, 0.0, r.Cost())
	})

	t.Run("nil cost info", func(t *testing.T) {
		r := &Response{message: &types.Message{}}
		assert.Equal(t, 0.0, r.Cost())
	})

	t.Run("with cost info", func(t *testing.T) {
		r := &Response{
			message: &types.Message{
				CostInfo: &types.CostInfo{TotalCost: 0.025},
			},
		}
		assert.InDelta(t, 0.025, r.Cost(), 0.0001)
	})
}

func TestResponseDuration(t *testing.T) {
	r := &Response{duration: 500 * time.Millisecond}
	assert.Equal(t, 500*time.Millisecond, r.Duration())
}

func TestResponsePendingTools(t *testing.T) {
	pending := []PendingTool{
		{ID: "pt1", Name: "dangerous_action"},
	}
	r := &Response{pendingTools: pending}
	assert.Equal(t, pending, r.PendingTools())
}

func TestResponseHasToolCalls(t *testing.T) {
	t.Run("no tool calls", func(t *testing.T) {
		r := &Response{}
		assert.False(t, r.HasToolCalls())
	})

	t.Run("with tool calls", func(t *testing.T) {
		r := &Response{
			toolCalls: []types.MessageToolCall{{ID: "call1"}},
		}
		assert.True(t, r.HasToolCalls())
	})
}

func TestChunkTypeString(t *testing.T) {
	tests := []struct {
		name string
		typ  ChunkType
		want string
	}{
		{"text", ChunkText, "text"},
		{"tool_call", ChunkToolCall, "tool_call"},
		{"media", ChunkMedia, "media"},
		{"done", ChunkDone, "done"},
		{"unknown", ChunkType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.typ.String()
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestStreamChunkType(t *testing.T) {
	// Test creating StreamChunks with different types
	t.Run("text chunk", func(t *testing.T) {
		chunk := StreamChunk{Type: ChunkText, Text: "Hello"}
		assert.Equal(t, ChunkText, chunk.Type)
		assert.Equal(t, "Hello", chunk.Text)
	})

	t.Run("done chunk", func(t *testing.T) {
		chunk := StreamChunk{Type: ChunkDone}
		assert.Equal(t, ChunkDone, chunk.Type)
	})

	t.Run("tool call chunk", func(t *testing.T) {
		chunk := StreamChunk{Type: ChunkToolCall}
		assert.Equal(t, ChunkToolCall, chunk.Type)
	})

	t.Run("media chunk", func(t *testing.T) {
		chunk := StreamChunk{Type: ChunkMedia}
		assert.Equal(t, ChunkMedia, chunk.Type)
	})
}
