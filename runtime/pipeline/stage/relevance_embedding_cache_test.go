package stage

import (
	"context"
	"errors"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// countingEmbedder records every text it is asked to embed, per call, and
// derives each vector from the text alone, so a vector is correct for its text
// wherever that text sits in a request. Safe for concurrent use.
type countingEmbedder struct {
	id    string
	model string
	fail  bool

	mu    sync.Mutex
	calls [][]string
}

func (e *countingEmbedder) Embed(_ context.Context, req providers.EmbeddingRequest) (providers.EmbeddingResponse, error) {
	e.mu.Lock()
	e.calls = append(e.calls, append([]string(nil), req.Texts...))
	e.mu.Unlock()
	if e.fail {
		return providers.EmbeddingResponse{}, errors.New("embedder down")
	}
	out := make([][]float32, len(req.Texts))
	for i, text := range req.Texts {
		out[i] = vectorFor(text)
	}
	return providers.EmbeddingResponse{Embeddings: out, Model: e.model}, nil
}

func (e *countingEmbedder) EmbeddingDimensions() int { return 3 }
func (e *countingEmbedder) MaxBatchSize() int        { return 100 }
func (e *countingEmbedder) ID() string               { return e.id }
func (e *countingEmbedder) Model() string            { return e.model }

func (e *countingEmbedder) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

// textsSince returns, sorted, every text embedded in calls after the first n.
func (e *countingEmbedder) textsSince(n int) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, call := range e.calls[n:] {
		out = append(out, call...)
	}
	sort.Strings(out)
	return out
}

// vectorFor maps a text to a stable 3-d vector.
func vectorFor(text string) []float32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(text))
	v := h.Sum32()
	return []float32{float32(v&0xff) + 1, float32((v>>8)&0xff) + 1, float32((v>>16)&0xff) + 1}
}

func cachingRelevanceStage(p providers.EmbeddingProvider, cache bool) *ContextBuilderStage {
	return NewContextBuilderStage(&ContextBuilderPolicy{
		TokenBudget: 40,
		Strategy:    TruncateLeastRelevant,
		RelevanceConfig: &RelevanceConfig{
			EmbeddingProvider: p,
			MinRecentMessages: 1,
			CacheEmbeddings:   cache,
		},
	})
}

// longMsg builds a message long enough that a few of them exceed the budget.
func longMsg(role, word string) types.Message {
	return types.Message{Role: role, Content: strings.Repeat(word+" ", 12)}
}

func runRelevance(t *testing.T, s *ContextBuilderStage, msgs []types.Message) []string {
	t.Helper()
	kept, truncated, err := processRelevance(s, msgs)
	require.NoError(t, err)
	require.True(t, truncated, "test input must exceed the budget so relevance truncation runs")
	return kept
}

// processRelevance runs the stage once and returns the kept contents and
// whether truncation ran. It never calls t, so goroutines can use it.
func processRelevance(s *ContextBuilderStage, msgs []types.Message) (kept []string, truncated bool, err error) {
	input := make(chan StreamElement, len(msgs))
	output := make(chan StreamElement, len(msgs))
	for i := range msgs {
		input <- NewMessageElement(&msgs[i])
	}
	close(input)
	if err := s.Process(context.Background(), input, output); err != nil {
		return nil, false, err
	}
	for elem := range output {
		if elem.Message != nil {
			kept = append(kept, elem.Message.Content)
			truncated = truncated || elem.Meta.ContextTruncated
		}
	}
	return kept, truncated, nil
}

func conversation() []types.Message {
	return []types.Message{
		longMsg("user", "alpha"),
		longMsg("assistant", "bravo"),
		longMsg("user", "charlie"),
		longMsg("assistant", "delta"),
	}
}

func TestRelevanceCache_SecondTruncationEmbedsOnlyNewMessages(t *testing.T) {
	emb := &countingEmbedder{id: "emb", model: "m1"}
	s := cachingRelevanceStage(emb, true)

	history := conversation()
	runRelevance(t, s, history)
	require.Equal(t, 1, emb.callCount())

	next := append(append([]types.Message(nil), history...),
		longMsg("user", "echo"), longMsg("assistant", "foxtrot"))
	runRelevance(t, s, next)

	// The query is the last user message ("echo"), which is also a message: it
	// is embedded once.
	want := []string{next[4].Content, next[5].Content}
	sort.Strings(want)
	assert.Equal(t, want, emb.textsSince(1))
	assert.Equal(t, 2, emb.callCount())
}

// A second truncation of the same conversation sends the embedding provider
// nothing, and keeps exactly what the first one kept.
func TestRelevanceCache_UnchangedConversationMakesNoEmbedCall(t *testing.T) {
	emb := &countingEmbedder{id: "emb", model: "m1"}
	s := cachingRelevanceStage(emb, true)

	first := runRelevance(t, s, conversation())
	second := runRelevance(t, s, conversation())
	assert.Equal(t, []string(nil), emb.textsSince(1), "no text is embedded again")
	assert.Equal(t, first, second)
}

