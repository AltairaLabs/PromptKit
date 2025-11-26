package middleware

import (
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextBuilderMiddleware_NilPolicy(t *testing.T) {
	middleware := ContextBuilderMiddleware(nil)
	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "test message"},
		},
	}

	called := false
	err := middleware.Process(execCtx, func() error {
		called = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, called)
	assert.Len(t, execCtx.Messages, 1) // No truncation
}

func TestContextBuilderMiddleware_ZeroBudget(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget: 0, // unlimited
		Strategy:    TruncateOldest,
	}
	middleware := ContextBuilderMiddleware(policy)
	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "test message one"},
			{Role: "assistant", Content: "response one"},
			{Role: "user", Content: "test message two"},
		},
	}

	err := middleware.Process(execCtx, func() error { return nil })

	assert.NoError(t, err)
	assert.Len(t, execCtx.Messages, 3) // No truncation with unlimited budget
}

func TestContextBuilderMiddleware_UnderBudget(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget:      1000,
		ReserveForOutput: 100,
		Strategy:         TruncateOldest,
	}
	middleware := ContextBuilderMiddleware(policy)
	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "You are a helpful assistant",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Metadata: make(map[string]interface{}),
	}

	err := middleware.Process(execCtx, func() error { return nil })

	assert.NoError(t, err)
	assert.Len(t, execCtx.Messages, 1)
	assert.Nil(t, execCtx.Metadata["context_truncated"])
}

func TestContextBuilderMiddleware_TruncateOldest(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget:      30, // Very small budget to force truncation
		ReserveForOutput: 5,
		Strategy:         TruncateOldest,
	}
	middleware := ContextBuilderMiddleware(policy)
	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "system",
		Messages: []types.Message{
			{Role: "user", Content: "message one with lots of words to exceed token budget and force truncation"},
			{Role: "assistant", Content: "response one with more words that will also be truncated"},
			{Role: "user", Content: "message two with even more content"},
			{Role: "assistant", Content: "response two"},
		},
		Metadata: make(map[string]interface{}),
	}

	err := middleware.Process(execCtx, func() error { return nil })

	assert.NoError(t, err)
	assert.Less(t, len(execCtx.Messages), 4, "Messages should be truncated")

	// Check metadata
	truncated, ok := execCtx.Metadata["context_truncated"]
	if ok && truncated != nil {
		assert.True(t, truncated.(bool))
		assert.Equal(t, 4, execCtx.Metadata["context_original_count"])
		assert.Greater(t, execCtx.Metadata["context_dropped_count"].(int), 0)
	}

	// Most recent messages should be kept
	if len(execCtx.Messages) > 0 {
		assert.Equal(t, "response two", execCtx.Messages[len(execCtx.Messages)-1].Content)
	}
}

func TestContextBuilderMiddleware_TruncateFail(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget:      30,
		ReserveForOutput: 5,
		Strategy:         TruncateFail,
	}
	middleware := ContextBuilderMiddleware(policy)
	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "system",
		Messages: []types.Message{
			{Role: "user", Content: "message one with lots of words to exceed token budget and trigger failure"},
			{Role: "assistant", Content: "response one with more words that will exceed the budget"},
			{Role: "user", Content: "message two with additional content"},
		},
		Metadata: make(map[string]interface{}),
	}

	err := middleware.Process(execCtx, func() error { return nil })

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token budget exceeded")
}

func TestContextBuilderMiddleware_TruncateLeastRelevant(t *testing.T) {
	// Currently falls back to truncateOldest
	policy := &ContextBuilderPolicy{
		TokenBudget:      30,
		ReserveForOutput: 5,
		Strategy:         TruncateLeastRelevant,
	}
	middleware := ContextBuilderMiddleware(policy)
	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "system",
		Messages: []types.Message{
			{Role: "user", Content: "message one with lots of words to exceed token budget and force truncation"},
			{Role: "assistant", Content: "response with additional content"},
			{Role: "user", Content: "another message with more content to ensure we exceed the budget"},
			{Role: "assistant", Content: "another response"},
		},
		Metadata: make(map[string]interface{}),
	}

	err := middleware.Process(execCtx, func() error { return nil })

	assert.NoError(t, err)
	assert.Less(t, len(execCtx.Messages), 4, "Should truncate with relevance strategy")
}

