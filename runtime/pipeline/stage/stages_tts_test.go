package stage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// errTTSService is a TTSService whose Synthesize always fails.
type errTTSService struct{}

func (errTTSService) Synthesize(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("synthesis boom")
}
func (errTTSService) MIMEType() string { return "audio/pcm" }

// TestTTSStage_SynthesisErrorForwardsElement verifies that a synthesis failure
// stamps the element's Error and still forwards the element (no audio) rather
// than aborting the pipeline.
func TestTTSStage_SynthesisErrorForwardsElement(t *testing.T) {
	s := stage.NewTTSStage(errTTSService{}, stage.DefaultTTSConfig())

	results := runStage(t, s, []stage.StreamElement{makeTextElement("hello")}, 2*time.Second)

	if len(results) != 1 {
		t.Fatalf("expected 1 forwarded element, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Fatal("expected result.Error to be set on synthesis failure")
	}
	if results[0].Audio != nil {
		t.Error("expected no audio when synthesis fails")
	}
}

// TestTTSStage_ExtractTextFromParts verifies text is pulled from Message.Parts
// when Content is empty.
func TestTTSStage_ExtractTextFromParts(t *testing.T) {
	svc := &pricingTTSService{audio: []byte("pcm")}
	s := stage.NewTTSStage(svc, stage.DefaultTTSConfig())

	partText := "from parts"
	msg := &types.Message{
		Role: "assistant",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &partText},
		},
	}
	results := runStage(t, s, []stage.StreamElement{stage.NewMessageElement(msg)}, 2*time.Second)

	if len(results) != 1 {
		t.Fatalf("expected 1 element, got %d", len(results))
	}
	if results[0].Audio == nil {
		t.Error("expected audio synthesized from part text")
	}
}

// TestTTSStage_NoTextNoSynthesis verifies an element with no extractable text is
// forwarded untouched (processElement early return).
func TestTTSStage_NoTextNoSynthesis(t *testing.T) {
	svc := &pricingTTSService{audio: []byte("pcm")}
	s := stage.NewTTSStage(svc, stage.DefaultTTSConfig())

	// Message with no Content and no text parts.
	msg := &types.Message{Role: "assistant"}
	results := runStage(t, s, []stage.StreamElement{stage.NewMessageElement(msg)}, 2*time.Second)

	if len(results) != 1 {
		t.Fatalf("expected 1 element, got %d", len(results))
	}
	if results[0].Audio != nil {
		t.Error("expected no audio for element without text")
	}
}

// TestTTSStage_BelowMinTextLengthSkipped verifies shouldSynthesize's
// MinTextLength filter skips short text.
func TestTTSStage_BelowMinTextLengthSkipped(t *testing.T) {
	svc := &pricingTTSService{audio: []byte("pcm")}
	s := stage.NewTTSStage(svc, stage.TTSConfig{SkipEmpty: true, MinTextLength: 10})

	results := runStage(t, s, []stage.StreamElement{makeTextElement("hi")}, 2*time.Second)

	if len(results) != 1 {
		t.Fatalf("expected 1 element, got %d", len(results))
	}
	if results[0].Audio != nil {
		t.Error("expected short text (below MinTextLength) to be skipped")
	}
}

// TestTTSStage_ContextCancelStopsForwarding verifies Process returns the context
// error when the consumer stops reading and the context is canceled.
func TestTTSStage_ContextCancelStopsForwarding(t *testing.T) {
	svc := &pricingTTSService{audio: []byte("pcm")}
	s := stage.NewTTSStage(svc, stage.DefaultTTSConfig())

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement) // unbuffered, never read

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- s.Process(ctx, input, output) }()

	input <- makeTextElement("hello world")
	close(input)

	// Give Process a moment to reach the blocked send, then cancel.
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Process did not return after context cancellation")
	}
}
