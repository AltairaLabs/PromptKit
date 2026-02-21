package skills

import (
	"context"
	"math"
	"sort"
)

// EmbeddingProvider generates vector embeddings for text inputs.
// Implementations may wrap OpenAI, Anthropic, or local embedding models.
type EmbeddingProvider interface {
	// Embed returns embedding vectors for the given texts.
	// Each text produces one vector. All vectors must have the same dimensionality.
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// embeddedSkill pairs a skill name with its precomputed embedding vector.
type embeddedSkill struct {
	name      string
	embedding []float64
}

// EmbeddingSelector uses semantic similarity to select the most relevant skills
// for a given query. It precomputes embeddings for all skill descriptions at
// initialization and ranks skills by cosine similarity at selection time.
//
// Effective for large skill sets (50+) where showing all skills in the Phase 1
// index would waste context. Returns the top-k most relevant skills.
//
// Safe for concurrent use after Build() completes.
type EmbeddingSelector struct {
	provider EmbeddingProvider
	topK     int
	index    []embeddedSkill
}

// NewEmbeddingSelector creates a new EmbeddingSelector with the given provider
// and top-k count. If topK <= 0, it defaults to 10.
func NewEmbeddingSelector(provider EmbeddingProvider, topK int) *EmbeddingSelector {
	if topK <= 0 {
		topK = 10
	}
	return &EmbeddingSelector{
		provider: provider,
		topK:     topK,
	}
}

// Build precomputes embeddings for the given skills' descriptions.
// Must be called before Select. Safe to call multiple times to rebuild the index.
func (s *EmbeddingSelector) Build(ctx context.Context, skills []SkillMetadata) error {
	if len(skills) == 0 {
		s.index = nil
		return nil
	}

	texts := make([]string, len(skills))
	for i, sk := range skills {
		texts[i] = sk.Name + ": " + sk.Description
	}

	embeddings, err := s.provider.Embed(ctx, texts)
	if err != nil {
		return err
	}

	index := make([]embeddedSkill, len(skills))
	for i, sk := range skills {
		index[i] = embeddedSkill{
			name:      sk.Name,
			embedding: embeddings[i],
		}
	}
	s.index = index
	return nil
}

// Select returns the top-k skill names most semantically similar to the query.
// If the embedding provider fails, it falls back to returning all skill names
// (equivalent to ModelDrivenSelector behavior).
// If available skills differ from the built index, only indexed skills that
// appear in available are considered.
func (s *EmbeddingSelector) Select(
	ctx context.Context,
	query string,
	available []SkillMetadata,
) ([]string, error) {
	// No index or no available skills — return all available
	if len(s.index) == 0 || len(available) == 0 {
		return allNames(available), nil
	}

	// Build set of currently available skill names
	availableSet := make(map[string]bool, len(available))
	for _, sk := range available {
		availableSet[sk.Name] = true
	}

	// Embed the query
	embeddings, err := s.provider.Embed(ctx, []string{query})
	if err != nil {
		// Graceful fallback: return all available skills
		return allNames(available), nil
	}
	queryVec := embeddings[0]

	// Score each indexed skill that is in the available set
	type scored struct {
		name  string
		score float64
	}
	var candidates []scored
	for _, es := range s.index {
		if !availableSet[es.name] {
			continue
		}
		sim := cosineSimilarity(queryVec, es.embedding)
		candidates = append(candidates, scored{name: es.name, score: sim})
	}

	// Sort by descending similarity
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Return top-k
	k := min(s.topK, len(candidates))
	names := make([]string, k)
	for i := 0; i < k; i++ {
		names[i] = candidates[i].name
	}
	return names, nil
}

// allNames extracts skill names from metadata.
func allNames(skills []SkillMetadata) []string {
	names := make([]string, len(skills))
	for i, sk := range skills {
		names[i] = sk.Name
	}
	return names
}

// cosineSimilarity computes the cosine similarity between two vectors.
// Returns 0 if either vector has zero magnitude.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, magA, magB float64
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}

	mag := math.Sqrt(magA) * math.Sqrt(magB)
	if mag == 0 {
		return 0
	}
	return dot / mag
}
