package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// turnWordCounter counts one token per word, so budgets in these tests are
// exact.
type turnWordCounter struct{}

func (turnWordCounter) CountTokens(text string) int { return len(strings.Fields(text)) }

func (c turnWordCounter) CountMultiple(texts []string) int {
	n := 0
	for _, t := range texts {
		n += c.CountTokens(t)
	}
	return n
}

func turnMsg(role, text string) types.Message { return types.Message{Role: role, Content: text} }

// turnCall is an assistant message calling tools with the given IDs; each
// call's args cost argWords tokens.
func turnCall(argWords int, ids ...string) types.Message {
	m := types.Message{Role: roleAssistant}
	for _, id := range ids {
		args, _ := json.Marshal(map[string]string{"q": strings.TrimSpace(strings.Repeat("w ", argWords))})
		m.ToolCalls = append(m.ToolCalls, types.MessageToolCall{ID: id, Name: "lookup", Args: args})
	}
	return m
}

func turnResult(id, text string) types.Message {
	r := types.NewTextToolResult(id, "lookup", text)
	return types.Message{Role: roleTool, Content: text, ToolResult: &r}
}

// runTurnBoundary sends messages through a context builder and returns what
// it emits, one "role:content" (or "role:calls=ids") per message.
func runTurnBoundary(t *testing.T, policy *ContextBuilderPolicy, msgs ...types.Message) []string {
	t.Helper()
	policy.TokenCounter = turnWordCounter{}
	s := NewContextBuilderStage(policy)
	input := make(chan StreamElement, len(msgs))
	for i := range msgs {
		input <- NewMessageElement(&msgs[i])
	}
	close(input)
	output := make(chan StreamElement, len(msgs)+1)
	require.NoError(t, s.Process(context.Background(), input, output))
	var got []string
	for e := range output {
		if e.Message == nil {
			continue
		}
		if len(e.Message.ToolCalls) > 0 {
			var ids []string
			for _, tc := range e.Message.ToolCalls {
				ids = append(ids, tc.ID)
			}
			got = append(got, fmt.Sprintf("%s:calls=%s", e.Message.Role, strings.Join(ids, ",")))
			continue
		}
		got = append(got, e.Message.Role+":"+e.Message.Content)
	}
	return got
}

// Sliding truncation keeps whole turns: the kept history opens on a user
// message, never on the reply to a question that was dropped.
func TestContextBuilder_SlidingKeepsWholeTurns(t *testing.T) {
	got := runTurnBoundary(t, &ContextBuilderPolicy{TokenBudget: 9, Strategy: TruncateOldest},
		turnMsg(roleUser, "q0 w w w w w w w"),
		turnMsg(roleAssistant, "a0 w"),
		turnMsg(roleUser, "q1 w"),
		turnMsg(roleAssistant, "a1 w"),
		turnMsg(roleUser, "q2 w"),
	)
	assert.Equal(t, []string{"user:q1 w", "assistant:a1 w", "user:q2 w"}, got)
}

// Sliding truncation never sends a tool result without the call it answers:
// every provider rejects that request.
func TestContextBuilder_SlidingNeverOrphansAToolResult(t *testing.T) {
	got := runTurnBoundary(t, &ContextBuilderPolicy{TokenBudget: 6, Strategy: TruncateOldest},
		turnMsg(roleUser, "look it up"),
		turnCall(20, "c1"),
		turnResult("c1", "found it"),
		turnMsg(roleAssistant, "here it is"),
		turnMsg(roleUser, "thanks"),
	)
	assert.Equal(t, []string{"user:thanks"}, got,
		"the budget fits the result but not its call, so the turn goes whole")

	got = runTurnBoundary(t, &ContextBuilderPolicy{TokenBudget: 40, Strategy: TruncateOldest},
		turnMsg(roleUser, "look it up"),
		turnCall(20, "c1"),
		turnResult("c1", "found it"),
		turnMsg(roleAssistant, "here it is"),
		turnMsg(roleUser, "thanks"),
	)
	assert.Equal(t, []string{"user:look it up", "assistant:calls=c1", "tool:found it",
		"assistant:here it is", "user:thanks"}, got, "a turn that fits keeps its call and result together")
}

// Summarizing folds the replies that open the kept tail into the summary, so
// the kept messages start on a user turn and nothing is silently lost.
func TestContextBuilder_SummarizeSplitsOnATurn(t *testing.T) {
	summarizer := &recordingSummarizer{}
	got := runTurnBoundary(t,
		&ContextBuilderPolicy{TokenBudget: 80, Strategy: TruncateSummarize, Summarizer: summarizer},
		turnMsg(roleUser, "q0 "+strings.Repeat("w ", 79)),
		turnMsg(roleAssistant, "a0 w w w w"),
		turnMsg(roleUser, "q1 w w w w"),
	)
	assert.Equal(t, []string{"system:[Summary of 2 earlier messages]: summary", "user:q1 w w w w"}, got)
	require.Len(t, summarizer.batch, 2)
	assert.Equal(t, "a0 w w w w", summarizer.batch[1].Content, "the reply is summarized, not dropped")

	// With no user message in the recent tail, everything is summarized.
	got = runTurnBoundary(t,
		&ContextBuilderPolicy{TokenBudget: 80, Strategy: TruncateSummarize, Summarizer: summarizer},
		turnMsg(roleUser, "q0 "+strings.Repeat("w ", 79)),
		turnMsg(roleAssistant, "a0 w w w w"),
	)
	assert.Equal(t, []string{"system:[Summary of 2 earlier messages]: summary"}, got)
}

// Relevance truncation picks messages one by one, so it can keep half of a
// tool exchange. The emitted history drops the other half.
func TestContextBuilder_RelevanceKeepsToolExchangesWhole(t *testing.T) {
	topic := newMockEmbeddingProvider()
	topic.embedFunc = func(texts []string) ([][]float32, error) {
		out := make([][]float32, len(texts))
		for i, x := range texts {
			out[i] = []float32{0, 1, 0}
			if strings.Contains(x, "apples") {
				out[i] = []float32{1, 0, 0}
			}
		}
		return out, nil
	}
	relevance := func(budget int) *ContextBuilderPolicy {
		return &ContextBuilderPolicy{TokenBudget: budget, Strategy: TruncateLeastRelevant,
			RelevanceConfig: &RelevanceConfig{EmbeddingProvider: topic, MinRecentMessages: 1}}
	}

	// The result is relevant and fits; its call is neither.
	got := runTurnBoundary(t, relevance(6),
		turnMsg(roleUser, "apples please"),
		turnCall(1, "c1"),
		turnResult("c1", "apples found"),
		turnMsg(roleAssistant, "pears here"),
		turnMsg(roleUser, "apples again"),
	)
	assert.Equal(t, []string{"user:apples please", "user:apples again"}, got, "no result without its call")

	// The call is relevant and fits; its result is neither.
	callWithText := turnCall(1, "c1")
	callWithText.Content = "apples lookup"
	got = runTurnBoundary(t, relevance(6),
		turnMsg(roleUser, "apples q"),
		callWithText,
		turnResult("c1", "pears"),
		turnMsg(roleAssistant, "pears"),
		turnMsg(roleUser, "apples"),
	)
	assert.Equal(t, []string{"user:apples q", "user:apples"}, got, "no call without its result")
}
