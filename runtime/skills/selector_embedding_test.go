package skills

import (
	"context"
	"errors"
	"math"
	"testing"
)

// mockEmbeddingProvider returns preset embeddings for testing.
type mockEmbeddingProvider struct {
	embeddings map[string][]float64
	callCount  int
	err        error
}

func (m *mockEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float64, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	result := make([][]float64, len(texts))
	for i, text := range texts {
		if emb, ok := m.embeddings[text]; ok {
			result[i] = emb
		} else {
			// Return zero vector for unknown texts
			result[i] = make([]float64, 3)
		}
	}
	return result, nil
}

func TestNewEmbeddingSelector(t *testing.T) {
	provider := &mockEmbeddingProvider{}

	s := NewEmbeddingSelector(provider, 5)
	if s.topK != 5 {
		t.Errorf("topK = %d, want 5", s.topK)
	}
	if s.provider != provider {
		t.Error("provider not set")
	}
}

func TestNewEmbeddingSelector_DefaultTopK(t *testing.T) {
	s := NewEmbeddingSelector(&mockEmbeddingProvider{}, 0)
	if s.topK != 10 {
		t.Errorf("topK = %d, want 10 (default)", s.topK)
	}

	s2 := NewEmbeddingSelector(&mockEmbeddingProvider{}, -1)
	if s2.topK != 10 {
		t.Errorf("topK = %d, want 10 (default for negative)", s2.topK)
	}
}

func TestEmbeddingSelector_Build(t *testing.T) {
	provider := &mockEmbeddingProvider{
		embeddings: map[string][]float64{
			"billing: Handle billing inquiries":   {1, 0, 0},
			"shipping: Handle shipping questions": {0, 1, 0},
		},
	}

	s := NewEmbeddingSelector(provider, 5)
	ctx := context.Background()

	skills := []SkillMetadata{
		{Name: "billing", Description: "Handle billing inquiries"},
		{Name: "shipping", Description: "Handle shipping questions"},
	}

	err := s.Build(ctx, skills)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(s.index) != 2 {
		t.Fatalf("index length = %d, want 2", len(s.index))
	}
	if s.index[0].name != "billing" {
		t.Errorf("index[0].name = %q, want %q", s.index[0].name, "billing")
	}
	if provider.callCount != 1 {
		t.Errorf("provider called %d times, want 1", provider.callCount)
	}
}

func TestEmbeddingSelector_BuildEmpty(t *testing.T) {
	s := NewEmbeddingSelector(&mockEmbeddingProvider{}, 5)
	err := s.Build(context.Background(), nil)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if s.index != nil {
		t.Errorf("index should be nil for empty skills")
	}
}

func TestEmbeddingSelector_BuildError(t *testing.T) {
	provider := &mockEmbeddingProvider{err: errors.New("embedding service unavailable")}

	s := NewEmbeddingSelector(provider, 5)
	err := s.Build(context.Background(), []SkillMetadata{
		{Name: "billing", Description: "Handle billing"},
	})
	if err == nil {
		t.Fatal("Build() should return error")
	}
}

func TestEmbeddingSelector_Select_TopK(t *testing.T) {
	// Create embeddings where billing is closest to the query
	provider := &mockEmbeddingProvider{
		embeddings: map[string][]float64{
			"billing: Handle billing inquiries":      {0.9, 0.1, 0.0},
			"shipping: Handle shipping questions":    {0.1, 0.9, 0.0},
			"returns: Handle return requests":        {0.8, 0.2, 0.0},
			"escalation: Escalate to supervisor":     {0.0, 0.1, 0.9},
			"I need help with my bill and a refund.": {0.95, 0.05, 0.0},
		},
	}

	s := NewEmbeddingSelector(provider, 2)
	ctx := context.Background()

	skills := []SkillMetadata{
		{Name: "billing", Description: "Handle billing inquiries"},
		{Name: "shipping", Description: "Handle shipping questions"},
		{Name: "returns", Description: "Handle return requests"},
		{Name: "escalation", Description: "Escalate to supervisor"},
	}

	err := s.Build(ctx, skills)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	names, err := s.Select(ctx, "I need help with my bill and a refund.", skills)
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2", len(names))
	}
	// billing should be first (most similar), returns second
	if names[0] != "billing" {
		t.Errorf("names[0] = %q, want %q", names[0], "billing")
	}
	if names[1] != "returns" {
		t.Errorf("names[1] = %q, want %q", names[1], "returns")
	}
}

func TestEmbeddingSelector_Select_TopKLargerThanAvailable(t *testing.T) {
	provider := &mockEmbeddingProvider{
		embeddings: map[string][]float64{
			"billing: Handle billing":  {1, 0, 0},
			"shipping: Handle shipping": {0, 1, 0},
			"query":                     {0.5, 0.5, 0},
		},
	}

	s := NewEmbeddingSelector(provider, 10) // topK=10 but only 2 skills
	ctx := context.Background()

	skills := []SkillMetadata{
		{Name: "billing", Description: "Handle billing"},
		{Name: "shipping", Description: "Handle shipping"},
	}
	_ = s.Build(ctx, skills)

	names, err := s.Select(ctx, "query", skills)
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2 (all available)", len(names))
	}
}

