package cosine

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/selection"
)

// fakeEmbedder returns a deterministic embedding per text via a lookup
// map. Texts not in the map get a zero vector. embedCalls counts how
// many Embed invocations the selector made — useful for asserting cache
// behavior.
type fakeEmbedder struct {
	vectors    map[string][]float32
	err        error
	embedCalls int
}

func (f *fakeEmbedder) Embed(_ context.Context, req providers.EmbeddingRequest) (providers.EmbeddingResponse, error) {
	f.embedCalls++
	if f.err != nil {
		return providers.EmbeddingResponse{}, f.err
	}
	out := make([][]float32, len(req.Texts))
	for i, t := range req.Texts {
		if v, ok := f.vectors[t]; ok {
			out[i] = v
			continue
		}
		out[i] = []float32{0, 0, 0}
	}
	return providers.EmbeddingResponse{Embeddings: out}, nil
}

func (f *fakeEmbedder) EmbeddingDimensions() int { return 3 }
func (f *fakeEmbedder) MaxBatchSize() int        { return 100 }
func (f *fakeEmbedder) ID() string               { return "fake" }

func candidatesABC() []selection.Candidate {
	return []selection.Candidate{
		{ID: "alpha", Name: "alpha", Description: "weather and forecasting"},
		{ID: "beta", Name: "beta", Description: "billing and invoices"},
		{ID: "gamma", Name: "gamma", Description: "user profile management"},
	}
}

func newTestSelector(t *testing.T, fe *fakeEmbedder, topK int) *Selector {
	t.Helper()
	s := New("test", fe, Options{TopK: topK})
	if err := s.Init(selection.SelectorContext{}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

func TestSelect_RanksMostSimilarFirst(t *testing.T) {
	fe := &fakeEmbedder{vectors: map[string][]float32{
		"alpha: weather and forecasting": {1, 0, 0},
		"beta: billing and invoices":     {0, 1, 0},
		"gamma: user profile management": {0, 0, 1},
		"will it rain tomorrow":          {1, 0, 0}, // matches alpha
	}}
	s := newTestSelector(t, fe, 0)

	ids, err := s.Select(context.Background(),
		selection.Query{Text: "will it rain tomorrow", Kind: "skill"},
		candidatesABC(),
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(ids) == 0 || ids[0] != "alpha" {
		t.Errorf("top result = %v, want alpha first", ids)
	}
}

func TestSelect_HonorsQueryK(t *testing.T) {
	fe := &fakeEmbedder{vectors: map[string][]float32{
		"alpha: weather and forecasting": {1, 0, 0},
		"beta: billing and invoices":     {1, 0, 0},
		"gamma: user profile management": {1, 0, 0},
		"q":                              {1, 0, 0},
	}}
	s := newTestSelector(t, fe, 10)
	ids, _ := s.Select(context.Background(),
		selection.Query{Text: "q", K: 2},
		candidatesABC(),
	)
	if len(ids) != 2 {
		t.Errorf("got %d ids, want 2", len(ids))
	}
}

func TestSelect_OptionsTopKCap(t *testing.T) {
	fe := &fakeEmbedder{vectors: map[string][]float32{
		"alpha: weather and forecasting": {1, 0, 0},
		"beta: billing and invoices":     {1, 0, 0},
		"gamma: user profile management": {1, 0, 0},
		"q":                              {1, 0, 0},
	}}
	s := newTestSelector(t, fe, 1)
	ids, _ := s.Select(context.Background(), selection.Query{Text: "q"}, candidatesABC())
	if len(ids) != 1 {
		t.Errorf("got %d ids, want 1 (TopK cap)", len(ids))
	}
}

func TestSelect_CachesCandidateEmbeddings(t *testing.T) {
	fe := &fakeEmbedder{vectors: map[string][]float32{
		"alpha: weather and forecasting": {1, 0, 0},
		"beta: billing and invoices":     {0, 1, 0},
		"gamma: user profile management": {0, 0, 1},
		"q1":                             {1, 0, 0},
		"q2":                             {0, 1, 0},
	}}
	s := newTestSelector(t, fe, 5)
	cands := candidatesABC()

	_, _ = s.Select(context.Background(), selection.Query{Text: "q1"}, cands)
	firstCalls := fe.embedCalls
	_, _ = s.Select(context.Background(), selection.Query{Text: "q2"}, cands)
	// Second call should embed only the new query, not the candidates again.
	if fe.embedCalls-firstCalls != 1 {
		t.Errorf("expected 1 additional embed call (query only), got %d", fe.embedCalls-firstCalls)
	}
}

func TestSelect_EmptyCandidates(t *testing.T) {
	fe := &fakeEmbedder{}
	s := newTestSelector(t, fe, 0)
	ids, err := s.Select(context.Background(), selection.Query{Text: "q"}, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if ids != nil {
		t.Errorf("ids = %v, want nil", ids)
	}
	if fe.embedCalls != 0 {
		t.Errorf("should not call embed for empty candidates, got %d calls", fe.embedCalls)
	}
}

func TestSelect_EmbeddingError(t *testing.T) {
	fe := &fakeEmbedder{err: errors.New("rate limited")}
	s := newTestSelector(t, fe, 0)
	_, err := s.Select(context.Background(), selection.Query{Text: "q"}, candidatesABC())
	if err == nil {
		t.Fatal("expected error from embedding failure")
	}
}

func TestInit_RequiresEmbeddingProvider(t *testing.T) {
	s := New("test", nil, Options{})
	err := s.Init(selection.SelectorContext{})
	if err == nil {
		t.Fatal("expected error when no provider supplied")
	}
}

func TestInit_ContextProviderOverridesCtor(t *testing.T) {
	ctor := &fakeEmbedder{vectors: map[string][]float32{"q": {1, 0, 0}}}
	ctxProv := &fakeEmbedder{vectors: map[string][]float32{
		"alpha: weather and forecasting": {1, 0, 0},
		"beta: billing and invoices":     {0, 1, 0},
		"gamma: user profile management": {0, 0, 1},
		"q":                              {1, 0, 0},
	}}
	s := New("test", ctor, Options{TopK: 1})
	if err := s.Init(selection.SelectorContext{Embeddings: ctxProv}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, _ = s.Select(context.Background(), selection.Query{Text: "q"}, candidatesABC())
	if ctor.embedCalls != 0 {
		t.Errorf("constructor provider should not be used; got %d calls", ctor.embedCalls)
	}
	if ctxProv.embedCalls == 0 {
		t.Errorf("context provider should have been used")
	}
}

func TestName(t *testing.T) {
	if got := New("my_sel", &fakeEmbedder{}, Options{}).Name(); got != "my_sel" {
		t.Errorf("Name() = %q", got)
	}
}

func TestCosineSimilarity_EdgeCases(t *testing.T) {
	if cosineSimilarity(nil, nil) != 0 {
		t.Error("nil/nil should be 0")
	}
	if cosineSimilarity([]float32{1, 2}, []float32{1}) != 0 {
		t.Error("differing lengths should be 0")
	}
	if cosineSimilarity([]float32{0, 0}, []float32{1, 1}) != 0 {
		t.Error("zero magnitude should be 0")
	}
}
