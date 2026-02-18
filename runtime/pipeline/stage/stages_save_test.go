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
