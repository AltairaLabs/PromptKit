// Package cosine is a reference Selector implementation that ranks
// candidates by cosine similarity over embeddings produced by a
// PromptKit EmbeddingProvider.
//
// This is the Go port of the in-tree EmbeddingSelector that shipped
// before #980 deleted it. It exists as an example, not as core code:
// PromptKit no longer ships any selector implementations beyond the
// exec client. Copy, adapt, or import directly.
//
// Usage:
//
//	emb, _ := openai.NewEmbeddingProvider()
//	sel := cosine.New("skills_local", emb, cosine.Options{TopK: 5})
//	conv, _ := sdk.Open("./pack.json", "chat",
//	    sdk.WithSelector("skills_local", sel),
//	    sdk.WithRuntimeConfig("./runtime.yaml"), // spec.skills.selector: skills_local
//	)
//
// The provider on SelectorContext (set by RAG / WithContextRetrieval)
// takes precedence over the one passed to New, which lets a single
// embedding instance back both RAG and selection. Pass nil to opt
// into context-driven embeddings exclusively.
package cosine

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/selection"
)

const defaultTopK = 10

// Options configures the cosine selector.
type Options struct {
	// TopK caps the number of candidates returned. Zero falls back to
	// defaultTopK (10). Use a large value to effectively disable
	// truncation.
	TopK int
}

// Selector ranks candidates by cosine similarity. Embeddings for
// identical (Candidate.ID, Description) pairs are cached across
// Select calls; changing a description re-embeds the candidate.
type Selector struct {
	name        string
	embFromCtor providers.EmbeddingProvider
	embCtx      providers.EmbeddingProvider
	topK        int

	mu    sync.Mutex
	cache map[cacheKey][]float32
}

type cacheKey struct {
	id   string
	desc string
}

// New returns a Selector that uses the given embedding provider when
// SelectorContext doesn't supply one. Pass nil to require the context
// to provide a provider; Select returns an error otherwise.
func New(name string, embeddings providers.EmbeddingProvider, opts Options) *Selector {
	topK := opts.TopK
	if topK <= 0 {
		topK = defaultTopK
	}
	return &Selector{
		name:        name,
		embFromCtor: embeddings,
		topK:        topK,
		cache:       make(map[cacheKey][]float32),
	}
}

// Name returns the selector's registered name.
func (s *Selector) Name() string { return s.name }

// Init records the embedding provider supplied by the host (typically
// the same instance configured for RAG). When non-nil it overrides the
// constructor-supplied provider so a single embedding pool serves both
// selection and retrieval.
func (s *Selector) Init(ctx selection.SelectorContext) error {
	if ctx.Embeddings != nil {
		s.embCtx = ctx.Embeddings
	}
	if s.provider() == nil {
		return fmt.Errorf("cosine selector %q: no embedding provider configured", s.name)
	}
	return nil
}

// Select embeds the query and the candidates, ranks by cosine
// similarity, and returns the top-K IDs. On any embedding failure
// it returns the error; the caller (skills executor) treats that as
// "include all eligible".
func (s *Selector) Select(ctx context.Context, q selection.Query, candidates []selection.Candidate) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	prov := s.provider()
	if prov == nil {
		return nil, fmt.Errorf("cosine selector %q: no embedding provider", s.name)
	}

	candVecs, err := s.embedCandidates(ctx, prov, candidates)
	if err != nil {
		return nil, fmt.Errorf("cosine selector %q: candidate embed: %w", s.name, err)
	}

	resp, err := prov.Embed(ctx, providers.EmbeddingRequest{Texts: []string{q.Text}})
	if err != nil {
		return nil, fmt.Errorf("cosine selector %q: query embed: %w", s.name, err)
	}
	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("cosine selector %q: empty query embedding", s.name)
	}
	queryVec := resp.Embeddings[0]

	type scored struct {
		id    string
		score float64
	}
	scores := make([]scored, len(candidates))
	for i, c := range candidates {
		scores[i] = scored{id: c.ID, score: cosineSimilarity(queryVec, candVecs[i])}
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })

	k := q.K
	if k <= 0 || k > s.topK {
		k = s.topK
	}
	if k > len(scores) {
		k = len(scores)
	}
	out := make([]string, k)
	for i := 0; i < k; i++ {
		out[i] = scores[i].id
	}
	return out, nil
}

func (s *Selector) provider() providers.EmbeddingProvider {
	if s.embCtx != nil {
		return s.embCtx
	}
	return s.embFromCtor
}

// embedCandidates returns one embedding per candidate, batching the
// uncached descriptions into a single Embed call. Cache lives for the
// selector's lifetime.
func (s *Selector) embedCandidates(ctx context.Context, prov providers.EmbeddingProvider,
	candidates []selection.Candidate,
) ([][]float32, error) {
	s.mu.Lock()
	out := make([][]float32, len(candidates))
	var pending []string
	var pendingIdx []int
	for i, c := range candidates {
		key := cacheKey{id: c.ID, desc: c.Description}
		if v, ok := s.cache[key]; ok {
			out[i] = v
			continue
		}
		pending = append(pending, c.Name+": "+c.Description)
		pendingIdx = append(pendingIdx, i)
	}
	s.mu.Unlock()

	if len(pending) == 0 {
		return out, nil
	}
	resp, err := prov.Embed(ctx, providers.EmbeddingRequest{Texts: pending})
	if err != nil {
		return nil, err
	}
	if len(resp.Embeddings) != len(pending) {
		return nil, fmt.Errorf("provider returned %d embeddings, expected %d", len(resp.Embeddings), len(pending))
	}

	s.mu.Lock()
	for j, idx := range pendingIdx {
		c := candidates[idx]
		s.cache[cacheKey{id: c.ID, desc: c.Description}] = resp.Embeddings[j]
		out[idx] = resp.Embeddings[j]
	}
	s.mu.Unlock()
	return out, nil
}

// cosineSimilarity returns the cosine similarity of two equal-length
// vectors, or 0 when either has zero magnitude or differing length.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		af, bf := float64(a[i]), float64(b[i])
		dot += af * bf
		magA += af * af
		magB += bf * bf
	}
	mag := math.Sqrt(magA) * math.Sqrt(magB)
	if mag == 0 {
		return 0
	}
	return dot / mag
}