func TestRelevanceCache_ChangedContentIsReembedded(t *testing.T) {
	emb := &countingEmbedder{id: "emb", model: "m1"}
	s := cachingRelevanceStage(emb, true)

	runRelevance(t, s, conversation())

	edited := conversation()
	edited[1] = longMsg("assistant", "bravissimo")
	runRelevance(t, s, edited)

	assert.Equal(t, []string{edited[1].Content}, emb.textsSince(1))
}

func TestRelevanceCache_ReorderedMessagesKeepTheirOwnVectors(t *testing.T) {
	emb := &countingEmbedder{id: "emb", model: "m1"}
	cached := cachingRelevanceStage(emb, true)
	runRelevance(t, cached, conversation())

	reordered := conversation()
	reordered[0], reordered[2] = reordered[2], reordered[0]
	gotCached := runRelevance(t, cached, reordered)
	assert.Equal(t, 1, emb.callCount(), "reordering must not re-embed")

	fresh := cachingRelevanceStage(&countingEmbedder{id: "emb", model: "m1"}, false)
	assert.Equal(t, runRelevance(t, fresh, reordered), gotCached,
		"cached vectors must select exactly what fresh embeddings select")
}

func TestRelevanceCache_KeyIncludesProviderIdentity(t *testing.T) {
	first := &countingEmbedder{id: "emb", model: "m1"}
	s := cachingRelevanceStage(first, true)
	runRelevance(t, s, conversation())
	// The four messages; the query is the last user message, which is one
	// of them, so it is not embedded separately.
	allTexts := first.textsSince(0)
	require.Len(t, allTexts, 4)

	for _, other := range []*countingEmbedder{
		{id: "emb", model: "m2"},   // same provider, different model
		{id: "other", model: "m1"}, // different provider
	} {
		s.policy.RelevanceConfig.EmbeddingProvider = other
		runRelevance(t, s, conversation())
		assert.Equal(t, allTexts, other.textsSince(0),
			"%s/%s must embed every message itself, not reuse another identity's vectors", other.id, other.model)
	}
}

func TestRelevanceCache_OffByDefault(t *testing.T) {
	emb := &countingEmbedder{id: "emb", model: "m1"}
	s := cachingRelevanceStage(emb, false)

	runRelevance(t, s, conversation())
	runRelevance(t, s, conversation())
	assert.Equal(t, 2, emb.callCount())
	// Four messages plus the query (the last user message) again.
	assert.Len(t, emb.textsSince(1), 5, "without caching every message is embedded again")
}

func TestRelevanceCache_KeepsOnlyTheLastCallsVectors(t *testing.T) {
	emb := &countingEmbedder{id: "emb", model: "m1"}
	s := cachingRelevanceStage(emb, true)

	runRelevance(t, s, conversation())
	assert.Len(t, s.embedCache.vectors, 4)

	// A hot window that has moved on: the first two messages are gone.
	window := append(conversation()[2:], longMsg("user", "golf"), longMsg("assistant", "hotel"))
	runRelevance(t, s, window)
	assert.Len(t, s.embedCache.vectors, 4, "vectors for messages no longer in the input are released")
}

func TestRelevanceCache_EmbedErrorFallsBackAndKeepsCache(t *testing.T) {
	emb := &countingEmbedder{id: "emb", model: "m1"}
	s := cachingRelevanceStage(emb, true)
	runRelevance(t, s, conversation())

	emb.fail = true
	more := append(conversation(), longMsg("user", "india"))
	runRelevance(t, s, more) // falls back to oldest, still truncates
	assert.Len(t, s.embedCache.vectors, 4)

	emb.fail = false
	runRelevance(t, s, more)
	assert.Equal(t, []string{more[4].Content}, emb.textsSince(2))
}

func TestRelevanceCache_ConcurrentProcess(t *testing.T) {
	emb := &countingEmbedder{id: "emb", model: "m1"}
	s := cachingRelevanceStage(emb, true)
	longer := append(conversation(), longMsg("user", "juliett"))
	uncached := func(msgs []types.Message) []string {
		return runRelevance(t, cachingRelevanceStage(&countingEmbedder{id: "emb", model: "m1"}, false), msgs)
	}
	want := [][]string{uncached(conversation()), uncached(longer)}

	const workers = 16
	results := make([][]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msgs := conversation()
			if i%2 == 1 {
				msgs = append(msgs, longMsg("user", "juliett"))
			}
			results[i], _, errs[i] = processRelevance(s, msgs)
		}(i)
	}
	wg.Wait()
	for i := range workers {
		require.NoError(t, errs[i])
	}
	for i := range workers {
		assert.Equal(t, want[i%2], results[i], "worker %d selected differently from fresh embeddings", i)
	}
}
