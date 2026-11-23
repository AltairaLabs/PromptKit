package persist

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/history"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// guardedReply is a canned reply as runBeforeCallHooks (direction input) or an
// output block (direction output) records it.
func guardedReply(content, direction string) types.Message {
	return types.Message{
		Role: core.RoleAssistant, Content: content, FinishReason: types.FinishReasonSafety,
		Validations: []types.ValidationResult{{
			ValidatorType: "content_excludes",
			Details:       map[string]any{"direction": direction},
		}},
	}
}

func blockedInputReply(content string) types.Message {
	return guardedReply(content, hooks.DirectionInput)
}

func roleLines(msgs []types.Message) []string {
	out := make([]string, 0, len(msgs))
	for i := range msgs {
		out = append(out, msgs[i].Role+":"+msgs[i].GetContent())
	}
	return out
}

// cutThenLateBlocked is a stored duplex history where a barge-in cut the reply
// to a spoken input whose late transcript was then blocked: the reply before
// the blocked request is the cut one, not the answer to the turn before.
func cutThenLateBlocked(next ...types.Message) []types.Message {
	return append([]types.Message{
		{Role: core.RoleUser, Content: "hello"},
		{Role: core.RoleAssistant, Content: "Hi!"},
		interruptedReply("Sure, I'll"),
		{Role: core.RoleUser, Content: "a wire transfer please", Meta: map[string]interface{}{
			types.MetaBlockedInput: true, history.BlockedReplyBeforeMetaKey: true,
		}},
	}, next...)
}

func TestIncrementalSaveStage_IndexSkipsBlockedInputTurn(t *testing.T) {
	mockIndex := &mockMessageIndexForSave{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: statestore.NewMemoryStore(), ConversationID: "c"},
		MessageIndex:     mockIndex,
	})

	blockedUser := types.Message{Role: core.RoleUser, Content: "forbidden"}
	canned := blockedInputReply("refused")
	next := types.Message{Role: core.RoleUser, Content: "q2"}
	again := types.Message{Role: core.RoleUser, Content: "forbidden again"}
	cannedAgain := blockedInputReply("refused again")
	runTestStage(t, s, []core.StreamElement{
		core.NewMessageElement(&blockedUser), core.NewMessageElement(&canned), core.NewMessageElement(&next),
		core.NewMessageElement(&again), core.NewMessageElement(&cannedAgain),
	})

	require.Len(t, mockIndex.indexCalls, 1)
	assert.Equal(t, "q2", mockIndex.indexCalls[0].message.Content)
	assert.Equal(t, 2, mockIndex.indexCalls[0].turnIndex, "the skipped turn keeps its positions")
}

// The batch ends on the blocked user message; its canned reply falls in the
// next batch. The user message must still be left out of the summary.
func TestIncrementalSaveStage_SummaryExcludesBlockedInputTurn(t *testing.T) {
	store := statestore.NewMemoryStore()
	summarizer := &recordingSummarizer{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig:   &pipeline.StateStoreConfig{Store: store, ConversationID: "c"},
		Summarizer:         summarizer,
		SummarizeThreshold: 2,
		SummarizeBatchSize: 3,
	})

	u1 := types.Message{Role: core.RoleUser, Content: "q1"}
	a1 := types.Message{Role: core.RoleAssistant, Content: "a1"}
	blockedUser := types.Message{Role: core.RoleUser, Content: "forbidden"}
	canned := blockedInputReply("refused")
	runTestStage(t, s, []core.StreamElement{
		core.NewMessageElement(&u1), core.NewMessageElement(&a1),
		core.NewMessageElement(&blockedUser), core.NewMessageElement(&canned),
	})

	assert.Equal(t, []string{"user:q1", "assistant:a1"}, roleLines(summarizer.batch))
	summaries, err := store.LoadSummaries(context.Background(), "c")
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, 3, summaries[0].EndTurn, "the summary still covers the blocked message's turn")
}

// recordingExtractor records the messages memory extraction was given.
type recordingExtractor struct {
	got []types.Message
}

func (e *recordingExtractor) Extract(
	_ context.Context, _ map[string]string, msgs []types.Message,
) ([]*memory.Memory, error) {
	e.got = append([]types.Message(nil), msgs...)
	return nil, nil
}

func TestMemoryExtractionStage_SkipsBlockedInputTurn(t *testing.T) {
	ext := &recordingExtractor{}
	s := NewMemoryExtractionStage(ext, memory.NewInMemoryStore(), map[string]string{"user_id": "u"})

	u1 := types.Message{Role: core.RoleUser, Content: "I love Go"}
	a1 := types.Message{Role: core.RoleAssistant, Content: "Great choice!"}
	blockedUser := types.Message{Role: core.RoleUser, Content: "forbidden"}
	canned := blockedInputReply("refused")
	out := runTestStage(t, s, []core.StreamElement{
		core.NewMessageElement(&u1), core.NewMessageElement(&a1),
		core.NewMessageElement(&blockedUser), core.NewMessageElement(&canned),
	})

	assert.Equal(t, []string{"user:I love Go", "assistant:Great choice!"}, roleLines(ext.got))
	assert.Len(t, out, 4, "every message still flows downstream")
}

func TestMemoryExtractionStage_CutReplyBeforeLateBlockedRequest(t *testing.T) {
	ext := &recordingExtractor{}
	s := NewMemoryExtractionStage(ext, memory.NewInMemoryStore(), map[string]string{"user_id": "u"})
	history := cutThenLateBlocked()
	elems := make([]core.StreamElement, len(history))
	for i := range history {
		elems[i] = core.NewMessageElement(&history[i])
	}
	runTestStage(t, s, elems)
	assert.Equal(t, []string{"user:hello", "assistant:Hi!"}, roleLines(ext.got))
}
