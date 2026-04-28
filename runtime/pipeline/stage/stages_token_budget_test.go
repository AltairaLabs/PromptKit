package stage

import (
	"context"
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tokenizer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TokenBudgetStage Tests
// =============================================================================

func TestTokenBudgetStage_NoBudget_PassThrough(t *testing.T) {
	s := NewTokenBudgetStage(&TokenBudgetConfig{})

	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
		newTestMsgElement("assistant", "Hi there!"),
		newTestMsgElement("user", "How are you?"),
	}

	results := runTestStage(t, s, inputs)
	require.Len(t, results, 3)
	assert.Equal(t, "Hello", results[0].Message.Content)
	assert.Equal(t, "Hi there!", results[1].Message.Content)
	assert.Equal(t, "How are you?", results[2].Message.Content)
}

func TestTokenBudgetStage_NilConfig(t *testing.T) {
	s := NewTokenBudgetStage(nil)

	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
	}

	results := runTestStage(t, s, inputs)
	require.Len(t, results, 1)
	assert.Equal(t, "Hello", results[0].Message.Content)
}

func TestTokenBudgetStage_WithinBudget(t *testing.T) {
	// Use a generous budget that fits all messages
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:    100000,
		TokenCounter: tokenizer.NewHeuristicTokenCounter(tokenizer.ModelFamilyDefault),
	})

	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
		newTestMsgElement("assistant", "Hi!"),
	}

	results := runTestStage(t, s, inputs)
	require.Len(t, results, 2)
	assert.Equal(t, "Hello", results[0].Message.Content)
	assert.Equal(t, "Hi!", results[1].Message.Content)
}

func TestTokenBudgetStage_ExceedsBudget_Truncates(t *testing.T) {
	// Use a very small budget that forces truncation.
	// Each message has ~4 overhead + ~1-2 content tokens.
	// With budget of 15 tokens, only the most recent messages should fit.
	counter := tokenizer.NewHeuristicTokenCounterWithRatio(1.0)
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:    15,
		TokenCounter: counter,
	})

	inputs := make([]StreamElement, 10)
	for i := range inputs {
		inputs[i] = newTestMsgElement("user", fmt.Sprintf("Message %d content", i))
	}

	results := runTestStage(t, s, inputs)

	// Should have fewer messages than input
	require.Less(t, len(results), 10)
	require.Greater(t, len(results), 0)

	// The last message in results should be the last input message
	lastResult := results[len(results)-1]
	assert.Equal(t, "Message 9 content", lastResult.Message.Content)
}

func TestTokenBudgetStage_PreservesSystemMessages(t *testing.T) {
	counter := tokenizer.NewHeuristicTokenCounterWithRatio(1.0)
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:    30,
		TokenCounter: counter,
	})

	inputs := []StreamElement{
		newTestMsgElement("system", "You are a helpful assistant"),
		newTestMsgElement("user", "Message 1 with some content"),
		newTestMsgElement("assistant", "Response 1 with some content"),
		newTestMsgElement("user", "Message 2 with some content"),
		newTestMsgElement("assistant", "Response 2 with some content"),
		newTestMsgElement("user", "Message 3 with some content"),
	}

	results := runTestStage(t, s, inputs)

	// System message should always be first
	require.Greater(t, len(results), 0)
	assert.Equal(t, "system", results[0].Message.Role)
	assert.Equal(t, "You are a helpful assistant", results[0].Message.Content)

	// Should have truncated some messages
	require.Less(t, len(results), 6)
}

func TestTokenBudgetStage_ReserveTokens(t *testing.T) {
	counter := tokenizer.NewHeuristicTokenCounterWithRatio(1.0)

	// Without reserve
	s1 := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:    50,
		TokenCounter: counter,
	})

	// With large reserve — should allow fewer messages
	s2 := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:     50,
		ReserveTokens: 30,
		TokenCounter:  counter,
	})

	inputs := make([]StreamElement, 10)
	for i := range inputs {
		inputs[i] = newTestMsgElement("user", fmt.Sprintf("Message %d", i))
	}

	results1 := runTestStage(t, s1, inputs)
	results2 := runTestStage(t, s2, inputs)

	// With reserve, fewer messages should fit
	assert.Less(t, len(results2), len(results1))
}

func TestTokenBudgetStage_ForwardsNonMessageElements(t *testing.T) {
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:    100000,
		TokenCounter: tokenizer.NewHeuristicTokenCounter(tokenizer.ModelFamilyDefault),
	})

	text := "some text"
	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
		{
			Text: &text,
		},
	}

	results := runTestStage(t, s, inputs)
	require.Len(t, results, 2)

	// Message comes first, then non-message element
	assert.NotNil(t, results[0].Message)
	assert.Equal(t, "Hello", results[0].Message.Content)
	assert.NotNil(t, results[1].Text)
	assert.Equal(t, "some text", *results[1].Text)
}