func TestEmbeddingSelector_Select_EmptyIndex(t *testing.T) {
	s := NewEmbeddingSelector(&mockEmbeddingProvider{}, 5)
	// No Build() called

	available := []SkillMetadata{
		{Name: "billing", Description: "Handle billing"},
	}

	names, err := s.Select(context.Background(), "help", available)
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if len(names) != 1 || names[0] != "billing" {
		t.Errorf("expected fallback to all names, got %v", names)
	}
}

func TestEmbeddingSelector_Select_EmptyAvailable(t *testing.T) {
	s := NewEmbeddingSelector(&mockEmbeddingProvider{}, 5)

	names, err := s.Select(context.Background(), "help", nil)
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected empty names for nil available, got %v", names)
	}
}

func TestEmbeddingSelector_Select_EmbedErrorFallback(t *testing.T) {
	provider := &mockEmbeddingProvider{
		embeddings: map[string][]float64{
			"billing: Handle billing": {1, 0, 0},
		},
	}

	s := NewEmbeddingSelector(provider, 5)
	ctx := context.Background()

	skills := []SkillMetadata{
		{Name: "billing", Description: "Handle billing"},
	}
	_ = s.Build(ctx, skills)

	// Now make provider fail for the query embedding
	provider.err = errors.New("rate limited")

	names, err := s.Select(ctx, "help with billing", skills)
	if err != nil {
		t.Fatalf("Select() should not return error on embed failure, got: %v", err)
	}
	// Should fall back to all available
	if len(names) != 1 || names[0] != "billing" {
		t.Errorf("expected fallback to all names, got %v", names)
	}
}

func TestEmbeddingSelector_Select_FiltersByAvailable(t *testing.T) {
	provider := &mockEmbeddingProvider{
		embeddings: map[string][]float64{
			"billing: Handle billing":   {0.9, 0.1, 0},
			"shipping: Handle shipping": {0.1, 0.9, 0},
			"returns: Handle returns":   {0.8, 0.2, 0},
			"query":                     {0.95, 0.05, 0},
		},
	}

	s := NewEmbeddingSelector(provider, 5)
	ctx := context.Background()

	allSkills := []SkillMetadata{
		{Name: "billing", Description: "Handle billing"},
		{Name: "shipping", Description: "Handle shipping"},
		{Name: "returns", Description: "Handle returns"},
	}
	_ = s.Build(ctx, allSkills)

	// Only billing and shipping are available (e.g., workflow state filter)
	available := []SkillMetadata{
		{Name: "billing", Description: "Handle billing"},
		{Name: "shipping", Description: "Handle shipping"},
	}

	names, err := s.Select(ctx, "query", available)
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	// returns should not appear even though it's in the index
	for _, n := range names {
		if n == "returns" {
			t.Error("returns should not be in results — not in available set")
		}
	}
}

func TestEmbeddingSelector_BuildRebuild(t *testing.T) {
	provider := &mockEmbeddingProvider{
		embeddings: map[string][]float64{
			"billing: Handle billing":   {1, 0, 0},
			"shipping: Handle shipping": {0, 1, 0},
		},
	}

	s := NewEmbeddingSelector(provider, 5)
	ctx := context.Background()

	// Initial build
	_ = s.Build(ctx, []SkillMetadata{
		{Name: "billing", Description: "Handle billing"},
	})
	if len(s.index) != 1 {
		t.Fatalf("index length = %d after first build, want 1", len(s.index))
	}

	// Rebuild with different skills
	_ = s.Build(ctx, []SkillMetadata{
		{Name: "billing", Description: "Handle billing"},
		{Name: "shipping", Description: "Handle shipping"},
	})
	if len(s.index) != 2 {
		t.Fatalf("index length = %d after rebuild, want 2", len(s.index))
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float64
		want float64
	}{
		{
			name: "identical vectors",
			a:    []float64{1, 0, 0},
			b:    []float64{1, 0, 0},
			want: 1.0,
		},
		{
			name: "orthogonal vectors",
			a:    []float64{1, 0, 0},
			b:    []float64{0, 1, 0},
			want: 0.0,
		},
		{
			name: "opposite vectors",
			a:    []float64{1, 0, 0},
			b:    []float64{-1, 0, 0},
			want: -1.0,
		},
		{
			name: "similar vectors",
			a:    []float64{0.9, 0.1, 0},
			b:    []float64{0.8, 0.2, 0},
			want: 0.9910,
		},
		{
			name: "empty vectors",
			a:    []float64{},
			b:    []float64{},
			want: 0.0,
		},
		{
			name: "different lengths",
			a:    []float64{1, 0},
			b:    []float64{1, 0, 0},
			want: 0.0,
		},
		{
			name: "zero magnitude vector",
			a:    []float64{0, 0, 0},
			b:    []float64{1, 0, 0},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("cosineSimilarity() = %f, want %f", got, tt.want)
			}
		})
	}
}
