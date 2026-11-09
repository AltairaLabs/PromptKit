package stage

import (
	"context"
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
	s := NewTokenBudgetStage(&TokenBudgetConfig{
		MaxTokens:    maxTokens,
		TokenCounter: tokenizer.NewHeuristicTokenCounterWithRatio(1.0),
	})
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
// sends, and a system prompt that fills the budget on its own.
func TestTokenBudgetStage_CurrentTurnThatDoesNotFitFails(t *testing.T) {
	got, err := runTokenBudget(t, 20,
		turnMsg(roleUser, "q0 "+strings.Repeat("w ", 20)),
		turnCall(1, "c1"),
		turnResult("c1", "found it"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too small for the current turn")
	assert.Empty(t, got, "nothing is sent")

	got, err = runTokenBudget(t, 10,
		turnMsg(roleSystem, "You are a very helpful assistant with many long instructions"),
		turnMsg(roleUser, "Hello"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too small for the current turn")
	assert.Empty(t, got, "the system prompt alone is not sent")
}
