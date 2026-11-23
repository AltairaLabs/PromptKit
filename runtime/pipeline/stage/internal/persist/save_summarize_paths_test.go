package persist

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// contents returns each message's content.
func contents(msgs []types.Message) []string {
	out := make([]string, 0, len(msgs))
	for i := range msgs {
		out = append(out, msgs[i].Content)
	}
	return out
}

// The next summary starts where the last one ended and is built from exactly
// the messages it covers.
func TestMaybeSummarize_SummarizesTheBatchAfterTheLastSummary(t *testing.T) {
	store := newReaderWithSummaryStore()
	store.messages["conv-ms"] = numberedMessages(9)
	store.summaries["conv-ms"] = []statestore.Summary{{StartTurn: 0, EndTurn: 4, Content: "old"}}
	summarizer := &recordingSummarizer{}

	callMaybeSummarize(t, store, summarizer, 3, 5)

	assert.Equal(t, []string{"msg 4", "msg 5", "msg 6", "msg 7", "msg 8"}, contents(summarizer.batch))
	require.Len(t, store.summaries["conv-ms"], 2)
	assert.Equal(t, statestore.Summary{StartTurn: 4, EndTurn: 9, Content: "summary"}, store.summaries["conv-ms"][1])
}

// summarizingStage saves into store with the given summarizer settings.
func summarizingStage(store any, summarizer statestore.Summarizer, threshold, batch int) *IncrementalSaveStage {
	return NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig:   &pipeline.StateStoreConfig{Store: store, ConversationID: "conv-ms"},
		Summarizer:         summarizer,
		SummarizeThreshold: threshold,
		SummarizeBatchSize: batch,
	})
}

// messageElements wraps each message in an element.
func messageElements(msgs []types.Message) []core.StreamElement {
	elems := make([]core.StreamElement, len(msgs))
	for i := range msgs {
		elems[i] = core.NewMessageElement(&msgs[i])
	}
	return elems
}

// A threshold with no summarizer summarizes nothing, and the turn is saved.
func TestIncrementalSaveStage_ThresholdWithoutSummarizerSummarizesNothing(t *testing.T) {
	store := newReaderWithSummaryStore()
	runTestStage(t, summarizingStage(store, nil, 1, 1), messageElements(numberedMessages(3)))

	assert.Empty(t, store.summaries["conv-ms"])
	assert.Equal(t, []string{"msg 0", "msg 1", "msg 2"}, contents(store.messages["conv-ms"]))
}

// A zero threshold disables summarization.
func TestIncrementalSaveStage_ZeroThresholdDisablesSummaries(t *testing.T) {
	store := newReaderWithSummaryStore()
	summarizer := &recordingSummarizer{}
	runTestStage(t, summarizingStage(store, summarizer, 0, 1), messageElements(numberedMessages(3)))

	assert.Empty(t, store.summaries["conv-ms"])
	assert.Nil(t, summarizer.batch, "the summarizer is not called")
}

// The full-save path keeps the metadata the stored state already has.
func TestIncrementalSaveStage_FullSaveKeepsStoredMetadata(t *testing.T) {
	store := newMinimalStore()
	store.states["c"] = &statestore.ConversationState{
		ID: "c", Messages: []types.Message{{Role: "user", Content: "old"}},
		Metadata: map[string]any{"tenant": "acme"},
	}
	ts := core.NewTurnState()
	ts.ProviderRequestMetadata = map[string]any{"trace": "t1"}
	s := NewIncrementalSaveStageWithTurnState(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
	}, ts)

	msg := types.Message{Role: "user", Content: "new"}
	runTestStage(t, s, []core.StreamElement{core.NewMessageElement(&msg)})

	assert.Equal(t, map[string]any{"tenant": "acme", "trace": "t1"}, store.states["c"].Metadata)
	assert.Equal(t, []string{"old", "new"}, contents(store.states["c"].Messages))
}
