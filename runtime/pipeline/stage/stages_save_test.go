package stage

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// IncrementalSaveStage Tests
// =============================================================================

func TestIncrementalSaveStage_WithAppender(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Pre-populate with existing messages
	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-123",
		Messages: []types.Message{
			{Role: "user", Content: "Old message 1"},
			{Role: "assistant", Content: "Old message 2"},
		},
	})
	require.NoError(t, err)

	config := &IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-123",
		},
	}

	s := NewIncrementalSaveStage(config)

	// Send a mix of history messages (Source="statestore") and new messages
	historyMsg1 := types.Message{Role: "user", Content: "Old message 1", Source: "statestore"}
	historyMsg2 := types.Message{Role: "assistant", Content: "Old message 2", Source: "statestore"}
	newMsg1 := types.Message{Role: "user", Content: "New question"}
	newMsg2 := types.Message{Role: "assistant", Content: "New answer"}

	inputs := []StreamElement{
		NewMessageElement(&historyMsg1),
		NewMessageElement(&historyMsg2),
		NewMessageElement(&newMsg1),
		NewMessageElement(&newMsg2),
	}

	results := runTestStage(t, s, inputs)

	// All four elements should be forwarded
	require.Len(t, results, 4)

	// Verify only new messages were appended (not history)
	state, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)

	// Original 2 + appended 2 = 4
	require.Len(t, state.Messages, 4)
	assert.Equal(t, "Old message 1", state.Messages[0].Content)
	assert.Equal(t, "Old message 2", state.Messages[1].Content)
	assert.Equal(t, "New question", state.Messages[2].Content)
	assert.Equal(t, "New answer", state.Messages[3].Content)
}

func TestIncrementalSaveStage_FallbackToFullSave(t *testing.T) {
	store := newMinimalStore()
	ctx := context.Background()

	// Pre-populate
	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-123",
		Messages: []types.Message{
			{Role: "user", Content: "Old message"},
		},
	})
	require.NoError(t, err)

	config := &IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-123",
		},
	}

	s := NewIncrementalSaveStage(config)

	// Send history + new messages
	historyMsg := types.Message{Role: "user", Content: "Old message", Source: "statestore"}
	newMsg := types.Message{Role: "assistant", Content: "New response"}

	inputs := []StreamElement{
		NewMessageElement(&historyMsg),
		NewMessageElement(&newMsg),
	}

	results := runTestStage(t, s, inputs)

	// Both elements should be forwarded
	require.Len(t, results, 2)

	// Verify full save was done (minimalStore doesn't implement MessageAppender)
	state, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)

	// Full save replaces all messages with what was collected
	require.Len(t, state.Messages, 2)
	assert.Equal(t, "Old message", state.Messages[0].Content)
	assert.Equal(t, "New response", state.Messages[1].Content)
}

func TestIncrementalSaveStage_ErrorForwarding(t *testing.T) {
	store := statestore.NewMemoryStore()

	config := &IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-123",
		},
	}

	s := NewIncrementalSaveStage(config)

	testErr := errors.New("something went wrong")

	input := make(chan StreamElement, 10)
	output := make(chan StreamElement, 10)

	// Send a message followed by an error element followed by another message
	msg1 := types.Message{Role: "user", Content: "Hello"}
	input <- NewMessageElement(&msg1)
	input <- NewErrorElement(testErr)
	msg2 := types.Message{Role: "assistant", Content: "World"}
	input <- NewMessageElement(&msg2)
	close(input)

	ctx := context.Background()
	err := s.Process(ctx, input, output)
	require.NoError(t, err)

	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	// Should have 3 elements: message, error, message
	require.Len(t, results, 3)

	// First element is a message
	assert.NotNil(t, results[0].Message)
	assert.Equal(t, "Hello", results[0].Message.Content)

	// Second element is the error
	assert.NotNil(t, results[1].Error)
	assert.Equal(t, "something went wrong", results[1].Error.Error())

	// Third element is a message
	assert.NotNil(t, results[2].Message)
	assert.Equal(t, "World", results[2].Message.Content)
}

