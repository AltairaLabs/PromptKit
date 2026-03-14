package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// 4.1 — Save and Resume
// ---------------------------------------------------------------------------

func TestState_SaveAndResume(t *testing.T) {
	store := statestore.NewMemoryStore()
	packPath := writePackFile(t, minimalPackJSON)
	provider := mock.NewProvider("mock-test", "mock-model", false)
	ctx := context.Background()

	// Open a conversation with explicit ID and state store.
	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStateStore(store),
		sdk.WithConversationID("test-1"),
	)
	require.NoError(t, err)

	// Send 2 messages.
	_, err = conv.Send(ctx, "Hello")
	require.NoError(t, err)
	_, err = conv.Send(ctx, "World")
	require.NoError(t, err)

	// Capture message count before close.
	msgsBefore := conv.Messages(ctx)
	require.GreaterOrEqual(t, len(msgsBefore), 4, "should have at least 4 messages (2 user + 2 assistant)")

	// Close the conversation to persist state.
	err = conv.Close()
	require.NoError(t, err)

	// Resume with the same store and ID.
	resumed, err := sdk.Resume("test-1", packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStateStore(store),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resumed.Close() })

	// Verify the full history is restored.
	msgsAfterResume := resumed.Messages(ctx)
	assert.Equal(t, len(msgsBefore), len(msgsAfterResume), "resumed conversation should have same message count")

	// Send another message and verify history grew.
	_, err = resumed.Send(ctx, "After resume")
	require.NoError(t, err)

	msgsAfterSend := resumed.Messages(ctx)
	assert.Greater(t, len(msgsAfterSend), len(msgsAfterResume),
		"message count should increase after sending on resumed conversation")
}

// ---------------------------------------------------------------------------
// 4.2 — Resume with unknown ID
// ---------------------------------------------------------------------------

func TestState_ResumeWithUnknownID(t *testing.T) {
	store := statestore.NewMemoryStore()
	packPath := writePackFile(t, minimalPackJSON)
	provider := mock.NewProvider("mock-test", "mock-model", false)

	_, err := sdk.Resume("nonexistent-id", packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStateStore(store),
	)
	require.Error(t, err)
	// The MemoryStore returns statestore.ErrNotFound which Resume wraps.
	// Check both the store-level sentinel and the SDK-level sentinel.
	assert.True(t,
		errors.Is(err, sdk.ErrConversationNotFound) || errors.Is(err, statestore.ErrNotFound),
		"expected ErrConversationNotFound or ErrNotFound, got: %v", err,
	)
}

// ---------------------------------------------------------------------------
// 4.3 — Concurrent conversation isolation
// ---------------------------------------------------------------------------

