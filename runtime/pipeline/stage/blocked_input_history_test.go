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
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
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

// markedReply is a canned reply as a block marks it, with no validation (a
// hook that names no validator).
func markedReply(content string) types.Message {
	return types.Message{
		Role: roleAssistant, Content: content, FinishReason: types.FinishReasonSafety,
		Meta: map[string]interface{}{types.MetaBlockedInput: true},
	}
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
			name: "drops a marked reply that records no validation",
			msgs: []types.Message{
				{Role: roleUser, Content: "forbidden"}, markedReply("refused"),
				{Role: roleUser, Content: "q2"},
			},
			want: []string{"user:q2"},
		},
		{
			name: "keeps a reply whose mark is not true",
			msgs: []types.Message{
				{Role: roleUser, Content: "q1"},
				func() types.Message {
					m := markedReply("a1")
					m.Meta[types.MetaBlockedInput] = false
					return m
				}(),
			},
			want: []string{"user:q1", "assistant:a1"},
		},
		{
			name: "keeps a marked message that is neither a reply nor a request",
			msgs: []types.Message{func() types.Message {
				m := markedReply("r1")
				m.Role = roleTool
				return m
			}()},
			want: []string{"tool:r1"},
		},
		{
			name: "drops a marked user message alone, keeping the reply before it",
			msgs: []types.Message{
				{Role: roleUser, Content: "q1"}, {Role: roleAssistant, Content: "sure"},
				{Role: roleUser, Content: "forbidden", Meta: map[string]interface{}{types.MetaBlockedInput: true}},
				{Role: roleUser, Content: "q2"},
			},
			want: []string{"user:q1", "assistant:sure", "user:q2"},
		},
		{
			name: "a request answered by the reply before it takes that reply",
			msgs: []types.Message{
				{Role: roleUser, Content: "q1"}, {Role: roleAssistant, Content: "a1"},
				{Role: roleAssistant, Content: "sure"},
				{Role: roleUser, Content: "forbidden", Meta: map[string]interface{}{
					types.MetaBlockedInput: true, blockedReplyBeforeMetaKey: true,
				}},
			},
			want: []string{"user:q1", "assistant:a1"},
		},
		{
			name: "a request answered by the reply before it never takes a tool call or a user message",
			msgs: []types.Message{
				{Role: roleUser, Content: "q1"},
				{Role: roleAssistant, ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "t"}}},
				{Role: roleUser, Content: "forbidden", Meta: map[string]interface{}{
					types.MetaBlockedInput: true, blockedReplyBeforeMetaKey: true,
				}},
				{Role: roleUser, Content: "forbidden again", Meta: map[string]interface{}{
					types.MetaBlockedInput: true, blockedReplyBeforeMetaKey: true,
				}},
			},
			want: []string{"user:q1", "assistant:"},
		},
		{
			name: "a marked user message never takes the user message before it",
			msgs: []types.Message{
				{Role: roleUser, Content: "q1"},
				{Role: roleUser, Content: "forbidden", Meta: map[string]interface{}{
					types.MetaBlockedInput: true, blockedReplyBeforeMetaKey: true,
				}},
			},
			want: []string{"user:q1"},
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
	assert.Equal(t, []string{"user:q1", "user:q2"}, roleLines(ExcludeFromModelContext(msgs)))
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

// Relevance truncation scores messages one by one, so it could keep a blocked
// user message and drop the canned reply that marks it; the provider request
// would then carry the blocked request with nothing to say it was blocked.
// Truncation works on the history without blocked turns.
func TestContextBuilder_RelevanceNeverKeepsHalfABlockedTurn(t *testing.T) {
	topic := newMockEmbeddingProvider()
	topic.embedFunc = func(texts []string) ([][]float32, error) {
		out := make([][]float32, len(texts))
		for i, x := range texts {
			out[i] = []float32{0, 1, 0}
			if strings.Contains(x, "apples") {
				out[i] = []float32{1, 0, 0}
			}
		}
		return out, nil
	}
	got := runTurnBoundary(t,
		&ContextBuilderPolicy{TokenBudget: 5, Strategy: TruncateLeastRelevant,
			RelevanceConfig: &RelevanceConfig{EmbeddingProvider: topic, MinRecentMessages: 1}},
		turnMsg(roleUser, "pears question here"),
		turnMsg(roleAssistant, "pears answer here"),
		turnMsg(roleUser, "apples forbidden"),
		markedReply("refused refused refused"),
		turnMsg(roleUser, "apples again"),
	)
	assert.Equal(t, []string{"user:pears question here", "user:apples again"}, got,
		"the budget the blocked turn would have used goes to the rest of the history")
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

// A unary turn a hook blocked without naming a validator is still marked, so
// it is left out with no validation to go on.
func TestProviderStage_BlockedInputIsMarkedWithoutValidation(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{replacement: "Not here."}))
	stage := NewProviderStageWithHooks(providersmock.NewProvider("p", "m", false), nil, nil,
		&ProviderConfig{MaxTokens: 100}, nil, reg)
	user := types.Message{Role: roleUser, Content: "forbidden"}
	out := runTestStage(t, stage, []StreamElement{NewMessageElement(&user)})

	var msgs []types.Message
	for _, e := range out {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	require.Equal(t, []string{"user:forbidden", "assistant:Not here."}, roleLines(msgs))
	assert.Empty(t, msgs[1].Validations)
	assert.Equal(t, true, msgs[1].Meta[types.MetaBlockedInput])
	assert.Empty(t, excludeBlockedInputTurns(msgs), "the pair is left out of model context")
}

// emittedMessages returns the messages of the emitted elements.
func emittedMessages(elems []StreamElement) []types.Message {
	var msgs []types.Message
	for _, e := range elems {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	return msgs
}

// A spoken turn an input hook blocks is replaced by a canned reply marked as a
// blocked input turn, so a later text turn on the stored conversation leaves
// both out.
func TestDuplexHooks_SpokenBlockedInputIsMarked(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	elems, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "a wire transfer please",
		}},
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
	)
	require.NoError(t, err)

	msgs := emittedMessages(elems)
	require.Equal(t, []string{"user:a wire transfer please", "assistant:I can't help with transfers."},
		roleLines(msgs))
	assert.Equal(t, true, msgs[1].Meta[types.MetaBlockedInput])
	assert.Empty(t, excludeBlockedInputTurns(msgs))
}

