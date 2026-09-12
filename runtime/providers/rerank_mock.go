package providers

import (
	"context"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// mockRerankProviderType is the spec type that selects the in-process mock.
const mockRerankProviderType = "mock"

// MockRerankProvider ranks without a network call, for tests and local
// development.
//
// It scores by counting how many of the query's whitespace-separated terms
// appear in each document, case-insensitively. That is deliberately crude —
// it is not trying to be a good reranker, it is trying to be a *predictable*
// one, so a test can assert an exact order without pinning a vendor model's
// behavior. Ties keep the input order, so the result is fully deterministic.
//
// Scores are normalized to 0..1 (matched terms over query terms) purely so
// they look like the hosted providers' scores; they are not comparable to
// them, which is true of any two rerankers.
type MockRerankProvider struct {
	*base.Implementation

	// Handler, when set, replaces the built-in scoring entirely. Use it to
	// pin an exact order, or to make the provider fail, without standing up
	// an HTTP server.
	Handler func(ctx context.Context, req RerankRequest) (RerankResponse, error)

	id      string
	maxDocs int
}

// MockRerankOption configures a MockRerankProvider.
type MockRerankOption func(*MockRerankProvider)

// WithMockRerankID sets the provider's reported ID.
func WithMockRerankID(id string) MockRerankOption {
	return func(p *MockRerankProvider) { p.id = id }
}

// WithMockRerankHandler installs a handler that replaces the built-in scoring.
func WithMockRerankHandler(
	h func(ctx context.Context, req RerankRequest) (RerankResponse, error),
) MockRerankOption {
	return func(p *MockRerankProvider) { p.Handler = h }
}

// WithMockRerankMaxDocuments sets the reported document cap, so a test can
// exercise a caller's batching without sending a thousand documents.
func WithMockRerankMaxDocuments(n int) MockRerankOption {
	return func(p *MockRerankProvider) { p.maxDocs = n }
}

// NewMockRerankProvider creates an in-process rerank provider.
func NewMockRerankProvider(opts ...MockRerankOption) *MockRerankProvider {
	p := &MockRerankProvider{
		Implementation: base.NewImplementation("mock-rerank", base.ProviderTypeRerank, nil),
		id:             "mock-rerank",
		maxDocs:        defaultMockRerankMaxDocs,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

const defaultMockRerankMaxDocs = 1000

// ID returns the provider identifier.
func (p *MockRerankProvider) ID() string { return p.id }

// MaxDocuments returns the configured document cap.
func (p *MockRerankProvider) MaxDocuments() int { return p.maxDocs }

// Rerank orders documents by term overlap with the query.
func (p *MockRerankProvider) Rerank(
	ctx context.Context, req RerankRequest,
) (RerankResponse, error) {
	if p.Handler != nil {
		return p.Handler(ctx, req)
	}
	// Honor cancellation even though there is no I/O: a caller testing its
	// own timeout handling should see the same behavior it would get live.
	if err := ctx.Err(); err != nil {
		return RerankResponse{}, err
	}

	terms := strings.Fields(strings.ToLower(req.Query))
	scored := make([]RankedDocument, len(req.Documents))
	for i, doc := range req.Documents {
		lower := strings.ToLower(doc)
		var hits int
		for _, term := range terms {
			if strings.Contains(lower, term) {
				hits++
			}
		}
		var score float64
		if len(terms) > 0 {
			score = float64(hits) / float64(len(terms))
		}
		scored[i] = RankedDocument{Index: i, Score: score, Document: doc}
	}

	// SliceStable so equal scores keep input order — an unstable sort would
	// make this mock's output vary between runs and turn any test that
	// asserts a full ordering into a flake.
	sort.SliceStable(scored, func(a, b int) bool { return scored[a].Score > scored[b].Score })

	keep := ClampTopN(req.TopN, len(scored))
	return RerankResponse{
		Results: scored[:keep],
		Model:   p.resolveModel(req.Model),
	}, nil
}

func (p *MockRerankProvider) resolveModel(reqModel string) string {
	if reqModel != "" {
		return reqModel
	}
	return "mock-rerank-v1"
}

//nolint:gochecknoinits // Factory registration requires init
func init() {
	RegisterRerankProviderFactory(mockRerankProviderType,
		func(spec RerankProviderSpec) (RerankProvider, error) {
			opts := []MockRerankOption{}
			if spec.ID != "" {
				opts = append(opts, WithMockRerankID(spec.ID))
			}
			if n, ok := IntFromConfig(spec.AdditionalConfig, "max_documents"); ok {
				opts = append(opts, WithMockRerankMaxDocuments(n))
			}
			return NewMockRerankProvider(opts...), nil
		},
	)
}

// Verify interface compliance.
var _ RerankProvider = (*MockRerankProvider)(nil)