func TestIncrementalSaveStage_NilConfig(t *testing.T) {
	s := NewIncrementalSaveStage(nil)

	inputs := []StreamElement{
		newTestMsgElement("user", "Message"),
	}

	results := runTestStage(t, s, inputs)

	// Should just forward without saving
	require.Len(t, results, 1)
	assert.Equal(t, "Message", results[0].Message.Content)
}

func TestIncrementalSaveStage_NoNewMessages(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Pre-populate
	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-123",
		Messages: []types.Message{
			{Role: "user", Content: "History 1"},
			{Role: "assistant", Content: "History 2"},
		},
	})
	require.NoError(t, err)

	config := &IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-123",
		},
	}

	s := NewIncrementalSaveStage(config)

	// All messages are from history (Source="statestore")
	msg1 := types.Message{Role: "user", Content: "History 1", Source: "statestore"}
	msg2 := types.Message{Role: "assistant", Content: "History 2", Source: "statestore"}

	inputs := []StreamElement{
		NewMessageElement(&msg1),
		NewMessageElement(&msg2),
	}

	results := runTestStage(t, s, inputs)

	// Elements should still be forwarded
	require.Len(t, results, 2)

	// Verify the store still has just the original 2 messages (nothing appended)
	state, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)
	assert.Len(t, state.Messages, 2)
	assert.Equal(t, "History 1", state.Messages[0].Content)
	assert.Equal(t, "History 2", state.Messages[1].Content)
}

func TestIncrementalSaveStage_FiltersSummaryAndRetrievedSources(t *testing.T) {
	store := statestore.NewMemoryStore()

	config := &IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-123",
		},
	}

	s := NewIncrementalSaveStage(config)

	// Messages with various Source values that should be filtered
	summaryMsg := types.Message{Role: "system", Content: "Summary text", Source: "summary"}
	retrievedMsg := types.Message{Role: "user", Content: "Retrieved text", Source: "retrieved"}
	historyMsg := types.Message{Role: "user", Content: "History", Source: "statestore"}
	newMsg := types.Message{Role: "user", Content: "New question"}

	inputs := []StreamElement{
		NewMessageElement(&summaryMsg),
		NewMessageElement(&retrievedMsg),
		NewMessageElement(&historyMsg),
		NewMessageElement(&newMsg),
	}

	results := runTestStage(t, s, inputs)

	// All 4 elements should be forwarded
	require.Len(t, results, 4)

	// Only the new message (no Source) should be saved
	state, err := store.Load(context.Background(), "conv-123")
	require.NoError(t, err)
	require.Len(t, state.Messages, 1)
	assert.Equal(t, "New question", state.Messages[0].Content)
}

func TestIncrementalSaveStage_AppendCreatesConversation(t *testing.T) {
	store := statestore.NewMemoryStore()

	// Don't pre-populate — conversation doesn't exist yet

	config := &IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "new-conv",
		},
	}

	s := NewIncrementalSaveStage(config)

	msg := types.Message{Role: "user", Content: "First message"}
	inputs := []StreamElement{
		NewMessageElement(&msg),
	}

	results := runTestStage(t, s, inputs)

	require.Len(t, results, 1)

	// Verify conversation was created via AppendMessages
	ctx := context.Background()
	count, err := store.MessageCount(ctx, "new-conv")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	msgs, err := store.LoadRecentMessages(ctx, "new-conv", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "First message", msgs[0].Content)
	assert.Equal(t, fmt.Sprintf("%s", "user"), msgs[0].Role)
}

// =============================================================================
// Mock types for indexing and summarization tests
// =============================================================================

// mockMessageIndexForSave tracks Index calls for testing the indexNewMessages path.
type mockMessageIndexForSave struct {
	indexCalls []indexCall
}

type indexCall struct {
	convID    string
	turnIndex int
	message   types.Message
}

func (m *mockMessageIndexForSave) Index(
	_ context.Context, conversationID string, turnIndex int, message types.Message,
) error {
	m.indexCalls = append(m.indexCalls, indexCall{
		convID:    conversationID,
		turnIndex: turnIndex,
		message:   message,
	})
	return nil
}

