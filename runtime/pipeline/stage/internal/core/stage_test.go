package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStageType_String(t *testing.T) {
	tests := []struct {
		stageType StageType
		expected  string
	}{
		{StageTypeTransform, "transform"},
		{StageTypeAccumulate, "accumulate"},
		{StageTypeGenerate, "generate"},
		{StageTypeSink, "sink"},
		{StageTypeBidirectional, "bidirectional"},
		{StageType(99), "unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.stageType.String())
	}
}

func TestStageFunc_Process(t *testing.T) {
	t.Run("executes custom function", func(t *testing.T) {
		// Create a function that transforms content
		fn := func(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
			defer close(output)
			for elem := range input {
				if elem.Message != nil {
					elem.Message.Content = "transformed: " + elem.Message.Content
				}
				select {
				case output <- elem:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}

		stage := NewStageFunc("transformer", StageTypeTransform, fn)
		assert.Equal(t, "transformer", stage.Name())
		assert.Equal(t, StageTypeTransform, stage.Type())

		input := make(chan StreamElement, 1)
		output := make(chan StreamElement, 1)

		msg := &types.Message{Role: "user", Content: "hello"}
		input <- NewMessageElement(msg)
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		result := <-output
		assert.Equal(t, "transformed: hello", result.Message.Content)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		fn := func(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
			<-ctx.Done()
			return ctx.Err()
		}

		stage := NewStageFunc("blocker", StageTypeTransform, fn)
		input := make(chan StreamElement)
		output := make(chan StreamElement)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := stage.Process(ctx, input, output)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestFilterStage_Process(t *testing.T) {
	t.Run("filters elements based on predicate", func(t *testing.T) {
		// Only allow assistant messages
		predicate := func(elem StreamElement) bool {
			return elem.Message != nil && elem.Message.Role == "assistant"
		}

		stage := NewFilterStage("assistant-filter", predicate)
		assert.Equal(t, "assistant-filter", stage.Name())
		assert.Equal(t, StageTypeTransform, stage.Type())

		input := make(chan StreamElement, 3)
		output := make(chan StreamElement, 3)

		input <- NewMessageElement(&types.Message{Role: "user", Content: "hello"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "hi"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "bye"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		// Should only have the assistant message
		var results []StreamElement
		for elem := range output {
			results = append(results, elem)
		}
		assert.Len(t, results, 1)
		assert.Equal(t, "assistant", results[0].Message.Role)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		predicate := func(elem StreamElement) bool { return true }
		stage := NewFilterStage("filter", predicate)

		ctx, cancel := context.WithCancel(context.Background())
		input := make(chan StreamElement, 1)
		output := make(chan StreamElement) // Unbuffered

		msg := NewMessageElement(&types.Message{Role: "user", Content: "test"})
		input <- msg
		close(input)

		cancel()

		err := stage.Process(ctx, input, output)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("passes all elements when predicate always returns true", func(t *testing.T) {
		predicate := func(elem StreamElement) bool { return true }
		stage := NewFilterStage("passthrough-filter", predicate)

		input := make(chan StreamElement, 2)
		output := make(chan StreamElement, 2)

		input <- NewMessageElement(&types.Message{Role: "user", Content: "hello"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "hi"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		var count int
		for range output {
			count++
		}
		assert.Equal(t, 2, count)
	})

	t.Run("filters all elements when predicate always returns false", func(t *testing.T) {
		predicate := func(elem StreamElement) bool { return false }
		stage := NewFilterStage("reject-all", predicate)

		input := make(chan StreamElement, 2)
		output := make(chan StreamElement, 2)

		input <- NewMessageElement(&types.Message{Role: "user", Content: "hello"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "hi"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		var count int
		for range output {
			count++
		}
		assert.Equal(t, 0, count)
	})
}

func TestPassthroughStage_ForwardsEverythingThenCloses(t *testing.T) {
	stage := NewPassthroughStage("pass")
	assert.Equal(t, "pass", stage.Name())
	assert.Equal(t, StageTypeTransform, stage.Type())

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 2)
	input <- NewTextElement("a")
	input <- NewTextElement("b")
	close(input)
	require.NoError(t, stage.Process(context.Background(), input, output))

	var got []string
	for elem := range output {
		got = append(got, *elem.Text)
	}
	assert.Equal(t, []string{"a", "b"}, got)
}

func TestPassthroughStage_StopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	input := make(chan StreamElement, 1)
	input <- NewTextElement("a")
	close(input)
	err := NewPassthroughStage("pass").Process(ctx, input, make(chan StreamElement))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestMapStage_TransformsEachElement(t *testing.T) {
	stage := NewMapStage("upper", func(e StreamElement) (StreamElement, error) {
		return NewTextElement(strings.ToUpper(*e.Text)), nil
	})
	assert.Equal(t, "upper", stage.Name())
	assert.Equal(t, StageTypeTransform, stage.Type())

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 2)
	input <- NewTextElement("a")
	input <- NewTextElement("b")
	close(input)
	require.NoError(t, stage.Process(context.Background(), input, output))

	var got []string
	for elem := range output {
		got = append(got, *elem.Text)
	}
	assert.Equal(t, []string{"A", "B"}, got)
}

func TestMapStage_ErrorIsEmittedAndReturned(t *testing.T) {
	boom := errors.New("boom")
	stage := NewMapStage("fail", func(StreamElement) (StreamElement, error) { return StreamElement{}, boom })
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)
	input <- NewTextElement("a")
	close(input)

	err := stage.Process(context.Background(), input, output)
	assert.ErrorIs(t, err, boom)
	emitted := <-output
	assert.ErrorIs(t, emitted.Error, boom, "the error goes downstream too")
	assert.Equal(t, PriorityCritical, emitted.Priority)
}

func TestMapStage_StopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	input := make(chan StreamElement, 1)
	input <- NewTextElement("a")
	close(input)
	stage := NewMapStage("m", func(e StreamElement) (StreamElement, error) { return e, nil })
	assert.ErrorIs(t, stage.Process(ctx, input, make(chan StreamElement)), context.Canceled)
}
