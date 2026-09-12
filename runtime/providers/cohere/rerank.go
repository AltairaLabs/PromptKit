// Package cohere provides reranking via the Cohere Rerank API.
//
// Only the rerank role is implemented. Cohere also offers chat and embedding
// endpoints; those would be separate providers in this package if they are
// ever needed, and their absence is deliberate rather than an oversight.
package cohere

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Rerank model constants for Cohere.
const (
	// DefaultRerankModel is the recommended multilingual reranker.
	DefaultRerankModel = "rerank-v3.5"

	// ModelRerankV35 is Cohere's current general-purpose reranker.
	ModelRerankV35 = "rerank-v3.5"

	// ModelRerankEnglishV30 is the English-only v3 reranker.
	ModelRerankEnglishV30 = "rerank-english-v3.0"

	// ModelRerankMultilingualV30 is the multilingual v3 reranker.
	ModelRerankMultilingualV30 = "rerank-multilingual-v3.0"
)

// API constants.
const (
	cohereBaseURL        = "https://api.cohere.com/v2"
	cohereRerankEndpoint = "/rerank"
	cohereTimeout        = 60 * time.Second
	// cohereRerankMaxDocs is Cohere's documented per-request document cap.
	cohereRerankMaxDocs = 1000
)

// RerankProvider implements reranking via the Cohere Rerank API.
type RerankProvider struct {
	*providers.BaseRerankProvider
	// maxTokensPerDoc caps how much of each document Cohere considers.
	// Zero leaves Cohere's own default in place.
	maxTokensPerDoc int
}

// RerankOption configures the RerankProvider.
type RerankOption func(*RerankProvider)

// WithModel sets the rerank model.
func WithModel(model string) RerankOption {
	return func(p *RerankProvider) { p.ProviderModel = model }
}

// WithBaseURL sets a custom base URL.
func WithBaseURL(url string) RerankOption {
	return func(p *RerankProvider) { p.BaseURL = url }
}

// WithAPIKey sets the API key explicitly.
func WithAPIKey(key string) RerankOption {
	return func(p *RerankProvider) { p.APIKey = key }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) RerankOption {
	return func(p *RerankProvider) { p.HTTPClient = client }
}

// WithMaxTokensPerDoc caps how much of each document Cohere reads.
func WithMaxTokensPerDoc(n int) RerankOption {
	return func(p *RerankProvider) { p.maxTokensPerDoc = n }
}

// NewRerankProvider creates a Cohere rerank provider.
func NewRerankProvider(opts ...RerankOption) (*RerankProvider, error) {
	p := &RerankProvider{
		BaseRerankProvider: providers.NewBaseRerankProvider(
			"cohere-rerank",
			DefaultRerankModel,
			cohereBaseURL,
			cohereRerankMaxDocs,
			cohereTimeout,
		),
	}
	for _, opt := range opts {
		opt(p)
	}

	if !p.PlatformAuth {
		if p.APIKey == "" {
			p.APIKey = os.Getenv("COHERE_API_KEY")
		}
		if p.APIKey == "" {
			return nil, fmt.Errorf("cohere API key not found: set COHERE_API_KEY environment variable")
		}
	}
	return p, nil
}

// cohereRerankRequest is the Cohere v2 rerank API request format.
type cohereRerankRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopN            int      `json:"top_n,omitempty"`
	MaxTokensPerDoc int      `json:"max_tokens_per_doc,omitempty"`
}

// cohereRerankResponse is the Cohere v2 rerank API response format.
//
// Note that v2 does NOT echo the document text back — only indices and scores.
// That is why RankedDocument.Index is the authoritative identifier and
// RankedDocument.Document is documented as possibly empty.
type cohereRerankResponse struct {
	ID      string               `json:"id"`
	Results []cohereRerankResult `json:"results"`
	Meta    cohereMeta           `json:"meta"`
}

type cohereRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

type cohereMeta struct {
	BilledUnits cohereBilledUnits `json:"billed_units"`
}

type cohereBilledUnits struct {
	SearchUnits int `json:"search_units"`
}

// Rerank orders documents by relevance to the query.
func (p *RerankProvider) Rerank(
	ctx context.Context, req providers.RerankRequest,
) (providers.RerankResponse, error) {
	return p.RerankWithEmptyCheck(ctx, req, p.rerankDocuments)
}

func (p *RerankProvider) rerankDocuments(
	ctx context.Context, req providers.RerankRequest, model string,
) (providers.RerankResponse, error) {
	cohereReq := cohereRerankRequest{
		Model:           model,
		Query:           req.Query,
		Documents:       req.Documents,
		TopN:            req.TopN,
		MaxTokensPerDoc: p.maxTokensPerDoc,
	}

	reqBytes, err := providers.MarshalRequest(cohereReq)
	if err != nil {
		return providers.RerankResponse{}, err
	}

	start := time.Now()
	respBytes, err := p.DoRerankRequest(ctx, providers.HTTPRequestConfig{
		URL:       p.BaseURL + cohereRerankEndpoint,
		Body:      reqBytes,
		UseAPIKey: true,
	})
	if err != nil {
		return providers.RerankResponse{}, err
	}

	var cohereResp cohereRerankResponse
	if err := providers.UnmarshalResponse(respBytes, &cohereResp); err != nil {
		return providers.RerankResponse{}, err
	}

	keep := providers.ClampTopN(req.TopN, len(cohereResp.Results))
	results := make([]providers.RankedDocument, 0, keep)
	for i, r := range cohereResp.Results {
		if i >= keep {
			break
		}
		if r.Index < 0 || r.Index >= len(req.Documents) {
			return providers.RerankResponse{}, fmt.Errorf(
				"cohere-rerank: result index %d is outside the %d documents sent",
				r.Index, len(req.Documents))
		}
		results = append(results, providers.RankedDocument{
			Index: r.Index,
			Score: r.RelevanceScore,
			// Cohere v2 returns no document text; fill it from the request so
			// callers see the same shape whichever backend they configured.
			Document: req.Documents[r.Index],
		})
	}

	// Cohere bills rerank in search units, not tokens. Reporting search units
	// in TotalTokens would be a lie a cost dashboard would happily add up, so
	// usage is left nil and the unit count is logged instead.
	providers.LogRerankRequest("Cohere", model, len(req.Documents), cohereResp.Meta.BilledUnits.SearchUnits, start)

	return providers.RerankResponse{
		Results: results,
		Model:   model,
	}, nil
}

// Verify interface compliance.
var _ providers.RerankProvider = (*RerankProvider)(nil)