func (m *mockMessageIndexForSave) Search(
	_ context.Context, _ string, _ string, _ int,
) ([]statestore.IndexResult, error) {
	return nil, nil
}

func (m *mockMessageIndexForSave) Delete(_ context.Context, _ string) error {
	return nil
}

// mockSummarizerForSave tracks Summarize calls for testing the maybeSummarize path.
type mockSummarizerForSave struct {
	summarizeCalls int
	summaryContent string
}

func (m *mockSummarizerForSave) Summarize(
	_ context.Context, _ []types.Message,
) (string, error) {
	m.summarizeCalls++
	return m.summaryContent, nil
}

// =============================================================================
// Additional IncrementalSaveStage Tests
// =============================================================================

func TestIncrementalSaveStage_WithIndexing(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Pre-populate with existing messages so MessageCount returns a valid base.
	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-idx",
		Messages: []types.Message{
			{Role: "user", Content: "Old message"},
		},
	})
	require.NoError(t, err)

	mockIndex := &mockMessageIndexForSave{}

	config := &IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-idx",
		},
		MessageIndex: mockIndex,
	}

	s := NewIncrementalSaveStage(config)

	// Send one history message and two new messages
	historyMsg := types.Message{Role: "user", Content: "Old message", Source: "statestore"}
	newMsg1 := types.Message{Role: "user", Content: "New question"}
	newMsg2 := types.Message{Role: "assistant", Content: "New answer"}

	inputs := []StreamElement{
		NewMessageElement(&historyMsg),
		NewMessageElement(&newMsg1),
		NewMessageElement(&newMsg2),
	}

	results := runTestStage(t, s, inputs)

	// All three elements should be forwarded
	require.Len(t, results, 3)

	// Verify Index was called for each new message (2 calls)
	require.Len(t, mockIndex.indexCalls, 2)

	// After appending 2 messages, the store has 3 messages total (1 old + 2 new).
	// baseIndex = count - len(newMessages) = 3 - 2 = 1
	assert.Equal(t, "conv-idx", mockIndex.indexCalls[0].convID)
	assert.Equal(t, 1, mockIndex.indexCalls[0].turnIndex)
	assert.Equal(t, "New question", mockIndex.indexCalls[0].message.Content)

	assert.Equal(t, "conv-idx", mockIndex.indexCalls[1].convID)
	assert.Equal(t, 2, mockIndex.indexCalls[1].turnIndex)
	assert.Equal(t, "New answer", mockIndex.indexCalls[1].message.Content)
}

func TestIncrementalSaveStage_WithAutoSummarize(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Pre-populate with enough messages to exceed the summarize threshold.
	// We'll have 4 existing messages + 2 new = 6 total, threshold = 3, batch = 3.
	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-sum",
		Messages: []types.Message{
			{Role: "user", Content: "Message 1"},
			{Role: "assistant", Content: "Message 2"},
			{Role: "user", Content: "Message 3"},
			{Role: "assistant", Content: "Message 4"},
		},
	})
	require.NoError(t, err)

	mockSummarizer := &mockSummarizerForSave{
		summaryContent: "This is a summary of the conversation.",
	}

	config := &IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-sum",
		},
		Summarizer:         mockSummarizer,
		SummarizeThreshold: 3, // Trigger when > 3 messages
		SummarizeBatchSize: 3, // Summarize 3 messages at a time
	}

	s := NewIncrementalSaveStage(config)

	// Send history messages (sourced from statestore) and new messages
	hist1 := types.Message{Role: "user", Content: "Message 1", Source: "statestore"}
	hist2 := types.Message{Role: "assistant", Content: "Message 2", Source: "statestore"}
	hist3 := types.Message{Role: "user", Content: "Message 3", Source: "statestore"}
	hist4 := types.Message{Role: "assistant", Content: "Message 4", Source: "statestore"}
	newMsg1 := types.Message{Role: "user", Content: "Message 5"}
	newMsg2 := types.Message{Role: "assistant", Content: "Message 6"}

	inputs := []StreamElement{
		NewMessageElement(&hist1),
		NewMessageElement(&hist2),
		NewMessageElement(&hist3),
		NewMessageElement(&hist4),
		NewMessageElement(&newMsg1),
		NewMessageElement(&newMsg2),
	}

	results := runTestStage(t, s, inputs)

	// All 6 elements should be forwarded
	require.Len(t, results, 6)

	// Verify Summarize was called
	assert.Equal(t, 1, mockSummarizer.summarizeCalls)

	// Verify summary was saved to the store
	summaries, err := store.LoadSummaries(ctx, "conv-sum")
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, "This is a summary of the conversation.", summaries[0].Content)
	assert.Equal(t, 0, summaries[0].StartTurn)
	assert.Equal(t, 3, summaries[0].EndTurn)
}

