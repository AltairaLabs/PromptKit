package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/AltairaLabs/PromptKit/runtime/memory/corpus"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func groundingCorpus() *corpus.Retriever {
	return corpus.New([]corpus.Document{
		{ID: "refunds", Title: "Refund policy", Text: "Refunds are issued within 14 days."},
	})
}

// TestWithRetriever_GroundsWithoutMemory is the point of the standalone option:
// grounding needs a retriever and nothing else. No store, no scope, no memory
// capability.
func TestWithRetriever_GroundsWithoutMemory(t *testing.T) {
	rec := newRecordingProvider()
	conv := openTestConvWithPack(t, memoryContextPack("grounding:{{memory_context}}"), "chat",
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		sdk.WithRetriever(groundingCorpus()),
	)

	_, err := conv.Send(context.Background(), "how long do refunds take?")
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "Refunds are issued within 14 days.")
	assert.NotContains(t, rec.system(), "{{memory_context}}")
}

// TestWithRetriever_RegistersNoMemoryTools pins that grounding alone does not
// hand the model memory tools — the two paths stay separate.
//
// The memory-configured conversation is the control: without it, a mistyped
// tool name would report both conversations tool-free and the assertion would
// hold for the wrong reason.
func TestWithRetriever_RegistersNoMemoryTools(t *testing.T) {
	toolNames := []string{memory.RecallToolName, memory.RememberToolName}

	grounded := openTestConvWithPack(t, memoryContextPack("grounding:{{memory_context}}"), "chat",
		sdk.WithSkipSchemaValidation(),
		sdk.WithRetriever(groundingCorpus()),
	)
	remembering := openTestConvWithPack(t, memoryContextPack("grounding:{{memory_context}}"), "chat",
		sdk.WithSkipSchemaValidation(),
		sdk.WithMemory(memory.NewInMemoryStore(), map[string]string{"user_id": "u1"}),
	)

	for _, name := range toolNames {
		assert.Nil(t, grounded.ToolRegistry().Get(name),
			"want %s unregistered when only a retriever is configured", name)

		registered := remembering.ToolRegistry().Get(name)
		require.NotNil(t, registered, "control: %s must register under WithMemory", name)
		assert.Equal(t, name, registered.Name,
			"control: the registry lookup must resolve the tool it was asked for")
	}
}

// TestWithRetrievalFormatter_ReachesSystemPrompt covers the formatter on the
// standalone path — WithMemoryContextFormatter is a MemoryOption and cannot
// reach a conversation configured without the memory capability.
func TestWithRetrievalFormatter_ReachesSystemPrompt(t *testing.T) {
	rec := newRecordingProvider()
	conv := openTestConvWithPack(t, memoryContextPack("grounding:{{memory_context}}"), "chat",
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		sdk.WithRetriever(groundingCorpus()),
		sdk.WithRetrievalFormatter(func(m []*memory.Memory) string {
			return "SOURCE(" + m[0].ID + "): " + m[0].Content
		}),
	)

	_, err := conv.Send(context.Background(), "how long do refunds take?")
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "SOURCE(refunds): Refunds are issued within 14 days.")
	assert.NotContains(t, rec.system(), "confidence")
}

// TestWithRetriever_OverridesMemoryCapabilityRetriever pins precedence when a
// host configures both: the explicit standalone retriever wins.
func TestWithRetriever_OverridesMemoryCapabilityRetriever(t *testing.T) {
	rec := newRecordingProvider()
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}

	capabilityRetriever := corpus.New([]corpus.Document{
		{ID: "wrong", Title: "Wrong", Text: "Refunds are never issued."},
	})

	conv := openTestConvWithPack(t, memoryContextPack("grounding:{{memory_context}}"), "chat",
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		sdk.WithMemory(store, scope, sdk.WithMemoryRetriever(capabilityRetriever)),
		sdk.WithRetriever(groundingCorpus()),
	)

	_, err := conv.Send(context.Background(), "how long do refunds take?")
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "Refunds are issued within 14 days.")
	assert.NotContains(t, rec.system(), "never issued")
}

// TestWithMemory_StillWiresItsOwnRetriever guards the existing path against
// the new option's plumbing.
func TestWithMemory_StillWiresItsOwnRetriever(t *testing.T) {
	rec := newRecordingProvider()
	conv := openTestConvWithPack(t, memoryContextPack("grounding:{{memory_context}}"), "chat",
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		sdk.WithMemory(memory.NewInMemoryStore(), map[string]string{"user_id": "u1"},
			sdk.WithMemoryRetriever(groundingCorpus())),
	)

	_, err := conv.Send(context.Background(), "how long do refunds take?")
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "Refunds are issued within 14 days.")
}