// A typed duplex turn an input hook blocks, without naming a validator, is
// marked; the reply streaming ahead of it is not.
func TestDuplexHooks_TypedBlockedInputIsMarked(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{replacement: "Not here."}))
	s, sess, _ := duplexStageWithHooks(t, reg)
	s.turnIDQueue = []string{"t1"}
	s.accumulatedText.WriteString("half a reply")

	out := make(chan StreamElement, 16)
	loopDone := make(chan error, 1)
	go func() { loopDone <- s.forwardResponseElements(context.Background(), out) }()
	text := "blocked input"
	require.False(t, s.handleInputElement(context.Background(), &StreamElement{Text: &text}, out, make(chan error, 1)))
	sess.respCh <- stopChunk()
	close(sess.respCh)
	require.NoError(t, <-loopDone)
	close(out)

	var elems []StreamElement
	for e := range out {
		elems = append(elems, e)
	}
	msgs := emittedMessages(elems)
	require.Equal(t, []string{"assistant:half a reply", "user:blocked input", "assistant:Not here."},
		roleLines(msgs))
	assert.Nil(t, msgs[0].Meta[types.MetaBlockedInput])
	assert.Equal(t, true, msgs[2].Meta[types.MetaBlockedInput])
	assert.Equal(t, []string{"assistant:half a reply"}, roleLines(excludeBlockedInputTurns(msgs)))
}