// =============================================================================
// maybeSummarize unit tests
// =============================================================================

// noReaderStore implements MessageAppender but NOT MessageReader.
// Used to test the "store doesn't implement MessageReader" branch.
type noReaderStore struct {
	messages map[string][]types.Message
}

func newNoReaderStore() *noReaderStore {
	return &noReaderStore{messages: make(map[string][]types.Message)}
}

func (s *noReaderStore) AppendMessages(_ context.Context, id string, msgs []types.Message) error {
	s.messages[id] = append(s.messages[id], msgs...)
	return nil
}

// readerOnlyStore implements MessageAppender and MessageReader but NOT SummaryAccessor or Store.
type readerOnlyStore struct {
	messages     map[string][]types.Message
	countErr     error
	countOverride int
}

func newReaderOnlyStore() *readerOnlyStore {
	return &readerOnlyStore{messages: make(map[string][]types.Message)}
}

func (s *readerOnlyStore) AppendMessages(_ context.Context, id string, msgs []types.Message) error {
	s.messages[id] = append(s.messages[id], msgs...)
	return nil
}

func (s *readerOnlyStore) LoadRecentMessages(_ context.Context, id string, n int) ([]types.Message, error) {
	msgs := s.messages[id]
	if n > len(msgs) {
		n = len(msgs)
	}
	return msgs[len(msgs)-n:], nil
}

func (s *readerOnlyStore) MessageCount(_ context.Context, id string) (int, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	if s.countOverride > 0 {
		return s.countOverride, nil
	}
	return len(s.messages[id]), nil
}

// readerWithSummaryStore implements MessageAppender, MessageReader, SummaryAccessor, and Store.
type readerWithSummaryStore struct {
	messages       map[string][]types.Message
	states         map[string]*statestore.ConversationState
	summaries      map[string][]statestore.Summary
	countOverride  int
	loadSumErr     error
	loadErr        error
	saveSummaryErr error
}

func newReaderWithSummaryStore() *readerWithSummaryStore {
	return &readerWithSummaryStore{
		messages:  make(map[string][]types.Message),
		states:    make(map[string]*statestore.ConversationState),
		summaries: make(map[string][]statestore.Summary),
	}
}

func (s *readerWithSummaryStore) AppendMessages(_ context.Context, id string, msgs []types.Message) error {
	s.messages[id] = append(s.messages[id], msgs...)
	// Also update the state for Load
	state, ok := s.states[id]
	if !ok {
		state = &statestore.ConversationState{ID: id}
		s.states[id] = state
	}
	state.Messages = append(state.Messages, msgs...)
	return nil
}

func (s *readerWithSummaryStore) LoadRecentMessages(_ context.Context, id string, n int) ([]types.Message, error) {
	msgs := s.messages[id]
	if n > len(msgs) {
		n = len(msgs)
	}
	return msgs[len(msgs)-n:], nil
}

func (s *readerWithSummaryStore) MessageCount(_ context.Context, id string) (int, error) {
	if s.countOverride > 0 {
		return s.countOverride, nil
	}
	return len(s.messages[id]), nil
}

func (s *readerWithSummaryStore) LoadSummaries(_ context.Context, id string) ([]statestore.Summary, error) {
	if s.loadSumErr != nil {
		return nil, s.loadSumErr
	}
	return s.summaries[id], nil
}

