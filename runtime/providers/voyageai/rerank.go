package voyageai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Rerank model constants for Voyage AI.
const (
	// DefaultRerankModel is the recommended general-purpose reranker.
	DefaultRerankModel = "rerank-2.5"

	// ModelRerank25 is the latest general-purpose reranker.
	ModelRerank25 = "rerank-2.5"

	// ModelRerank25Lite is the lower-latency, lower-cost reranker.
	ModelRerank25Lite = "rerank-2.5-lite"
)

const (
	voyageRerankEndpoint = "/rerank"
	// voyageRerankMaxDocs is Voyage's documented per-request document cap.
	voyageRerankMaxDocs = 1000
)

// RerankProvider implements reranking via the Voyage AI API.
type RerankProvider struct {
	*providers.BaseRerankProvider
	// truncation lets Voyage trim over-long documents to the model's context
	// rather than rejecting the call. Defaults to Voyage's own default when
	// unset, which is why this is a pointer.
	truncation *bool
}

// RerankOption configures the RerankProvider.
type RerankOption func(*RerankProvider)

// WithRerankModel sets the rerank model.
func WithRerankModel(model string) RerankOption {
	return func(p *RerankProvider) { p.ProviderModel = model }
}

// WithRerankBaseURL sets a custom base URL.
func WithRerankBaseURL(url string) RerankOption {
	return func(p *RerankProvider) { p.BaseURL = url }
}

// WithRerankAPIKey sets the API key explicitly.
func WithRerankAPIKey(key string) RerankOption {
	return func(p *RerankProvider) { p.APIKey = key }
}

// WithRerankHTTPClient sets a custom HTTP client.
func WithRerankHTTPClient(client *http.Client) RerankOption {
	return func(p *RerankProvider) { p.HTTPClient = client }
}

// WithRerankTruncation asks Voyage to truncate over-long documents instead of
// failing the request.
func WithRerankTruncation(truncate bool) RerankOption {
	return func(p *RerankProvider) { p.truncation = &truncate }
}

// NewRerankProvider creates a Voyage AI rerank provider.
func NewRerankProvider(opts ...RerankOption) (*RerankProvider, error) {
	p := &RerankProvider{
		BaseRerankProvider: providers.NewBaseRerankProvider(
			"voyageai-rerank",
			DefaultRerankModel,
			voyageBaseURL,
			voyageRerankMaxDocs,
			voyageTimeout,
		),
	}
	for _, opt := range opts {
		opt(p)
	}

	if !p.PlatformAuth {
		if p.APIKey == "" {
			p.APIKey = os.Getenv("VOYAGE_API_KEY")
		}
		if p.APIKey == "" {
			return nil, fmt.Errorf("voyage AI API key not found: set VOYAGE_API_KEY environment variable")
		}
	}
	return p, nil
}

// voyageRerankRequest is the Voyage AI rerank API request format.
type voyageRerankRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopK            int      `json:"top_k,omitempty"`
	ReturnDocuments bool     `json:"return_documents,omitempty"`
	Truncation      *bool    `json:"truncation,omitempty"`
}

// voyageRerankResponse is the Voyage AI rerank API response format.
type voyageRerankResponse struct {
	Object string               `json:"object"`
	Data   []voyageRerankResult `json:"data"`
	Model  string               `json:"model"`
	Usage  voyageUsage          `json:"usage"`
}

type voyageRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
	Document       string  `json:"document,omitempty"`
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
	voyageReq := voyageRerankRequest{
		Model:     model,
		Query:     req.Query,
		Documents: req.Documents,
		TopK:      req.TopN,
		// Ask for the text back so callers that only keep the strings do not
		// have to re-index into their own slice. Index remains authoritative.
		ReturnDocuments: true,
		Truncation:      p.truncation,
	}

	reqBytes, err := providers.MarshalRequest(voyageReq)
	if err != nil {
		return providers.RerankResponse{}, err
	}

	start := time.Now()
	respBytes, err := p.DoRerankRequest(ctx, providers.HTTPRequestConfig{
		URL:       p.BaseURL + voyageRerankEndpoint,
		Body:      reqBytes,
		UseAPIKey: true,
	})
	if err != nil {
		return providers.RerankResponse{}, err
	}

	var voyageResp voyageRerankResponse
	if err := providers.UnmarshalResponse(respBytes, &voyageResp); err != nil {
		return providers.RerankResponse{}, err
	}

	keep := providers.ClampTopN(req.TopN, len(voyageResp.Data))
	results := make([]providers.RankedDocument, 0, keep)
	for i, d := range voyageResp.Data {
		if i >= keep {
			break
		}
		// A returned index outside the request would make the caller read the
		// wrong document — refuse rather than hand back a plausible-looking
		// mismatch.
		if d.Index < 0 || d.Index >= len(req.Documents) {
			return providers.RerankResponse{}, fmt.Errorf(
				"voyageai-rerank: result index %d is outside the %d documents sent",
				d.Index, len(req.Documents))
		}
		results = append(results, providers.RankedDocument{
			Index:    d.Index,
			Score:    d.RelevanceScore,
			Document: d.Document,
		})
	}

	providers.LogRerankRequest("Voyage AI", model, len(req.Documents), voyageResp.Usage.TotalTokens, start)

	return providers.RerankResponse{
		Results: results,
		Model:   voyageResp.Model,
		Usage:   &providers.RerankUsage{TotalTokens: voyageResp.Usage.TotalTokens},
	}, nil
}

// Verify interface compliance.
var _ providers.RerankProvider = (*RerankProvider)(nil)
