package stage

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// guardedReply is a canned reply as runBeforeCallHooks (direction input) or an
// output block (direction output) records it.
func guardedReply(content, direction string) types.Message {
	return types.Message{
		Role: roleAssistant, Content: content, FinishReason: types.FinishReasonSafety,
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

func TestExcludeBlockedInputTurns(t *testing.T) {
	tests := []struct {
		name string
		msgs []types.Message
		want []string
	}{
		{
			name: "drops the blocked user message and its canned reply",
			msgs: []types.Message{
				{Role: roleUser, Content: "q1"}, {Role: roleAssistant, Content: "a1"},
				{Role: roleUser, Content: "forbidden"}, blockedInputReply("refused"),
				{Role: roleUser, Content: "q2"},
			},
			want: []string{"user:q1", "assistant:a1", "user:q2"},
		},
		{
			name: "keeps an output block's replacement",
			msgs: []types.Message{
				{Role: roleUser, Content: "q1"}, guardedReply("withheld", hooks.DirectionOutput),
				{Role: roleUser, Content: "q2"},
			},
			want: []string{"user:q1", "assistant:withheld", "user:q2"},
		},
		{
			name: "keeps a safety reply without a validation",
			msgs: []types.Message{
				{Role: roleUser, Content: "q1"},
				{Role: roleAssistant, Content: "filtered", FinishReason: types.FinishReasonSafety},
			},
			want: []string{"user:q1", "assistant:filtered"},
		},
		{
			name: "keeps an input validation on a reply that did not stop for safety",
			msgs: []types.Message{
				{Role: roleUser, Content: "q1"},
				func() types.Message { m := blockedInputReply("a1"); m.FinishReason = "stop"; return m }(),
			},
			want: []string{"user:q1", "assistant:a1"},
		},
		{
			name: "only an assistant message is a canned reply",
			msgs: []types.Message{func() types.Message {
				m := blockedInputReply("q1")
				m.Role = roleUser
				return m
			}()},
			want: []string{"user:q1"},
		},
		{
			name: "drops only the reply when no user message precedes it",
			msgs: []types.Message{
				{Role: roleUser, Content: "q1"},
				{Role: roleAssistant, ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "t"}}},
				turnResult("c1", "r"),
				blockedInputReply("refused"),
				{Role: roleUser, Content: "q2"},
			},
			want: []string{"user:q1", "assistant:", "tool:r", "user:q2"},
		},
		{
			name: "a blocked reply that opens the history drops alone",
			msgs: []types.Message{blockedInputReply("refused"), {Role: roleUser, Content: "q2"}},
			want: []string{"user:q2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, roleLines(excludeBlockedInputTurns(tt.msgs)))
		})
	}
}

func TestExcludeBlockedInputTurns_KeepsAReplyWithToolCalls(t *testing.T) {
	reply := blockedInputReply("calling")
	reply.ToolCalls = []types.MessageToolCall{{ID: "c1", Name: "t"}}
	msgs := []types.Message{{Role: roleUser, Content: "q1"}, reply}
	got := excludeBlockedInputTurns(msgs)
	require.Len(t, got, 2, "a reply carrying tool calls is not a canned turn; dropping it would orphan its results")
	assert.Equal(t, "c1", got[1].ToolCalls[0].ID)
}

func TestExcludeFromModelContext_DropsBothInterruptedAndBlocked(t *testing.T) {
	msgs := []types.Message{
		{Role: roleUser, Content: "q1"}, interruptedReply("half"),
		{Role: roleUser, Content: "forbidden"}, blockedInputReply("refused"),
		{Role: roleUser, Content: "q2"},
	}
	assert.Equal(t, []string{"user:q1", "user:q2"}, roleLines(excludeFromModelContext(msgs)))
}