func (s *readerWithSummaryStore) SaveSummary(_ context.Context, id string, summary statestore.Summary) error {
	if s.saveSummaryErr != nil {
		return s.saveSummaryErr
	}
	s.summaries[id] = append(s.summaries[id], summary)
	return nil
}

func (s *readerWithSummaryStore) Load(_ context.Context, id string) (*statestore.ConversationState, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	state, ok := s.states[id]
	if !ok {
		return nil, statestore.ErrNotFound
	}
	return state, nil
}

func (s *readerWithSummaryStore) Save(_ context.Context, state *statestore.ConversationState) error {
	s.states[state.ID] = state
	return nil
}

func (s *readerWithSummaryStore) Fork(_ context.Context, sourceID, newID string) error {
	state, ok := s.states[sourceID]
	if !ok {
		return statestore.ErrNotFound
	}
	copied := *state
	copied.ID = newID
	s.states[newID] = &copied
	return nil
}

// noStoreInterfaceSummaryStore implements MessageAppender, MessageReader, SummaryAccessor
// but NOT Store. Used to test the "store doesn't implement Store" branch after LoadSummaries.
type noStoreInterfaceSummaryStore struct {
	messages      map[string][]types.Message
	summaries     map[string][]statestore.Summary
	countOverride int
}

func newNoStoreInterfaceSummaryStore() *noStoreInterfaceSummaryStore {
	return &noStoreInterfaceSummaryStore{
		messages:  make(map[string][]types.Message),
		summaries: make(map[string][]statestore.Summary),
	}
}

func (s *noStoreInterfaceSummaryStore) AppendMessages(_ context.Context, id string, msgs []types.Message) error {
	s.messages[id] = append(s.messages[id], msgs...)
	return nil
}

func (s *noStoreInterfaceSummaryStore) LoadRecentMessages(
	_ context.Context, id string, n int,
) ([]types.Message, error) {
	msgs := s.messages[id]
	if n > len(msgs) {
		n = len(msgs)
	}
	return msgs[len(msgs)-n:], nil
}

func (s *noStoreInterfaceSummaryStore) MessageCount(_ context.Context, id string) (int, error) {
	if s.countOverride > 0 {
		return s.countOverride, nil
	}
	return len(s.messages[id]), nil
}

func (s *noStoreInterfaceSummaryStore) LoadSummaries(_ context.Context, id string) ([]statestore.Summary, error) {
	return s.summaries[id], nil
}

func (s *noStoreInterfaceSummaryStore) SaveSummary(
	_ context.Context, id string, summary statestore.Summary,
) error {
	s.summaries[id] = append(s.summaries[id], summary)
	return nil
}

// failingSummarizerForSave returns an error from Summarize.
type failingSummarizerForSave struct{}

func (f *failingSummarizerForSave) Summarize(_ context.Context, _ []types.Message) (string, error) {
	return "", errors.New("summarizer failed")
}

// helper to build an IncrementalSaveStage and call maybeSummarize directly.
func callMaybeSummarize(t *testing.T, store interface{}, summarizer statestore.Summarizer, threshold, batch int) {
	t.Helper()
	config := &IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-ms",
		},
		Summarizer:         summarizer,
		SummarizeThreshold: threshold,
		SummarizeBatchSize: batch,
	}
	s := NewIncrementalSaveStage(config)
	s.maybeSummarize(context.Background(), "conv-ms")
}

func TestMaybeSummarize_StoreNotMessageReader(t *testing.T) {
	// noReaderStore does not implement MessageReader
	store := newNoReaderStore()
	summarizer := &mockSummarizerForSave{summaryContent: "summary"}
	callMaybeSummarize(t, store, summarizer, 3, 3)
	// Should return early — summarizer never called
	assert.Equal(t, 0, summarizer.summarizeCalls)
}

func TestMaybeSummarize_MessageCountError(t *testing.T) {
	store := newReaderOnlyStore()
	store.countErr = errors.New("count error")
	summarizer := &mockSummarizerForSave{summaryContent: "summary"}
	callMaybeSummarize(t, store, summarizer, 3, 3)
	assert.Equal(t, 0, summarizer.summarizeCalls)
}

