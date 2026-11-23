package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The facade's wrappers of internal/persist's constructors build each stage
// under its own name and with what they were given, so a stage.X caller sees
// no difference from before the move (#2876).

// facadeRetriever returns its memories for every request.
type facadeRetriever struct{ memories []*memory.Memory }

func (r facadeRetriever) RetrieveContext(
	context.Context, map[string]string, []types.Message,
) ([]*memory.Memory, error) {
	return r.memories, nil
}

func TestFacade_MemoryStagesKeepTheirName(t *testing.T) {
	assert.Equal(t, "memory_retrieval", NewMemoryRetrievalStage(nil, nil, nil).Name())
	assert.Equal(t, "memory_extraction", NewMemoryExtractionStage(nil, nil, nil).Name())
}

func TestFacade_MemoryRetrievalWritesTheTurnState(t *testing.T) {
	mems := []*memory.Memory{{Type: "fact", Content: "User likes Go"}}
	ts := NewTurnState()
	s := NewMemoryRetrievalStageWithTurnState(facadeRetriever{memories: mems}, nil, nil, ts)
	assert.Equal(t, "memory_retrieval", s.Name())

	msg := types.Message{Role: "user", Content: "hello"}
	out := runTestStage(t, s, []StreamElement{NewMessageElement(&msg)})

	require.Len(t, out, 1)
	assert.Equal(t, memory.DefaultContextFormatter(mems), ts.Variables["memory_context"])
}

func TestFacade_IncrementalSaveStagesSave(t *testing.T) {
	for name, build := range map[string]func(*IncrementalSaveConfig) *IncrementalSaveStage{
		"plain": NewIncrementalSaveStage,
		"turn state": func(c *IncrementalSaveConfig) *IncrementalSaveStage {
			return NewIncrementalSaveStageWithTurnState(c, NewTurnState())
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := statestore.NewMemoryStore()
			s := build(&IncrementalSaveConfig{
				StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
			})
			assert.Equal(t, "incremental_save", s.Name())

			msg := types.Message{Role: "user", Content: "hello"}
			runTestStage(t, s, []StreamElement{NewMessageElement(&msg)})

			state, err := store.Load(context.Background(), "c")
			require.NoError(t, err)
			require.Len(t, state.Messages, 1)
			assert.Equal(t, "hello", state.Messages[0].Content)
		})
	}
}

func TestFacade_IsTranscriptMessage(t *testing.T) {
	newMsg := types.Message{Role: "user", Content: "new"}
	history := types.Message{Role: "user", Content: "old", Source: "statestore"}
	held := NewMessageElement(&newMsg)
	held.Meta.PlayoutHeld = true

	newElem := NewMessageElement(&newMsg)
	historyElem := NewMessageElement(&history)
	assert.True(t, IsTranscriptMessage(&newElem))
	assert.False(t, IsTranscriptMessage(&historyElem))
	assert.False(t, IsTranscriptMessage(&held))
	assert.False(t, IsTranscriptMessage(&StreamElement{}))
}
