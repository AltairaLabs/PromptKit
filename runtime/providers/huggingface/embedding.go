// Package huggingface implements providers.EmbeddingProvider against Hugging
// Face's feature-extraction API: the serverless Inference Providers router
// (https://router.huggingface.co/hf-inference) and dedicated Inference
// Endpoints, which serve the same request and response shape.
package huggingface

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

const (
	// DefaultBaseURL is the serverless router's hf-inference provider.
	DefaultBaseURL = "https://router.huggingface.co/hf-inference"
	// DefaultModel is a small, widely used sentence-embedding model.
	DefaultModel = "sentence-transformers/all-MiniLM-L6-v2"

	// featureExtractionPath pins the pipeline. The bare /models/{id} route
	// lets HF pick the model's default pipeline, and for sentence-transformers
	// models that is sentence-similarity, which rejects a plain input list.
	featureExtractionPath = "/pipeline/feature-extraction"

	defaultBatchSize = 32
	defaultTimeout   = 60 * time.Second
)

// EmbeddingProvider embeds text with a Hugging Face feature-extraction model.
type EmbeddingProvider struct {
	*providers.BaseEmbeddingProvider
	// dedicated means BaseURL is an Inference Endpoint that serves exactly
	// one model, so requests go to it directly with no /models/{id} path.
	dedicated bool
}

// EmbeddingOption configures the EmbeddingProvider.
type EmbeddingOption func(*EmbeddingProvider)

// WithWiring applies the transport-derived settings the factory resolved.
// A declared dimensions is reported and checked, but not sent: the
// feature-extraction API has no parameter for it.
func WithWiring(w providers.EmbeddingWiring) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.ApplyWiring(w) }
}

// WithAPIKey sets the Hugging Face token explicitly.
func WithAPIKey(key string) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.APIKey = key }
}

// WithDedicatedEndpoint marks BaseURL as a dedicated Inference Endpoint.
func WithDedicatedEndpoint() EmbeddingOption {
	return func(p *EmbeddingProvider) { p.dedicated = true }
}

// NewEmbeddingProvider creates a Hugging Face embedding provider. The model's
// vector size is the declared dimensions if any, otherwise the length of the
// first vector returned — Hugging Face hosts too many models for a lookup
// table to be anything but a guess.
func NewEmbeddingProvider(opts ...EmbeddingOption) (*EmbeddingProvider, error) {
	p := &EmbeddingProvider{
		BaseEmbeddingProvider: providers.NewBaseEmbeddingProvider(
			"huggingface-embedding", DefaultModel, DefaultBaseURL, 0, defaultBatchSize, defaultTimeout,
		),
	}
	for _, opt := range opts {
		opt(p)
	}
	p.BaseURL = strings.TrimSuffix(p.BaseURL, "/")
	if p.APIKey == "" && !p.PlatformAuth {
		_, p.APIKey = providers.NewBaseProviderWithAPIKey("", false, "HF_TOKEN", "HUGGING_FACE_HUB_TOKEN")
	}
	if p.APIKey == "" && !p.dedicated {
		return nil, fmt.Errorf("hugging face token not found: set HF_TOKEN")
	}
	return p, nil
}

// Embed generates embeddings for the given texts.
func (p *EmbeddingProvider) Embed(
	ctx context.Context, req providers.EmbeddingRequest,
) (providers.EmbeddingResponse, error) {
	return p.EmbedWithEmptyCheck(ctx, req, p.embedTexts)
}

func (p *EmbeddingProvider) embedTexts(
	ctx context.Context, texts []string, model string,
) (providers.EmbeddingResponse, error) {
	out := providers.EmbeddingResponse{Model: model, Embeddings: make([][]float32, 0, len(texts))}
	for start := 0; start < len(texts); start += p.BatchSize {
		end := start + p.BatchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := p.embedBatch(ctx, texts[start:end], model)
		if err != nil {
			return providers.EmbeddingResponse{}, err
		}
		out.Embeddings = append(out.Embeddings, vecs...)
	}
	return out, nil
}

type featureExtractionRequest struct {
	Inputs []string `json:"inputs"`
}

func (p *EmbeddingProvider) embedBatch(ctx context.Context, texts []string, model string) ([][]float32, error) {
	body, err := providers.MarshalRequest(featureExtractionRequest{Inputs: texts})
	if err != nil {
		return nil, err
	}
	start := time.Now()
	respBody, err := p.DoEmbeddingRequest(ctx, providers.HTTPRequestConfig{
		URL:       p.requestURL(model),
		Body:      body,
		UseAPIKey: true,
	})
	if err != nil {
		return nil, err
	}
	var vecs [][]float32
	if err := json.Unmarshal(respBody, &vecs); err != nil {
		// A per-token model returns [batch][tokens][dims]; it needs pooling
		// the API did not do, and is not an embedding model in this sense.
		return nil, fmt.Errorf("huggingface: model %q did not return one vector per input: %w", model, err)
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("huggingface: sent %d inputs, got %d vectors", len(texts), len(vecs))
	}
	providers.LogEmbeddingRequest("HuggingFace", model, len(texts), start)
	return vecs, nil
}

// requestURL targets the model's feature-extraction pipeline on the router,
// or the endpoint itself when it is dedicated to one model.
func (p *EmbeddingProvider) requestURL(model string) string {
	if p.dedicated {
		return p.BaseURL
	}
	segments := strings.Split(model, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return p.BaseURL + "/models/" + strings.Join(segments, "/") + featureExtractionPath
}

// Verify interface compliance.
var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)