func TestMaybeSummarize_CountBelowThreshold(t *testing.T) {
	store := newReaderOnlyStore()
	// 2 messages, threshold 3 → no summarization
	store.messages["conv-ms"] = []types.Message{
		{Role: "user", Content: "m1"},
		{Role: "assistant", Content: "m2"},
	}
	summarizer := &mockSummarizerForSave{summaryContent: "summary"}
	callMaybeSummarize(t, store, summarizer, 3, 3)
	assert.Equal(t, 0, summarizer.summarizeCalls)
}

func TestMaybeSummarize_StoreNotSummaryAccessor(t *testing.T) {
	store := newReaderOnlyStore()
	// Enough messages to pass the threshold
	store.countOverride = 10
	summarizer := &mockSummarizerForSave{summaryContent: "summary"}
	callMaybeSummarize(t, store, summarizer, 3, 3)
	// readerOnlyStore does not implement SummaryAccessor → early return
	assert.Equal(t, 0, summarizer.summarizeCalls)
}

func TestMaybeSummarize_LoadSummariesError(t *testing.T) {
	store := newReaderWithSummaryStore()
	store.countOverride = 10
	store.loadSumErr = errors.New("load summaries error")
	summarizer := &mockSummarizerForSave{summaryContent: "summary"}
	callMaybeSummarize(t, store, summarizer, 3, 3)
	assert.Equal(t, 0, summarizer.summarizeCalls)
}

func TestMaybeSummarize_UnsummarizedLessThanBatch(t *testing.T) {
	store := newReaderWithSummaryStore()
	// 5 messages total, batch = 6 → unsummarized (5) < batch (6) → skip
	store.messages["conv-ms"] = make([]types.Message, 5)
	store.states["conv-ms"] = &statestore.ConversationState{
		ID:       "conv-ms",
		Messages: make([]types.Message, 5),
	}
	summarizer := &mockSummarizerForSave{summaryContent: "summary"}
	callMaybeSummarize(t, store, summarizer, 3, 6)
	assert.Equal(t, 0, summarizer.summarizeCalls)
}

func TestMaybeSummarize_StoreLoadFails(t *testing.T) {
	// Use noStoreInterfaceSummaryStore which doesn't implement Store
	store := newNoStoreInterfaceSummaryStore()
	store.countOverride = 10
	summarizer := &mockSummarizerForSave{summaryContent: "summary"}
	callMaybeSummarize(t, store, summarizer, 3, 3)
	// Store doesn't implement statestore.Store → early return at store.Load
	assert.Equal(t, 0, summarizer.summarizeCalls)
}

func TestMaybeSummarize_StoreLoadReturnsError(t *testing.T) {
	store := newReaderWithSummaryStore()
	store.countOverride = 10
	store.loadErr = errors.New("load error")
	summarizer := &mockSummarizerForSave{summaryContent: "summary"}
	callMaybeSummarize(t, store, summarizer, 3, 3)
	assert.Equal(t, 0, summarizer.summarizeCalls)
}

func TestMaybeSummarize_SummarizeFails(t *testing.T) {
	store := newReaderWithSummaryStore()
	// Put 10 messages in the store
	msgs := make([]types.Message, 10)
	for i := range msgs {
		msgs[i] = types.Message{Role: "user", Content: fmt.Sprintf("msg %d", i)}
	}
	store.messages["conv-ms"] = msgs
	store.states["conv-ms"] = &statestore.ConversationState{
		ID:       "conv-ms",
		Messages: msgs,
	}
	summarizer := &failingSummarizerForSave{}
	callMaybeSummarize(t, store, summarizer, 3, 5)
	// Summarize fails → no summary saved
	assert.Empty(t, store.summaries["conv-ms"])
}