// A spoken input blocked after its reply completed cannot replace the reply,
// which the caller already has. The blocked request is marked as answered by
// the reply before it, so model context leaves out both; the transcript keeps
// both.
func TestDuplexHooks_LateSpokenBlockedInputIsMarked(t *testing.T) {
	s, rec, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	s.session = &interruptingSession{recordingSession: rec}
	elems, err := feedChunks(t, s,
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
		finalTranscript("a wire transfer please"),
	)
	require.NoError(t, err)

	msgs := emittedMessages(elems)
	require.Equal(t, []string{"assistant:Sure", "user:a wire transfer please"}, roleLines(msgs))
	assert.Equal(t, true, msgs[1].Meta[types.MetaBlockedInput])
	assert.Equal(t, true, msgs[1].Meta[blockedReplyBeforeMetaKey])
	assert.Nil(t, msgs[0].Meta[types.MetaBlockedInput], "the emitted reply is not written after the fact")
	assert.Empty(t, excludeBlockedInputTurns(msgs))
}

// With a TranscriptReorderStage the late blocked request is placed ahead of
// the reply it held; the reply is marked instead, and model context still
// leaves out both.
func TestTranscriptReorderStage_LateBlockedRequestMarksTheHeldReply(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)

	prevUser := userElem("hello")
	prevReply := asstEndElem("Hi!")
	reply := asstEndElem("Sure")
	late := lateUserElem("a wire transfer please")
	late.Message.Meta = map[string]interface{}{types.MetaBlockedInput: true, blockedReplyBeforeMetaKey: true}
	original := reply.Message
	in <- prevUser
	in <- prevReply
	in <- reply
	in <- late
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))

	var msgs []types.Message
	for e := range out {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	require.Equal(t, []string{"user:hello", "assistant:Hi!", "user:a wire transfer please", "assistant:Sure"},
		roleLines(msgs))
	assert.Nil(t, msgs[2].Meta[blockedReplyBeforeMetaKey], "the reply is no longer before it")
	assert.Equal(t, true, msgs[3].Meta[types.MetaBlockedInput])
	assert.Nil(t, msgs[1].Meta[types.MetaBlockedInput])
	assert.Nil(t, original.Meta[types.MetaBlockedInput], "the duplex stage's message is not written")
	assert.Equal(t, []string{"user:hello", "assistant:Hi!"}, roleLines(excludeBlockedInputTurns(msgs)))
}

// A late blocked request of a tool turn moves ahead of the held tool round;
// only replies are marked, never the tool call or its result.
func TestTranscriptReorderStage_LateBlockedRequestNeverMarksAToolRound(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 8)
	out := make(chan StreamElement, 16)
	late := lateUserElem("a wire transfer please")
	late.Message.Meta = map[string]interface{}{types.MetaBlockedInput: true, blockedReplyBeforeMetaKey: true}
	in <- toolCallEndElem("c1")
	in <- toolResultElem("c1")
	in <- late
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))

	var msgs []types.Message
	for e := range out {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	require.Len(t, msgs, 3)
	assert.Equal(t, roleUser, msgs[0].Role)
	assert.Nil(t, msgs[1].Meta[types.MetaBlockedInput], "the tool call is not a reply")
	assert.Nil(t, msgs[2].Meta[types.MetaBlockedInput], "the tool result is not a reply")
}

// A late transcript that is not blocked moves ahead of its reply unmarked.
func TestTranscriptReorderStage_LateAllowedRequestLeavesReplyUnmarked(t *testing.T) {
	s := NewTranscriptReorderStage("[none]")
	in := make(chan StreamElement, 4)
	out := make(chan StreamElement, 8)
	in <- asstEndElem("Sure")
	in <- lateUserElem("what time is it")
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))

	var msgs []types.Message
	for e := range out {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	require.Equal(t, []string{"user:what time is it", "assistant:Sure"}, roleLines(msgs))
	assert.Nil(t, msgs[1].Meta[types.MetaBlockedInput])
}

