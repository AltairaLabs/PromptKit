package providers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// BaseRerankProvider carries the transport state and lifecycle every hosted
// reranker needs, so a vendor package only has to describe its wire format.
// It mirrors BaseEmbeddingProvider, with base.Implementation embedded to
// satisfy base.Provider (Name/Type/Pricing/Validate/Init/HealthCheck/Close).
type BaseRerankProvider struct {
	*base.Implementation

	ProviderID    string
	ProviderModel string
	BaseURL       string
	APIKey        string
	HTTPClient    *http.Client
	// MaxDocs is the largest candidate list one call accepts.
	MaxDocs int
	// PlatformAuth indicates the HTTPClient's transport applies
	// hyperscaler-platform auth per request, so the empty-API-key guard in
	// each vendor's constructor must be skipped.
	PlatformAuth bool
}

// NewBaseRerankProvider creates a base rerank provider with vendor defaults.
func NewBaseRerankProvider(
	providerID, defaultModel, defaultBaseURL string,
	defaultMaxDocs int,
	defaultTimeout time.Duration,
) *BaseRerankProvider {
	return &BaseRerankProvider{
		Implementation: base.NewImplementation(providerID, base.ProviderTypeRerank, nil),
		ProviderID:     providerID,
		ProviderModel:  defaultModel,
		BaseURL:        defaultBaseURL,
		MaxDocs:        defaultMaxDocs,
		HTTPClient:     &http.Client{Timeout: defaultTimeout},
	}
}

// ID returns the configured provider identifier.
func (b *BaseRerankProvider) ID() string { return b.ProviderID }

// Model returns the current rerank model.
func (b *BaseRerankProvider) Model() string { return b.ProviderModel }

// MaxDocuments returns the largest candidate list one call accepts.
func (b *BaseRerankProvider) MaxDocuments() int { return b.MaxDocs }

// ResolveModel returns the per-request model override, or the provider default.
func (b *BaseRerankProvider) ResolveModel(reqModel string) string {
	if reqModel != "" {
		return reqModel
	}
	return b.ProviderModel
}

// RerankWithEmptyCheck short-circuits the degenerate requests every vendor
// would otherwise have to guard against, and delegates the rest.
//
// No documents is NOT an error: an upstream search returning nothing is a
// normal outcome, and making it an error would force every caller to
// distinguish "search found nothing" from "the reranker is down". An empty
// query IS an error — ranking against nothing is meaningless, and silently
// returning the input order would look like the reranker had run.
func (b *BaseRerankProvider) RerankWithEmptyCheck(
	ctx context.Context,
	req RerankRequest,
	rerank func(ctx context.Context, req RerankRequest, model string) (RerankResponse, error),
) (RerankResponse, error) {
	if req.Query == "" {
		return RerankResponse{}, fmt.Errorf("%s: rerank query must not be empty", b.ProviderID)
	}
	model := b.ResolveModel(req.Model)
	if len(req.Documents) == 0 {
		return RerankResponse{Results: []RankedDocument{}, Model: model}, nil
	}
	if b.MaxDocs > 0 && len(req.Documents) > b.MaxDocs {
		return RerankResponse{}, fmt.Errorf(
			"%s: %d documents exceeds the provider maximum of %d; batch the call",
			b.ProviderID, len(req.Documents), b.MaxDocs)
	}
	return rerank(ctx, req, model)
}

// DoRerankRequest performs the vendor's HTTP call. It shares
// DoAncillaryJSONRequest with the embedding path so both roles wrap transport
// failures the same way — which matters, because that wrapping is what redacts
// credential-bearing URLs and makes failures classifiable by IsTransient.
func (b *BaseRerankProvider) DoRerankRequest(
	ctx context.Context, cfg HTTPRequestConfig,
) ([]byte, error) {
	return DoAncillaryJSONRequest(ctx, b.HTTPClient, b.ProviderID, b.APIKey, cfg)
}

// ClampTopN returns the number of results to keep: TopN when it is set and
// smaller than what came back, otherwise everything. Vendors that honor top_n
// server-side still call this, because a provider is free to return more than
// asked and the interface promises it never does.
func ClampTopN(topN, available int) int {
	if topN > 0 && topN < available {
		return topN
	}
	return available
}

// LogRerankRequest records a completed rerank at debug level, matching the
// embedding path's logging so the two roles read alike in a trace.
func LogRerankRequest(provider, model string, docCount, tokens int, start time.Time) {
	logger.Debug("rerank request completed",
		"provider", provider,
		"model", model,
		"documents", docCount,
		"tokens", tokens,
		"duration_ms", time.Since(start).Milliseconds())
}