func TestTokenBudgetStage_SystemPromptFromTurnState(t *testing.T) {
	counter := tokenizer.NewHeuristicTokenCounterWithRatio(1.0)
	turnState := NewTurnState()
	turnState.SystemPrompt = "You are a very helpful assistant with a long system prompt"
	s := NewTokenBudgetStageWithTurnState(&TokenBudgetConfig{
		MaxTokens:    30,
		TokenCounter: counter,
	}, turnState)

	inputs := []StreamElement{
		newTestMsgElement("user", "Message 1 content here"),
		newTestMsgElement("user", "Message 2 content here"),
		newTestMsgElement("user", "Message 3 content here"),
	}

	results := runTestStage(t, s, inputs)

	// Should have some messages truncated due to system prompt overhead
	require.Greater(t, len(results), 0)
}

func TestTokenBudgetStage_ZeroEffectiveBudget(t *testing.T) {
	// ReserveTokens >= MaxTokens means effective budget is 0
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:     10,
		ReserveTokens: 15,
		TokenCounter:  tokenizer.NewHeuristicTokenCounter(tokenizer.ModelFamilyDefault),
	})

	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
	}

	results := runTestStage(t, s, inputs)
	// Should pass through when effective budget <= 0
	require.Len(t, results, 1)
	assert.Equal(t, "Hello", results[0].Message.Content)
}

func TestTokenBudgetStage_SystemMessagesExceedBudget(t *testing.T) {
	counter := tokenizer.NewHeuristicTokenCounterWithRatio(1.0)
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:    10,
		TokenCounter: counter,
	})

	// System message that by itself exceeds the budget
	inputs := []StreamElement{
		newTestMsgElement("system",
			"You are a very helpful assistant with many instructions "+
				"that are very long and take up a lot of tokens in the context window"),
		newTestMsgElement("user", "Hello"),
	}

	results := runTestStage(t, s, inputs)
	// When system messages exceed budget, they're still returned
	require.Greater(t, len(results), 0)
	assert.Equal(t, "system", results[0].Message.Role)
}

func TestTokenBudgetStage_ContextCancellation(t *testing.T) {
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:    100000,
		TokenCounter: tokenizer.NewHeuristicTokenCounter(tokenizer.ModelFamilyDefault),
	})

	ctx, cancel := context.WithCancel(context.Background())

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement) // unbuffered — blocks on send

	// Send messages
	input <- newTestMsgElement("user", "test1")
	input <- newTestMsgElement("user", "test2")
	close(input)

	// Cancel context before Process tries to write
	cancel()

	err := s.Process(ctx, input, output)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestTokenBudgetStage_LargeConversationWarning(t *testing.T) {
	// With no budget and many messages, should just warn (not truncate)
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		LargeConversationThreshold: 5,
	})

	inputs := make([]StreamElement, 10)
	for i := range inputs {
		inputs[i] = newTestMsgElement("user", fmt.Sprintf("Message %d", i))
	}

	results := runTestStage(t, s, inputs)
	// All messages pass through since no budget is set
	require.Len(t, results, 10)
}

func TestTokenBudgetStage_DefaultLargeConversationThreshold(t *testing.T) {
	s := NewTokenBudgetStage(&TokenBudgetConfig{})
	assert.Equal(t, defaultLargeConversationThreshold,
		s.config.LargeConversationThreshold)
}

func TestTokenBudgetStage_DefaultTokenCounter(t *testing.T) {
	s := NewTokenBudgetStage(&TokenBudgetConfig{})
	assert.NotNil(t, s.config.TokenCounter)
}

func TestTokenBudgetStage_OnlyConversationMessages(t *testing.T) {
	// No system messages — should still truncate correctly
	counter := tokenizer.NewHeuristicTokenCounterWithRatio(1.0)
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:    20,
		TokenCounter: counter,
	})

	inputs := make([]StreamElement, 10)
	for i := range inputs {
		inputs[i] = newTestMsgElement("user", fmt.Sprintf("Message %d with content", i))
	}

	results := runTestStage(t, s, inputs)
	require.Less(t, len(results), 10)
	require.Greater(t, len(results), 0)
	// Most recent message should be preserved
	assert.Contains(t, results[len(results)-1].Message.Content, "Message 9")
}

func TestTokenBudgetStage_EmptyInput(t *testing.T) {
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens: 1000,
	})

	results := runTestStage(t, s, nil)
	require.Len(t, results, 0)
}

func TestTokenBudgetStage_StageMetadata(t *testing.T) {
	s := NewTokenBudgetStage(nil)
	assert.Equal(t, "token_budget", s.Name())
	assert.Equal(t, StageTypeTransform, s.Type())
}