func TestContextBuilderMiddleware_TruncateSummarize(t *testing.T) {
	// Currently falls back to truncateOldest
	policy := &ContextBuilderPolicy{
		TokenBudget:      30,
		ReserveForOutput: 5,
		Strategy:         TruncateSummarize,
	}
	middleware := ContextBuilderMiddleware(policy)
	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "system",
		Messages: []types.Message{
			{Role: "user", Content: "message one with lots of words to exceed token budget and force truncation"},
			{Role: "assistant", Content: "response with additional content"},
			{Role: "user", Content: "another message with more content to ensure we exceed the budget"},
			{Role: "assistant", Content: "another response"},
		},
		Metadata: make(map[string]interface{}),
	}

	err := middleware.Process(execCtx, func() error { return nil })

	assert.NoError(t, err)
	assert.Less(t, len(execCtx.Messages), 4, "Should truncate with summarize strategy")
}

func TestContextBuilderMiddleware_BudgetTooSmallForSystem(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget:      10, // Too small for system prompt
		ReserveForOutput: 5,
		Strategy:         TruncateOldest,
	}
	middleware := ContextBuilderMiddleware(policy)
	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "This is a very long system prompt with many many words that exceed the budget",
		Messages:     []types.Message{},
		Metadata:     make(map[string]interface{}),
	}

	err := middleware.Process(execCtx, func() error { return nil })

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token budget too small")
}

func TestContextBuilderMiddleware_CacheBreakpoints(t *testing.T) {
	// Cache breakpoints flag is only set when truncation logic runs (budget exceeded)
	policy := &ContextBuilderPolicy{
		TokenBudget:      25, // Very small budget to force truncation
		ReserveForOutput: 5,
		Strategy:         TruncateOldest,
		CacheBreakpoints: true,
	}
	middleware := ContextBuilderMiddleware(policy)
	execCtx := &pipeline.ExecutionContext{
		SystemPrompt: "s",
		Messages: []types.Message{
			{Role: "user", Content: "this is a very long message with many words that will definitely exceed the token budget and force truncation to occur"},
			{Role: "assistant", Content: "this is also a long response with many words to ensure we exceed budget"},
			{Role: "user", Content: "another message with lots of content"},
			{Role: "assistant", Content: "final response"},
		},
		Metadata: make(map[string]interface{}),
	}

	err := middleware.Process(execCtx, func() error { return nil })

	assert.NoError(t, err)
	// Should have truncated
	assert.Less(t, len(execCtx.Messages), 4)

	cacheBreakpoints, ok := execCtx.Metadata["enable_cache_breakpoints"]
	if assert.True(t, ok, "enable_cache_breakpoints should be set when truncation runs") {
		assert.True(t, cacheBreakpoints.(bool))
	}
}

func TestContextBuilderMiddleware_NextError(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget: 1000,
		Strategy:    TruncateOldest,
	}
	middleware := ContextBuilderMiddleware(policy)
	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "test"},
		},
		Metadata: make(map[string]interface{}),
	}

	expectedErr := errors.New("next middleware error")
	err := middleware.Process(execCtx, func() error {
		return expectedErr
	})

	assert.Equal(t, expectedErr, err)
}

func TestContextBuilderMiddleware_StreamChunk(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget: 1000,
		Strategy:    TruncateOldest,
	}
	middleware := ContextBuilderMiddleware(policy)
	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
	}
	chunk := &providers.StreamChunk{
		Content: "test chunk",
	}

	err := middleware.StreamChunk(execCtx, chunk)

	assert.NoError(t, err) // StreamChunk is a no-op
}

func TestCountTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		minCount int
		maxCount int
	}{
		{
			name:     "empty string",
			text:     "",
			minCount: 0,
			maxCount: 0,
		},
		{
			name:     "single word",
			text:     "hello",
			minCount: 1,
			maxCount: 2,
		},
		{
			name:     "multiple words",
			text:     "hello world this is a test",
			minCount: 6,
			maxCount: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := countTokens(tt.text)
			assert.GreaterOrEqual(t, count, tt.minCount)
			assert.LessOrEqual(t, count, tt.maxCount)
		})
	}
}

