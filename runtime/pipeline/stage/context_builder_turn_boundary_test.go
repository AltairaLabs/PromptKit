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
	got, err := runTurnBoundaryErr(t, policy, msgs...)
	require.NoError(t, err)
	return got
}

// runTurnBoundaryErr is runTurnBoundary for a run that may fail.
func runTurnBoundaryErr(t *testing.T, policy *ContextBuilderPolicy, msgs ...types.Message) ([]string, error) {
	t.Helper()
	policy.TokenCounter = turnWordCounter{}
	s := NewContextBuilderStage(policy)
	input := make(chan StreamElement, len(msgs))
	for i := range msgs {
		input <- NewMessageElement(&msgs[i])
	}
	close(input)
	output := make(chan StreamElement, len(msgs)+1)
	err := s.Process(context.Background(), input, output)
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
	return got, err
}

// When the budget cannot fit the current turn's user message, the stage
// fails with a reason rather than send a request with no question in it.
// This is the shape a Resume with tool results sends.
func TestContextBuilder_CurrentTurnThatDoesNotFitFails(t *testing.T) {
	got, err := runTurnBoundaryErr(t, &ContextBuilderPolicy{TokenBudget: 6, Strategy: TruncateOldest},
		turnMsg(roleUser, "q0 "+strings.Repeat("w ", 20)),
		turnCall(1, "c1"),
		turnResult("c1", "found it"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too small for the current turn")
	assert.Empty(t, got, "nothing is sent")
}

// A system message ahead of the first user message is kept; a call dropped
// because it came before the first user message does not keep a later result
// that reuses its (empty) ID.
func TestContextBuilder_OpeningPassRunsBeforePairing(t *testing.T) {
	got := runTurnBoundary(t, &ContextBuilderPolicy{TokenBudget: 12, Strategy: TruncateOldest},
		turnMsg(roleUser, "q0 "+strings.Repeat("w ", 20)),
		turnMsg(roleSystem, "be brief"),
		turnCall(1, ""),
		turnMsg(roleUser, "q1"),
		turnResult("", "r1"),
		turnMsg(roleUser, "q2"),
	)
	assert.Equal(t, []string{"system:be brief", "user:q1", "user:q2"}, got)
}

// A message with parallel tool calls is kept only when every result is.
func TestContextBuilder_ParallelCallsNeedEveryResult(t *testing.T) {
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
	got := runTurnBoundary(t, &ContextBuilderPolicy{TokenBudget: 9, Strategy: TruncateLeastRelevant,
		RelevanceConfig: &RelevanceConfig{EmbeddingProvider: topic, MinRecentMessages: 1}},
		turnMsg(roleUser, "apples please"),
		turnCall(1, "c1", "c2"),
		turnResult("c1", "pears w w w w w w"),
		turnResult("c2", "apples found"),
		turnMsg(roleUser, "apples again"),
	)
	assert.Equal(t, []string{"user:apples please", "user:apples again"}, got,
		"c1's result did not fit, so the call and c2's result go too")
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

	// With no user message in the recent tail, a summary alone would ask the
	// model nothing, so the stage fails instead.
	_, err := runTurnBoundaryErr(t,
		&ContextBuilderPolicy{TokenBudget: 80, Strategy: TruncateSummarize, Summarizer: summarizer},
		turnMsg(roleUser, "q0 "+strings.Repeat("w ", 79)),
		turnMsg(roleAssistant, "a0 w w w w"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too small for the current turn")
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
