package stages

import (
	"context"
	"strings"
	"unicode"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// minSentenceLen is the minimum length (trimmed) a fragment must reach before it
// is emitted as its own sentence. Short fragments (abbreviations like "Dr.",
// "e.g.", a bare "Yes.") are held back and merged with what follows, so they
// don't each become a separate, choppy TTS synthesis.
const minSentenceLen = 12

// TTSSentenceAggregatorStage turns a streaming provider's token-by-token text
// deltas into complete sentences for TTS, so the agent speaks in smooth phrases
// instead of "word … pause … word".
//
// It sits between the AssistantTTSFilterStage (which has already dropped the user
// transcript) and the TTS stage. As assistant text deltas arrive it accumulates
// them and emits each complete sentence (ending in . ! or ?) the moment it forms
// — while the model is still generating the rest. That overlaps generation with
// speech: audio starts after the first sentence rather than after the whole
// reply.
//
// The complete assistant Message that the provider emits at the end of a turn
// duplicates the deltas, so once any delta has been seen the Message is dropped
// (after flushing the trailing partial sentence). For a NON-streaming provider
// (no deltas), the Message's content is split into sentences instead.
//
// Control signals pass through: Interrupt clears the buffer (barge-in discards
// the half-spoken reply); EndOfTurn / EndOfStream flush the trailing buffer.
type TTSSentenceAggregatorStage struct {
	stage.BaseStage
	buf      strings.Builder
	sawDelta bool
}

// NewTTSSentenceAggregatorStage creates the sentence aggregator.
func NewTTSSentenceAggregatorStage() *TTSSentenceAggregatorStage {
	return &TTSSentenceAggregatorStage{
		BaseStage: stage.NewBaseStage("tts_sentence_aggregator", stage.StageTypeTransform),
	}
}

// Process implements the Stage interface.
func (s *TTSSentenceAggregatorStage) Process(
	ctx context.Context,
	input <-chan stage.StreamElement,
	output chan<- stage.StreamElement,
) error {
	defer close(output)

	for elem := range input {
		if err := s.handle(ctx, elem, output); err != nil {
			return err
		}
	}
	// Input closed without a trailing control signal: flush whatever remains.
	return s.flush(ctx, output)
}

//nolint:gocritic // hugeParam: StreamElement is the stage element type, passed by value by contract.
func (s *TTSSentenceAggregatorStage) handle(
	ctx context.Context, elem stage.StreamElement, output chan<- stage.StreamElement,
) error {
	switch {
	case elem.Interrupt:
		// Barge-in: discard the half-spoken reply, forward the Interrupt.
		s.reset()
		return s.send(ctx, elem, output)

	case elem.EndOfTurn || elem.EndOfStream:
		if err := s.flush(ctx, output); err != nil {
			return err
		}
		return s.send(ctx, elem, output)

	case elem.Message != nil:
		return s.handleMessage(ctx, elem, output)

	case elem.Text != nil:
		s.sawDelta = true
		s.buf.WriteString(*elem.Text)
		return s.emitReady(ctx, output)

	default:
		return s.send(ctx, elem, output)
	}
}

// handleMessage processes a complete assistant Message: drop it when deltas
// already covered the text (just flush the tail), or split it into sentences
// when the provider did not stream (no deltas).
//
//nolint:gocritic // hugeParam: StreamElement is the stage element type, passed by value by contract.
func (s *TTSSentenceAggregatorStage) handleMessage(
	ctx context.Context, elem stage.StreamElement, output chan<- stage.StreamElement,
) error {
	if s.sawDelta {
		// The deltas already produced the spoken sentences; the Message is a
		// duplicate. Flush the trailing partial sentence and drop the Message.
		return s.flush(ctx, output)
	}
	// Non-streaming provider: split the complete content into sentences.
	s.buf.WriteString(messageText(elem.Message))
	return s.flush(ctx, output)
}

// emitReady pulls every complete sentence currently in the buffer and emits each
// as its own Text element, leaving any trailing partial sentence buffered.
func (s *TTSSentenceAggregatorStage) emitReady(ctx context.Context, output chan<- stage.StreamElement) error {
	sentences, rest := splitSentences(s.buf.String(), false)
	s.buf.Reset()
	s.buf.WriteString(rest)
	for _, sentence := range sentences {
		if err := s.send(ctx, stage.NewTextElement(sentence), output); err != nil {
			return err
		}
	}
	return nil
}

// flush emits all remaining buffered text (including a trailing fragment with no
// terminal punctuation) as sentences, then clears per-turn state.
func (s *TTSSentenceAggregatorStage) flush(ctx context.Context, output chan<- stage.StreamElement) error {
	sentences, rest := splitSentences(s.buf.String(), true)
	if trimmed := strings.TrimSpace(rest); trimmed != "" {
		sentences = append(sentences, trimmed)
	}
	s.reset()
	for _, sentence := range sentences {
		if err := s.send(ctx, stage.NewTextElement(sentence), output); err != nil {
			return err
		}
	}
	return nil
}

func (s *TTSSentenceAggregatorStage) reset() {
	s.buf.Reset()
	s.sawDelta = false
}

//nolint:gocritic // hugeParam: StreamElement is the stage element type, passed by value by contract.
func (s *TTSSentenceAggregatorStage) send(
	ctx context.Context, elem stage.StreamElement, output chan<- stage.StreamElement,
) error {
	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// messageText returns the text content of a Message (Content, else the first
// non-empty text part).
func messageText(msg *types.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Content != "" {
		return msg.Content
	}
	for i := range msg.Parts {
		if msg.Parts[i].Text != nil && *msg.Parts[i].Text != "" {
			return *msg.Parts[i].Text
		}
	}
	return ""
}

// splitSentences pulls complete sentences off the front of text. A sentence ends
// at . ! or ? followed by whitespace. Fragments shorter than minSentenceLen are
// held back (merged with what follows) so abbreviations and brief clauses don't
// each become a choppy synthesis. When final is true the length floor is relaxed
// so a genuinely short final sentence (e.g. "Yes.") is still emitted.
//
// Returns the complete sentences and the unconsumed remainder.
func splitSentences(text string, final bool) (sentences []string, rest string) {
	runes := []rune(text)
	start := 0
	for i := 0; i < len(runes); i++ {
		if !isSentenceEnd(runes[i]) {
			continue
		}
		// Require whitespace after the terminator to confirm the sentence ended
		// (avoids splitting "3.14" or "U.S."). At the very end of the buffer we
		// can't see the next char, so leave it for the caller's flush.
		if i+1 >= len(runes) || !unicode.IsSpace(runes[i+1]) {
			continue
		}
		candidate := strings.TrimSpace(string(runes[start : i+1]))
		if !final && len(candidate) < minSentenceLen {
			continue // hold short fragments back to merge with the next sentence
		}
		if candidate != "" {
			sentences = append(sentences, candidate)
		}
		start = i + 1
	}
	return sentences, string(runes[start:])
}

func isSentenceEnd(r rune) bool {
	return r == '.' || r == '!' || r == '?'
}
