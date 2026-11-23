package persist

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// countingRetriever counts its calls and returns one memory.
type countingRetriever struct{ calls int }

func (r *countingRetriever) RetrieveContext(
	context.Context, map[string]string, []types.Message,
) ([]*memory.Memory, error) {
	r.calls++
	return []*memory.Memory{{Type: "fact", Content: "likes Go"}}, nil
}

// With no retriever, retrieval writes nothing onto the turn state and passes
// the turn on.
func TestMemoryRetrievalStage_NoRetrieverWritesNothing(t *testing.T) {
	ts := core.NewTurnState()
	s := NewMemoryRetrievalStageWithTurnState(nil, nil, nil, ts)

	msg := types.Message{Role: "user", Content: "hello"}
	out := runTestStage(t, s, []core.StreamElement{core.NewMessageElement(&msg)})

	require.Len(t, out, 1)
	assert.Equal(t, "hello", out[0].Message.Content)
	assert.Nil(t, ts.Variables)
}

// A turn without a message retrieves nothing and writes nothing.
func TestMemoryRetrievalStage_NoMessageRetrievesNothing(t *testing.T) {
	ret := &countingRetriever{}
	ts := core.NewTurnState()
	s := NewMemoryRetrievalStageWithTurnState(ret, nil, nil, ts)

	out := runTestStage(t, s, []core.StreamElement{core.NewTextElement("chunk")})

	require.Len(t, out, 1)
	assert.Equal(t, "chunk", *out[0].Text)
	assert.Equal(t, 0, ret.calls)
	assert.Nil(t, ts.Variables)
}