// What a text turn after such a session sends the provider: neither the
// blocked request nor the reply that answered it.
func TestProviderStage_LateBlockedRequestAndItsReplyAreNotSent(t *testing.T) {
	prov := &requestRecordingProvider{Provider: providersmock.NewProvider("p", "m", false)}
	stage := NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100}, nil, nil)
	history := []types.Message{
		{Role: roleUser, Content: "hello"},
		{Role: roleAssistant, Content: "Hi!"},
		{Role: roleAssistant, Content: "Sure"},
		{Role: roleUser, Content: "a wire transfer please", Meta: map[string]interface{}{
			types.MetaBlockedInput: true, blockedReplyBeforeMetaKey: true,
		}},
		{Role: roleUser, Content: "what time is it"},
	}
	elems := make([]StreamElement, len(history))
	for i := range history {
		elems[i] = NewMessageElement(&history[i])
	}
	runTestStage(t, stage, elems)

	require.Len(t, prov.seen, 1)
	assert.Equal(t, []string{"user:hello", "assistant:Hi!", "user:what time is it"}, roleLines(prov.seen[0]))
}

// requestRecordingProvider records the messages of every request.
type requestRecordingProvider struct {
	*providersmock.Provider
	seen [][]types.Message
}

func (p *requestRecordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	return p.Provider.Predict(ctx, req)
}

func (p *requestRecordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	return p.Provider.PredictStream(ctx, req)
}

// A late transcript that is not blocked is not marked.
func TestDuplexHooks_LateSpokenAllowedInputIsNotMarked(t *testing.T) {
	s, rec, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	s.session = &interruptingSession{recordingSession: rec}
	elems, err := feedChunks(t, s,
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
		finalTranscript("what time is it"),
	)
	require.NoError(t, err)

	msgs := emittedMessages(elems)
	require.Equal(t, []string{"assistant:Sure", "user:what time is it"}, roleLines(msgs))
	assert.Nil(t, msgs[1].Meta[types.MetaBlockedInput])
	assert.Len(t, excludeBlockedInputTurns(msgs), 2)
}

// A spoken input block and an output block on the same turn: the reply's
// mark survives the second block.
func TestDuplexHooks_SpokenInputMarkSurvivesALaterOutputBlock(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(&wordInputHook{word: "wire"}),
		hooks.WithProviderHook(denyAllRepliesHook{}),
	))
	elems, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "a wire transfer please",
		}},
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
	)
	require.NoError(t, err)

	msgs := emittedMessages(elems)
	require.Len(t, msgs, 2)
	assert.Equal(t, types.FinishReasonSafety, msgs[1].FinishReason)
	assert.Equal(t, true, msgs[1].Meta[types.MetaBlockedInput])
	assert.Empty(t, excludeBlockedInputTurns(msgs))
}

// A second block on a turn adds its metadata to the first's rather than
// replacing it, so an input block's mark is kept.
func TestDuplexHooks_BlockTurnWithMergesMeta(t *testing.T) {
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry())
	s.blockTurnWith(nil, hooks.DenyWithMetadata("no", map[string]any{"only": "c"}))
	assert.Equal(t, map[string]interface{}{"only": "c"}, s.turnGuard.blockedMeta,
		"a first block's metadata is taken as is")

	s.turnGuard.blockedMeta = withBlockedInputMark(map[string]interface{}{"first": "a"})
	s.blockTurnWith(nil, hooks.DenyWithMetadata("no", map[string]any{"second": "b"}))
	assert.Equal(t, map[string]interface{}{
		types.MetaBlockedInput: true, "first": "a", "second": "b",
	}, s.turnGuard.blockedMeta)
}

// denyAllRepliesHook is an output guardrail that denies every completed reply.
type denyAllRepliesHook struct{}

func (denyAllRepliesHook) Name() string { return "deny_all_replies" }

func (denyAllRepliesHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (denyAllRepliesHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.DenyWithMetadata("no replies", map[string]any{"rule": "replies"})
}

// A duplex reply an output hook denies is replaced, not marked: it stays in
// model context.
func TestDuplexHooks_OutputDenyIsNotMarked(t *testing.T) {
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(denyReplyHook{})))
	elems, err := feedChunks(t, s, transcriptChunk("deny-me", pcm(10)), stopChunk())
	require.NoError(t, err)

	msgs := emittedMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, types.FinishReasonSafety, msgs[0].FinishReason)
	assert.Nil(t, msgs[0].Meta[types.MetaBlockedInput])
	assert.Len(t, excludeBlockedInputTurns(msgs), 1)
}