func TestState_ConcurrentConversationIsolation(t *testing.T) {
	store := statestore.NewMemoryStore()
	packPath := writePackFile(t, minimalPackJSON)
	ctx := context.Background()

	const numConvs = 5

	// Open 5 conversations concurrently, each with a unique ID and message.
	var wg sync.WaitGroup
	errs := make([]error, numConvs)

	for i := 0; i < numConvs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			convID := fmt.Sprintf("conv-%d", idx)
			provider := mock.NewProvider("mock-test", "mock-model", false)

			conv, openErr := sdk.Open(packPath, "chat",
				sdk.WithProvider(provider),
				sdk.WithSkipSchemaValidation(),
				sdk.WithStateStore(store),
				sdk.WithConversationID(convID),
			)
			if openErr != nil {
				errs[idx] = openErr
				return
			}

			msg := fmt.Sprintf("Unique message from conversation %d", idx)
			if _, sendErr := conv.Send(ctx, msg); sendErr != nil {
				errs[idx] = sendErr
				return
			}

			errs[idx] = conv.Close()
		}(i)
	}
	wg.Wait()

	// Verify no errors occurred.
	for i, e := range errs {
		require.NoError(t, e, "conversation %d had error", i)
	}

	// Resume each and verify isolation.
	for i := 0; i < numConvs; i++ {
		convID := fmt.Sprintf("conv-%d", i)
		provider := mock.NewProvider("mock-test", "mock-model", false)

		resumed, err := sdk.Resume(convID, packPath, "chat",
			sdk.WithProvider(provider),
			sdk.WithSkipSchemaValidation(),
			sdk.WithStateStore(store),
		)
		require.NoError(t, err, "resume conv-%d", i)

		msgs := resumed.Messages(ctx)
		// Should have exactly 1 user message + 1 assistant response = 2 messages.
		assert.GreaterOrEqual(t, len(msgs), 2,
			"conv-%d should have at least 2 messages", i)

		// Verify the user message belongs to this conversation.
		foundOwnMessage := false
		expectedContent := fmt.Sprintf("Unique message from conversation %d", i)
		for _, m := range msgs {
			if m.Role == "user" && m.GetContent() == expectedContent {
				foundOwnMessage = true
				break
			}
		}
		assert.True(t, foundOwnMessage,
			"conv-%d should contain its own message %q", i, expectedContent)

		// Verify no messages from other conversations leaked in.
		for _, m := range msgs {
			if m.Role == "user" {
				for j := 0; j < numConvs; j++ {
					if j == i {
						continue
					}
					otherContent := fmt.Sprintf("Unique message from conversation %d", j)
					assert.NotEqual(t, otherContent, m.GetContent(),
						"conv-%d should not contain messages from conv-%d", i, j)
				}
			}
		}

		require.NoError(t, resumed.Close())
	}
}

// ---------------------------------------------------------------------------
// 4.4 — Fork
// ---------------------------------------------------------------------------

func TestState_Fork(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	conv := openTestConv(t,
		sdk.WithStateStore(store),
		sdk.WithConversationID("fork-original"),
	)

	// Send 2 messages to establish history.
	_, err := conv.Send(ctx, "First message")
	require.NoError(t, err)
	_, err = conv.Send(ctx, "Second message")
	require.NoError(t, err)

	originalMsgs := conv.Messages(ctx)
	require.GreaterOrEqual(t, len(originalMsgs), 4, "should have at least 4 messages")

	// Fork the conversation.
	forked, err := conv.Fork()
	require.NoError(t, err)
	t.Cleanup(func() { _ = forked.Close() })

	// Verify forked conversation has the same history.
	forkedMsgs := forked.Messages(ctx)
	assert.Equal(t, len(originalMsgs), len(forkedMsgs),
		"forked conversation should have same message count as original")

	// Verify the forked conversation has a different ID.
	assert.NotEqual(t, conv.ID(), forked.ID(),
		"forked conversation should have a different ID")

	// Send different messages to original and fork.
	_, err = conv.Send(ctx, "Original continues")
	require.NoError(t, err)
	_, err = forked.Send(ctx, "Fork diverges")
	require.NoError(t, err)

	// Verify histories diverged.
	originalAfter := conv.Messages(ctx)
	forkedAfter := forked.Messages(ctx)

	// Both should have grown by 2 (user + assistant).
	assert.Greater(t, len(originalAfter), len(originalMsgs),
		"original should have more messages after sending")
	assert.Greater(t, len(forkedAfter), len(forkedMsgs),
		"fork should have more messages after sending")

	// The last user message in each should be different.
	var lastOriginalUser, lastForkedUser string
	for i := len(originalAfter) - 1; i >= 0; i-- {
		if originalAfter[i].Role == "user" {
			lastOriginalUser = originalAfter[i].GetContent()
			break
		}
	}
	for i := len(forkedAfter) - 1; i >= 0; i-- {
		if forkedAfter[i].Role == "user" {
			lastForkedUser = forkedAfter[i].GetContent()
			break
		}
	}

	assert.Equal(t, "Original continues", lastOriginalUser)
	assert.Equal(t, "Fork diverges", lastForkedUser)
}
