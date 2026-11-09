package stage

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In the continuous (streaming) mode, typed text is a whole turn: it fires
// on arrival, with the input still open and no EndOfTurn behind it. A
// Message element still waits for its EndOfTurn.
func TestProviderStage_Streaming_TypedTextFiresWithoutEndOfTurn(t *testing.T) {
	provider := &multiTurnRecordingProvider{}
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{Streaming: true})
	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 16)
	done := make(chan error, 1)
	go func() { done <- stage.Process(context.Background(), input, output) }()

	input <- NewMessageElement(&types.Message{Role: "user", Content: "spoken, turn not ended"})
	input <- NewTextElement("typed")
	replies := assistantContents(output)
	select {
	case e := <-replies:
		assert.Equal(t, "reply-1", e)
	case <-time.After(2 * time.Second):
		t.Fatal("typed text did not fire a turn")
	}
	// The session stays open: the next typed text is the next turn.
	input <- NewTextElement("typed again")
	select {
	case e := <-replies:
		assert.Equal(t, "reply-2", e)
	case <-time.After(2 * time.Second):
		t.Fatal("the stage stopped after the first typed turn")
	}
	close(input)
	require.NoError(t, <-done)
	require.Len(t, provider.requests, 2)
	assert.Equal(t, []types.Message{
		{Role: "user", Content: "spoken, turn not ended"},
		{Role: "user", Content: "typed"},
	}, provider.requests[0], "the typed turn carries the input pending before it")
}

// A typed turn whose provider call fails reports the failure to the caller,
// as an EndOfTurn-fired turn does.
func TestProviderStage_Streaming_TypedTextTurnFailureIsReported(t *testing.T) {
	stage := NewProviderStage(&erroringProvider{}, nil, nil, &ProviderConfig{Streaming: true})
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 16)
	input <- NewTextElement("typed")
	done := make(chan error, 1)
	go func() { done <- stage.Process(context.Background(), input, output) }()

	var reported error
	deadline := time.After(2 * time.Second)
	for reported == nil {
		select {
		case e, ok := <-output:
			if !ok {
				reported = <-done
				require.Error(t, reported, "output closed without reporting the failure")
				break
			}
			reported = e.Error
		case <-deadline:
			t.Fatal("the failed typed turn was never reported")
		}
	}
	close(input)
	assert.Contains(t, reported.Error(), "boom")
}

// assistantContents forwards the content of each assistant message on out.
func assistantContents(out <-chan StreamElement) <-chan string {
	ch := make(chan string, 8)
	go func() {
		defer close(ch)
		for e := range out {
			if e.Message != nil && e.Message.Role == roleAssistant {
				ch <- e.Message.Content
			}
		}
	}()
	return ch
}
