package stages

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/stretchr/testify/assert"
)

// textsOf collects the Text content of every Text element in order.
func textsOf(results []stage.StreamElement) []string {
	var out []string
	for i := range results {
		if results[i].Text != nil {
			out = append(out, *results[i].Text)
		}
	}
	return out
}

func TestTTSSentenceAggregator_StreamingDeltasFormSentences(t *testing.T) {
	s := NewTTSSentenceAggregatorStage()

	// Token-by-token deltas (as a streaming provider emits them), then the
	// duplicate complete assistant Message, then the turn boundary.
	inputs := []stage.StreamElement{
		stage.NewTextElement("Hello there, how are you?"),
		stage.NewTextElement(" I am doing well."),
		stage.NewTextElement(" And you"),
		newTestMessageElement("assistant", "Hello there, how are you? I am doing well. And you"),
		stage.NewEndOfTurnElement(),
	}

	results := runStage(t, s, inputs)

	// TTS should receive complete sentences, not per-token fragments, and the
	// duplicate assistant Message must NOT be forwarded.
	assert.Equal(t, []string{
		"Hello there, how are you?",
		"I am doing well.",
		"And you",
	}, textsOf(results))

	for i := range results {
		assert.Nil(t, results[i].Message, "the duplicate assistant Message must be dropped (deltas covered it)")
	}

	// The EndOfTurn boundary is forwarded.
	var sawEndOfTurn bool
	for i := range results {
		if results[i].EndOfTurn {
			sawEndOfTurn = true
		}
	}
	assert.True(t, sawEndOfTurn, "EndOfTurn must be forwarded")
}

func TestTTSSentenceAggregator_NonStreamingMessageSplit(t *testing.T) {
	s := NewTTSSentenceAggregatorStage()

	// A non-streaming provider emits only the complete Message (no deltas).
	inputs := []stage.StreamElement{
		newTestMessageElement("assistant", "First sentence here. Second one follows! Third?"),
		stage.NewEndOfTurnElement(),
	}

	results := runStage(t, s, inputs)

	assert.Equal(t, []string{
		"First sentence here.",
		"Second one follows!",
		"Third?",
	}, textsOf(results))
}

func TestTTSSentenceAggregator_InterruptClearsBuffer(t *testing.T) {
	s := NewTTSSentenceAggregatorStage()

	inputs := []stage.StreamElement{
		stage.NewTextElement("I was about to say something long"), // partial, no terminator
		stage.NewInterruptElement(),
	}

	results := runStage(t, s, inputs)

	// The half-spoken reply is discarded on barge-in; only the Interrupt flows.
	assert.Empty(t, textsOf(results), "a barge-in must discard the buffered partial reply")
	var sawInterrupt bool
	for i := range results {
		if results[i].Interrupt {
			sawInterrupt = true
		}
	}
	assert.True(t, sawInterrupt, "Interrupt must be forwarded")
}

func TestSplitSentences(t *testing.T) {
	t.Run("splits on terminator + whitespace, holds the tail", func(t *testing.T) {
		got, rest := splitSentences("One sentence here. Two sentence here. Tail", false)
		assert.Equal(t, []string{"One sentence here.", "Two sentence here."}, got)
		assert.Equal(t, " Tail", rest)
	})

	t.Run("does not split a decimal", func(t *testing.T) {
		got, rest := splitSentences("Pi is 3.14 approximately and useful here. Next", false)
		assert.Equal(t, []string{"Pi is 3.14 approximately and useful here."}, got)
		assert.Equal(t, " Next", rest)
	})

	t.Run("holds short fragments when not final", func(t *testing.T) {
		// "Yes." is below minSentenceLen, so it is held to merge with the next.
		got, _ := splitSentences("Yes. The longer sentence continues here. ", false)
		assert.Equal(t, []string{"Yes. The longer sentence continues here."}, got)
	})

	t.Run("emits short final sentence when final", func(t *testing.T) {
		got, rest := splitSentences("Yes. ", true)
		assert.Equal(t, []string{"Yes."}, got)
		assert.Empty(t, strings.TrimSpace(rest), "only trailing whitespace may remain")
	})
}