func TestMaybeSummarize_SaveSummaryFails(t *testing.T) {
	store := newReaderWithSummaryStore()
	msgs := make([]types.Message, 10)
	for i := range msgs {
		msgs[i] = types.Message{Role: "user", Content: fmt.Sprintf("msg %d", i)}
	}
	store.messages["conv-ms"] = msgs
	store.states["conv-ms"] = &statestore.ConversationState{
		ID:       "conv-ms",
		Messages: msgs,
	}
	store.saveSummaryErr = errors.New("save summary failed")
	summarizer := &mockSummarizerForSave{summaryContent: "good summary"}
	callMaybeSummarize(t, store, summarizer, 3, 5)
	// Summarizer was called but save failed
	assert.Equal(t, 1, summarizer.summarizeCalls)
	assert.Empty(t, store.summaries["conv-ms"])
}

func TestMaybeSummarize_SuccessfulSummarization(t *testing.T) {
	store := newReaderWithSummaryStore()
	msgs := make([]types.Message, 10)
	for i := range msgs {
		msgs[i] = types.Message{Role: "user", Content: fmt.Sprintf("msg %d", i)}
	}
	store.messages["conv-ms"] = msgs
	store.states["conv-ms"] = &statestore.ConversationState{
		ID:       "conv-ms",
		Messages: msgs,
	}
	summarizer := &mockSummarizerForSave{summaryContent: "batch summary"}
	callMaybeSummarize(t, store, summarizer, 3, 5)
	assert.Equal(t, 1, summarizer.summarizeCalls)
	require.Len(t, store.summaries["conv-ms"], 1)
	assert.Equal(t, "batch summary", store.summaries["conv-ms"][0].Content)
	assert.Equal(t, 0, store.summaries["conv-ms"][0].StartTurn)
	assert.Equal(t, 5, store.summaries["conv-ms"][0].EndTurn)
}

func TestMaybeSummarize_WithExistingSummaries(t *testing.T) {
	store := newReaderWithSummaryStore()
	msgs := make([]types.Message, 12)
	for i := range msgs {
		msgs[i] = types.Message{Role: "user", Content: fmt.Sprintf("msg %d", i)}
	}
	store.messages["conv-ms"] = msgs
	store.states["conv-ms"] = &statestore.ConversationState{
		ID:       "conv-ms",
		Messages: msgs,
	}
	// Existing summary covers turns 0-4
	store.summaries["conv-ms"] = []statestore.Summary{
		{StartTurn: 0, EndTurn: 4, Content: "old summary"},
	}
	summarizer := &mockSummarizerForSave{summaryContent: "new batch summary"}
	callMaybeSummarize(t, store, summarizer, 3, 5)
	assert.Equal(t, 1, summarizer.summarizeCalls)
	require.Len(t, store.summaries["conv-ms"], 2)
	// New summary should start from turn 4 (lastSummarizedTurn) and cover 5 messages
	assert.Equal(t, 4, store.summaries["conv-ms"][1].StartTurn)
	assert.Equal(t, 9, store.summaries["conv-ms"][1].EndTurn)
	assert.Equal(t, "new batch summary", store.summaries["conv-ms"][1].Content)
}

func TestIncrementalSaveStage_Passthrough_WithErrors(t *testing.T) {
	// nil config triggers passthrough mode
	s := NewIncrementalSaveStage(nil)

	testErr := errors.New("passthrough error")

	input := make(chan StreamElement, 10)
	output := make(chan StreamElement, 10)

	// Send a message, then an error, then another message
	msg1 := types.Message{Role: "user", Content: "Before error"}
	input <- NewMessageElement(&msg1)
	input <- NewErrorElement(testErr)
	msg2 := types.Message{Role: "assistant", Content: "After error"}
	input <- NewMessageElement(&msg2)
	close(input)

	ctx := context.Background()
	err := s.Process(ctx, input, output)
	require.NoError(t, err)

	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	// Should have 3 elements: message, error, message
	require.Len(t, results, 3)

	// First element is a message
	assert.NotNil(t, results[0].Message)
	assert.Equal(t, "Before error", results[0].Message.Content)

	// Second element is the error — forwarded unconditionally
	assert.NotNil(t, results[1].Error)
	assert.Equal(t, "passthrough error", results[1].Error.Error())

	// Third element is a message
	assert.NotNil(t, results[2].Message)
	assert.Equal(t, "After error", results[2].Message.Content)
}
