package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tokenizer"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// runTokenBudget sends messages through a TokenBudgetStage with a budget of
// maxTokens, counting one token per word plus the counter's 4-token
// per-message overhead, and returns what it emits as "role:content" (or
// "role:calls=ids").
func runTokenBudget(t *testing.T, maxTokens int, msgs ...types.Message) ([]string, error) {
	t.Helper()
	return runTokenBudgetStage(t, wordBudgetStage(maxTokens, nil), nil, msgs...)
}

// wordBudgetStage is a TokenBudgetStage counting one token per word (plus
// the per-message overhead), reading the system prompt from turnState.
func wordBudgetStage(maxTokens int, turnState *TurnState) *TokenBudgetStage {
	return NewTokenBudgetStageWithTurnState(&TokenBudgetConfig{
		MaxTokens:    maxTokens,
		TokenCounter: tokenizer.NewHeuristicTokenCounterWithRatio(1.0),
	}, turnState)
}

// runTokenBudgetStage sends msgs, then extra, through s. Non-message
// elements it emits come back as "error:<message>".
func runTokenBudgetStage(
	t *testing.T, s *TokenBudgetStage, extra []StreamElement, msgs ...types.Message,
) ([]string, error) {
	t.Helper()
	input := make(chan StreamElement, len(msgs)+len(extra))
	for i := range msgs {
		input <- NewMessageElement(&msgs[i])
	}
	for _, e := range extra {
		input <- e
	}
	close(input)
	output := make(chan StreamElement, len(msgs)+len(extra)+1)
	err := s.Process(context.Background(), input, output)
	var got []string
	for e := range output {
		if e.Message == nil {
			if e.Error != nil {
				got = append(got, "error:"+e.Error.Error())
			}
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

// The budget fits the tool result and what follows, but not the call with
// its large args. The stage drops the whole exchange: a result without its
// call is a 400 on every provider.
func TestTokenBudgetStage_NeverOrphansAToolResult(t *testing.T) {
	got, err := runTokenBudget(t, 20,
		turnMsg(roleUser, "look it up"),
		turnCall(20, "c1"),
		turnResult("c1", "found it"),
		turnMsg(roleAssistant, "here it is"),
		turnMsg(roleUser, "thanks"),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"user:thanks"}, got)
}

// The kept history opens on a user message, never on the reply to a
// question that was dropped.
func TestTokenBudgetStage_KeepsWholeTurns(t *testing.T) {
	got, err := runTokenBudget(t, 14,
		turnMsg(roleUser, "q0 "+strings.Repeat("w ", 19)),
		turnMsg(roleAssistant, "a0 w"),
		turnMsg(roleUser, "q1 w"),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"user:q1 w"}, got)
}

// When no user message fits, the stage fails with a reason rather than send
// a request with no question in it: the shape a Resume with tool results
// sends. (A system message that fills the budget is
// TestTokenBudgetStage_SystemMessagesExceedBudget.)
func TestTokenBudgetStage_CurrentTurnThatDoesNotFitFails(t *testing.T) {
	got, err := runTokenBudget(t, 20,
		turnMsg(roleUser, "q0 "+strings.Repeat("w ", 20)),
		turnCall(1, "c1"),
		turnResult("c1", "found it"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too small for the current turn")
	assert.Empty(t, got, "nothing is sent")
}

// A current turn whose tool call and results fit comes back whole: only the
// older turn is dropped.
func TestTokenBudgetStage_KeepsAToolExchangeThatFits(t *testing.T) {
	got, err := runTokenBudget(t, 25,
		turnMsg(roleUser, "q0 "+strings.Repeat("w ", 20)),
		turnMsg(roleUser, "q1"),
		turnCall(1, "c1"),
		turnResult("c1", "found it"),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"user:q1", "assistant:calls=c1", "tool:found it"}, got)
}

// A history with no user message has no turn to open on: the stage keeps
// what fits, pairs its tool exchanges, and fails only when nothing but system
// messages would be sent.
func TestTokenBudgetStage_HistoryWithNoUserMessage(t *testing.T) {
	got, err := runTokenBudget(t, 8,
		turnMsg(roleAssistant, "a0 "+strings.Repeat("w ", 19)),
		turnMsg(roleAssistant, "a1 w"),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"assistant:a1 w"}, got)

	got, err = runTokenBudget(t, 12,
		turnMsg(roleSystem, "sys"),
		turnMsg(roleAssistant, "a0 "+strings.Repeat("w ", 19)),
		turnMsg(roleAssistant, "a1 w"),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"system:sys", "assistant:a1 w"}, got)

	got, err = runTokenBudget(t, 8,
		turnMsg(roleAssistant, "a0 "+strings.Repeat("w ", 19)),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too small for the current turn")
	assert.Empty(t, got)
}

// A system prompt that fills the budget on its own leaves no room for the
// user's message, so the stage fails.
func TestTokenBudgetStage_SystemPromptFillsTheBudget(t *testing.T) {
	ts := NewTurnState()
	ts.SystemPrompt = strings.TrimSpace(strings.Repeat("rule ", 15))
	got, err := runTokenBudgetStage(t, wordBudgetStage(12, ts), nil, turnMsg(roleUser, "hi"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too small for the current turn")
	assert.Empty(t, got)
}

// When an upstream stage failed, the turn and its failure signal pass on
// untouched: the stage does not truncate, or fail with an error of its own.
func TestTokenBudgetStage_PassesAnUpstreamFailureThrough(t *testing.T) {
	long := "q0 " + strings.Repeat("w ", 20)
	got, err := runTokenBudgetStage(t, wordBudgetStage(8, nil),
		[]StreamElement{newTurnFailureSignal(errors.New("upstream broke"))},
		turnMsg(roleUser, long),
		turnMsg(roleUser, "q1"),
	)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"user:" + long, "user:q1"}, got[:2])
	assert.Contains(t, got[2], "upstream broke")
}