func TestCountMessagesTokens(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "hello world"},
		{Role: "assistant", Content: "hi there"},
		{
			Role:    "assistant",
			Content: "calling tool",
			ToolCalls: []types.MessageToolCall{
				{Name: "search", Args: []byte(`{"query":"test"}`)},
			},
		},
	}

	count := countMessagesTokens(messages)
	assert.Greater(t, count, 0)
	assert.Greater(t, count, countTokens("hello world")+countTokens("hi there"))
}

func TestTruncateOldest(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "message 1"},
		{Role: "assistant", Content: "response 1"},
		{Role: "user", Content: "message 2"},
		{Role: "assistant", Content: "response 2"},
	}

	t.Run("keeps recent messages", func(t *testing.T) {
		result := truncateOldest(messages, 20)
		require.NotEmpty(t, result)
		// Most recent should be kept
		assert.Equal(t, "response 2", result[len(result)-1].Content)
	})

	t.Run("drops oldest when over budget", func(t *testing.T) {
		result := truncateOldest(messages, 5)
		assert.Less(t, len(result), len(messages))
		if len(result) > 0 {
			assert.Equal(t, "response 2", result[len(result)-1].Content)
		}
	})

	t.Run("returns empty for very small budget", func(t *testing.T) {
		result := truncateOldest(messages, 1)
		assert.Empty(t, result)
	})
}

func TestTruncateMessages(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "test"},
	}

	t.Run("oldest strategy", func(t *testing.T) {
		result, err := truncateMessages(messages, 100, TruncateOldest)
		assert.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("least relevant strategy", func(t *testing.T) {
		result, err := truncateMessages(messages, 100, TruncateLeastRelevant)
		assert.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("summarize strategy", func(t *testing.T) {
		result, err := truncateMessages(messages, 100, TruncateSummarize)
		assert.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("fail strategy over budget", func(t *testing.T) {
		result, err := truncateMessages(messages, 1, TruncateFail)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "token budget exceeded")
	})

	t.Run("default strategy", func(t *testing.T) {
		result, err := truncateMessages(messages, 100, "unknown")
		assert.NoError(t, err)
		assert.NotEmpty(t, result) // Falls back to oldest
	})
}

func TestGetContextMetadata(t *testing.T) {
	t.Run("no truncation", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Metadata: make(map[string]interface{}),
		}
		truncated, origCount, truncCount := GetContextMetadata(execCtx)
		assert.False(t, truncated)
		assert.Equal(t, 0, origCount)
		assert.Equal(t, 0, truncCount)
	})

	t.Run("with truncation", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Metadata: map[string]interface{}{
				"context_truncated":       true,
				"context_original_count":  10,
				"context_truncated_count": 5,
			},
		}
		truncated, origCount, truncCount := GetContextMetadata(execCtx)
		assert.True(t, truncated)
		assert.Equal(t, 10, origCount)
		assert.Equal(t, 5, truncCount)
	})

	t.Run("truncation false", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Metadata: map[string]interface{}{
				"context_truncated": false,
			},
		}
		truncated, origCount, truncCount := GetContextMetadata(execCtx)
		assert.False(t, truncated)
		assert.Equal(t, 0, origCount)
		assert.Equal(t, 0, truncCount)
	})
}

func TestTruncateOldestWithToolCalls(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "search for something with lots of words"},
		{
			Role:    "assistant",
			Content: "searching with additional words",
			ToolCalls: []types.MessageToolCall{
				{Name: "search", Args: []byte(`{"query":"something very long that takes many tokens and adds to the count"}`)},
			},
		},
		{Role: "tool", Content: "results from the search with more content"},
		{Role: "assistant", Content: "here are the results with detailed explanation"},
	}

	result := truncateOldest(messages, 15) // Very small budget

	// Should keep most recent messages and truncate
	assert.Less(t, len(result), len(messages), "Should truncate messages with tool calls")

	// Most recent should be kept if any remain
	if len(result) > 0 {
		assert.Equal(t, "here are the results with detailed explanation", result[len(result)-1].Content)
	}
}