func TestIncrementalSaveStage_IndexSkipsBlockedInputTurn(t *testing.T) {
	mockIndex := &mockMessageIndexForSave{}
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: statestore.NewMemoryStore(), ConversationID: "c"},
		MessageIndex:     mockIndex,
	})

	blockedUser := types.Message{Role: roleUser, Content: "forbidden"}
	canned := blockedInputReply("refused")
	next := types.Message{Role: roleUser, Content: "q2"}
	again := types.Message{Role: roleUser, Content: "forbidden again"}
	cannedAgain := blockedInputReply("refused again")
	runTestStage(t, s, []StreamElement{
		NewMessageElement(&blockedUser), NewMessageElement(&canned), NewMessageElement(&next),
		NewMessageElement(&again), NewMessageElement(&cannedAgain),
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

	u1 := types.Message{Role: roleUser, Content: "q1"}
	a1 := types.Message{Role: roleAssistant, Content: "a1"}
	blockedUser := types.Message{Role: roleUser, Content: "forbidden"}
	canned := blockedInputReply("refused")
	runTestStage(t, s, []StreamElement{
		NewMessageElement(&u1), NewMessageElement(&a1),
		NewMessageElement(&blockedUser), NewMessageElement(&canned),
	})

	assert.Equal(t, []string{"user:q1", "assistant:a1"}, roleLines(summarizer.batch))
	summaries, err := store.LoadSummaries(context.Background(), "c")
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, 3, summaries[0].EndTurn, "the summary still covers the blocked message's turn")
}

func TestContextBuilder_SummaryExcludesBlockedInputTurn(t *testing.T) {
	summarizer := &recordingSummarizer{}
	got := runTurnBoundary(t,
		&ContextBuilderPolicy{TokenBudget: 80, Strategy: TruncateSummarize, Summarizer: summarizer},
		turnMsg(roleUser, "forbidden"),
		blockedInputReply("refused"),
		turnMsg(roleUser, "q0 "+strings.Repeat("w ", 79)),
		turnMsg(roleAssistant, "a0"),
		turnMsg(roleUser, "q1 w w w w"),
	)
	assert.Equal(t, []string{"system:[Summary of 2 earlier messages]: summary", "user:q1 w w w w"}, got)
	assert.Equal(t, []string{"user:q0 " + strings.Repeat("w ", 79), "assistant:a0"}, roleLines(summarizer.batch))
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

	u1 := types.Message{Role: roleUser, Content: "I love Go"}
	a1 := types.Message{Role: roleAssistant, Content: "Great choice!"}
	blockedUser := types.Message{Role: roleUser, Content: "forbidden"}
	canned := blockedInputReply("refused")
	out := runTestStage(t, s, []StreamElement{
		NewMessageElement(&u1), NewMessageElement(&a1),
		NewMessageElement(&blockedUser), NewMessageElement(&canned),
	})

	assert.Equal(t, []string{"user:I love Go", "assistant:Great choice!"}, roleLines(ext.got))
	assert.Len(t, out, 4, "every message still flows downstream")
}

// reaskHistorySpy records the messages of every tool-path call, so a test can
// see what the structured-output re-ask sent.
type reaskHistorySpy struct {
	*schemaSpyProvider
	seen [][]types.Message
}

func (p *reaskHistorySpy) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest,
	providerTools providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	return p.schemaSpyProvider.PredictWithTools(ctx, req, providerTools, choice)
}

// Every call of a structured-output turn, the re-ask included, leaves out a
// blocked input turn from earlier in the conversation.
func TestFinalTurn_ReaskLeavesOutBlockedInputTurn(t *testing.T) {
	p := &reaskHistorySpy{schemaSpyProvider: &schemaSpyProvider{toolRounds: 1}}
	reg := registryWithTools(t, "probe")
	reg.RegisterExecutor(staticExecutor{})
	turnState := NewTurnState()
	turnState.SystemPrompt = "sys"
	turnState.AllowedTools = []string{"probe"}
	stage := NewProviderStageWithTurnState(p, reg, nil,
		&ProviderConfig{MaxTokens: 100, ResponseFormat: jsonSchemaFormat()}, nil, nil, turnState)

	blockedUser := types.Message{Role: roleUser, Content: "forbidden"}
	canned := blockedInputReply("refused")
	userMsg := types.Message{Role: roleUser, Content: "go"}
	runTestStage(t, stage, []StreamElement{
		NewMessageElement(&blockedUser), NewMessageElement(&canned), NewMessageElement(&userMsg),
	})

	require.Len(t, p.seen, 3, "a tool round, the round that ends the loop, and the re-ask")
	for i, msgs := range p.seen {
		lines := roleLines(msgs)
		require.NotEmpty(t, lines)
		assert.Equal(t, "user:go", lines[0], "call %d must start at the clean turn", i+1)
		assert.NotContains(t, lines, "user:forbidden", "call %d sent the blocked turn", i+1)
	}
}
