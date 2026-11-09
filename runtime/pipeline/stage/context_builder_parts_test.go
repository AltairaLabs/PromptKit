package stage

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// partsMessage is a message whose text lives in Parts, as a Send builds it.
func partsMessage(role, text string) *types.Message {
	m := &types.Message{Role: role}
	m.AddTextPart(text)
	return m
}

// runContextBuilder sends four 10-word messages, text in Parts, through a
// context builder with the given strategy and a budget that fits two.
func runContextBuilder(t *testing.T, strategy TruncationStrategy) ([]string, error) {
	t.Helper()
	s := NewContextBuilderStage(&ContextBuilderPolicy{TokenBudget: 30, Strategy: strategy})
	input := make(chan StreamElement, 4)
	for i := 0; i < 4; i++ {
		input <- NewMessageElement(partsMessage("user", fmt.Sprintf("m%d %s", i, strings.Repeat("w ", 9))))
	}
	close(input)
	output := make(chan StreamElement, 8)
	err := s.Process(context.Background(), input, output)
	var got []string
	for e := range output {
		if e.Message != nil {
			got = append(got, strings.Fields(e.Message.GetContent())[0])
		}
	}
	return got, err
}

// A message's tokens are its text wherever it is kept: text in Parts counts
// against the budget like Content does.
func TestContextBuilderStage_CountsTextInParts(t *testing.T) {
	got, err := runContextBuilder(t, TruncateOldest)
	require.NoError(t, err)
	assert.Equal(t, []string{"m2", "m3"}, got, "the oldest go first")

	_, err = runContextBuilder(t, TruncateFail)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token budget exceeded")

	got, err = runContextBuilder(t, TruncateSummarize)
	require.NoError(t, err)
	assert.Equal(t, []string{"m2", "m3"}, got, "with no summarizer, summarize drops the oldest")
}
