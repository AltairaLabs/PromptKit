package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/memory/corpus"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// stubRetriever returns a fixed set of memories regardless of the query. It
// stands in for a host retriever (vector store, document index) so the test
// exercises the injection path rather than any relevance logic.
type stubRetriever struct {
	memories []*memory.Memory
	sawScope map[string]string
	sawMsgs  []types.Message
}

func (s *stubRetriever) RetrieveContext(
	_ context.Context, scope map[string]string, messages []types.Message,
) ([]*memory.Memory, error) {
	s.sawScope = scope
	s.sawMsgs = messages
	return s.memories, nil
}

func memoryContextPack(systemTemplate string) string {
	return `{
		"id": "memory-context-test",
		"version": "1.0.0",
		"description": "Pack whose system prompt consumes memory_context",
		"prompts": {
			"chat": {
				"id": "chat",
				"name": "Chat",
				"system_template": ` + quote(systemTemplate) + `
			}
		}
	}`
}

// openMemoryConv opens a conversation wired for ambient memory injection with
// the supplied retriever, returning the recording provider so the caller can
// assert on the system prompt the model actually received.
func openMemoryConv(
	t *testing.T, systemTemplate string, retriever memory.Retriever, memOpts ...sdk.MemoryOption,
) (*sdk.Conversation, *recordingProvider) {
	t.Helper()
	rec := newRecordingProvider()
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}

	opts := append([]sdk.MemoryOption{sdk.WithMemoryRetriever(retriever)}, memOpts...)
	conv := openTestConvWithPack(t, memoryContextPack(systemTemplate), "chat",
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		sdk.WithMemory(store, scope, opts...),
	)
	return conv, rec
}

// TestMemoryContext_ReachesSystemPrompt is the regression test for #1958: the
// retrieval stage wrote memory_context after TemplateStage had already
// rendered, so the placeholder reached the model unresolved.
func TestMemoryContext_ReachesSystemPrompt(t *testing.T) {
	ret := &stubRetriever{memories: []*memory.Memory{
		{Type: "preference", Content: "prefers dark mode", Confidence: 0.9},
	}}

	conv, rec := openMemoryConv(t, "known:{{memory_context}}", ret)
	_, err := conv.Send(context.Background(), "what theme do I like?")
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "prefers dark mode")
	assert.NotContains(t, rec.system(), "{{memory_context}}")
}

// TestMemoryContext_HostFormatterReachesSystemPrompt covers
// WithMemoryContextFormatter end to end — until now it was only asserted to be
// stored on the capability, never to affect a prompt.
func TestMemoryContext_HostFormatterReachesSystemPrompt(t *testing.T) {
	ret := &stubRetriever{memories: []*memory.Memory{
		{Type: "preference", Content: "prefers dark mode", Confidence: 0.9},
	}}
	formatter := func(memories []*memory.Memory) string {
		return "HOST:" + memories[0].Content
	}

	conv, rec := openMemoryConv(t, "known:{{memory_context}}", ret,
		sdk.WithMemoryContextFormatter(formatter))
	_, err := conv.Send(context.Background(), "what theme do I like?")
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "HOST:prefers dark mode")
	assert.NotContains(t, rec.system(), "confidence")
}

// TestMemoryContext_CorpusRetrieverGroundsFromSeparateSource runs the whole
// ambient path against the shipped corpus retriever: host documents reach the
// system prompt without the host writing retrieval code, and without the
// memory store — which the tools own — being involved in grounding.
func TestMemoryContext_CorpusRetrieverGroundsFromSeparateSource(t *testing.T) {
	rec := newRecordingProvider()
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}

	// A fact the tools remembered about the user. Ambient injection must not
	// surface it — that is memory__recall's job, not the corpus's.
	require.NoError(t, store.Save(context.Background(), &memory.Memory{
		Type: "preference", Content: "prefers email over phone", Confidence: 0.9, Scope: scope,
	}))

	kb := corpus.New([]corpus.Document{
		{ID: "refunds", Title: "Refund policy", Text: "Refunds are issued within 14 days."},
		{ID: "shipping", Title: "Shipping", Text: "Orders ship next business day."},
	})

	conv := openTestConvWithPack(t, memoryContextPack("grounding:{{memory_context}}"), "chat",
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		sdk.WithMemory(store, scope, sdk.WithMemoryRetriever(kb)),
	)

	_, err := conv.Send(context.Background(), "how long do refunds take?")
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "Refunds are issued within 14 days.")
	assert.NotContains(t, rec.system(), "{{memory_context}}")
	assert.NotContains(t, rec.system(), "prefers email over phone")
}

// TestMemoryContext_RetrieverSeesScopeAndTurnMessages pins the two inputs a
// host retriever gets and a variables.Provider cannot: the memory scope and
// the turn's messages.
func TestMemoryContext_RetrieverSeesScopeAndTurnMessages(t *testing.T) {
	ret := &stubRetriever{memories: []*memory.Memory{
		{Type: "fact", Content: "lives in Bristol", Confidence: 1.0},
	}}

	conv, _ := openMemoryConv(t, "known:{{memory_context}}", ret)
	_, err := conv.Send(context.Background(), "where do I live?")
	require.NoError(t, err)

	assert.Equal(t, "u1", ret.sawScope["user_id"])
	require.NotEmpty(t, ret.sawMsgs)
	assert.Contains(t, ret.sawMsgs[len(ret.sawMsgs)-1].GetContent(), "where do I live?")
}

// TestMemoryContext_RefreshesEachTurn pins that grounding follows the
// conversation. The system prompt used to render once per conversation, so
// turn two was answered with turn one's retrieval.
func TestMemoryContext_RefreshesEachTurn(t *testing.T) {
	rec := newRecordingProvider()
	kb := corpus.New([]corpus.Document{
		{ID: "refunds", Title: "Refunds", Text: "Refunds take 14 days."},
		{ID: "shipping", Title: "Shipping", Text: "Shipping is next day."},
	})

	conv := openTestConvWithPack(t, memoryContextPack("g:{{memory_context}}"), "chat",
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		sdk.WithRetriever(kb),
	)

	_, err := conv.Send(context.Background(), "tell me about refunds")
	require.NoError(t, err)
	assert.Contains(t, rec.system(), "Refunds take 14 days.")

	_, err = conv.Send(context.Background(), "tell me about shipping")
	require.NoError(t, err)
	assert.Contains(t, rec.system(), "Shipping is next day.")
	assert.NotContains(t, rec.system(), "Refunds take 14 days.")
}

// TestMemoryContext_StaleContextDoesNotLeak pins the other half: a turn that
// retrieves nothing must not inherit the previous turn's grounding, which the
// variable map would otherwise still be holding.
func TestMemoryContext_StaleContextDoesNotLeak(t *testing.T) {
	rec := newRecordingProvider()
	kb := corpus.New([]corpus.Document{
		{ID: "refunds", Title: "Refunds", Text: "Refunds take 14 days."},
	})

	conv := openTestConvWithPack(t, memoryContextPack("g:{{memory_context}}"), "chat",
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		sdk.WithRetriever(kb),
	)

	_, err := conv.Send(context.Background(), "tell me about refunds")
	require.NoError(t, err)
	require.Contains(t, rec.system(), "Refunds take 14 days.")

	_, err = conv.Send(context.Background(), "what is the weather like")
	require.NoError(t, err)
	assert.NotContains(t, rec.system(), "Refunds take 14 days.")
	assert.NotContains(t, rec.system(), "{{memory_context}}")
}
