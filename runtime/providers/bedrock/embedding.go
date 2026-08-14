// Package bedrock provides embedding generation via AWS Bedrock's
// platform-native models. Unlike the OpenAI-compatible providers, Bedrock
// embeddings use per-family request and response bodies posted to
// /model/{modelId}/invoke, so they need their own provider type rather than a
// transport-level rewrite of the OpenAI wire format.
//
// Two families are supported, selected from the model id:
//
//   - Amazon Titan (amazon.titan-embed-*): one text per call, {"inputText": …}
//   - Cohere (cohere.embed-*): natively batched, {"texts": […], "input_type": …}
//
// Authentication is AWS SigV4, applied by the HTTP client's transport (see
// providers.ResolveEmbeddingTransport with platform "bedrock"), so this
// package never handles credentials directly.
package bedrock

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Model family prefixes used to select the wire format.
const (
	titanModelPrefix  = "amazon.titan-embed"
	cohereModelPrefix = "cohere.embed"
)

// DefaultModel is Amazon's current general-purpose embedding model.
const DefaultModel = "amazon.titan-embed-text-v2:0"

// Dimension defaults per family. Titan v2 emits 1024 by default (configurable
// to 512/256); Titan v1 emits 1536; Cohere v3 emits 1024.
const (
	titanV2Dimensions  = 1024
	titanV1Dimensions  = 1536
	cohereDimensions   = 1024
	titanV1ModelSuffix = "text-v1"
)

// Batch limits. Titan's InvokeModel body carries a single string, so a
// multi-text request must fan out; Cohere accepts an array.
const (
	titanMaxBatch  = 1
	cohereMaxBatch = 96
)

// DefaultInputType is Cohere's required input_type for indexing documents.
// Use "search_query" when embedding a query for retrieval.
const DefaultInputType = "search_document"

const bedrockTimeout = 60 * time.Second

// EmbeddingProvider implements embedding generation via AWS Bedrock.
type EmbeddingProvider struct {
	*providers.BaseEmbeddingProvider
	inputType string
	// dimsExplicit records that the caller set Dimensions, so the family
	// default must not overwrite it — tracking the intent rather than
	// comparing against a default value, which cannot distinguish "unset"
	// from "set to the same number".
	dimsExplicit bool
}

// EmbeddingOption configures the EmbeddingProvider.
type EmbeddingOption func(*EmbeddingProvider)

// WithModel sets the Bedrock model id (e.g. amazon.titan-embed-text-v2:0).
func WithModel(model string) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.ProviderModel = model }
}

// WithBaseURL overrides the Bedrock runtime host. The provider appends
// /model/{modelId}/invoke per request.
func WithBaseURL(url string) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.BaseURL = url }
}

// WithHTTPClient sets the HTTP client. For real use this must be the
// SigV4-applying client from providers.ResolveEmbeddingTransport.
func WithHTTPClient(client *http.Client) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.HTTPClient = client }
}

// WithDimensions overrides the reported embedding dimensionality.
func WithDimensions(dims int) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.Dimensions = dims
		p.dimsExplicit = true
	}
}

// WithInputType sets Cohere's input_type ("search_document" or
// "search_query"). Ignored by Titan, which has no equivalent.
func WithInputType(inputType string) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.inputType = inputType }
}

// WithPlatformAuth marks the provider as authenticated by its HTTP client's
// transport. Bedrock has no API-key mode, so this is always the case in real
// use; the flag exists for symmetry with the other embedding providers.
func WithPlatformAuth() EmbeddingOption {
	return func(p *EmbeddingProvider) { p.PlatformAuth = true }
}

// WithWiring applies the transport-derived settings the factory resolved.
func WithWiring(w providers.EmbeddingWiring) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.dimsExplicit = p.ApplyWiring(w) }
}

// NewEmbeddingProvider creates a Bedrock embedding provider. It fails when the
// model is not a recognized embedding family, rather than guessing a wire
// format that the endpoint would reject at request time.
func NewEmbeddingProvider(opts ...EmbeddingOption) (*EmbeddingProvider, error) {
	p := &EmbeddingProvider{
		BaseEmbeddingProvider: providers.NewBaseEmbeddingProvider(
			"bedrock-embedding", DefaultModel, "", titanV2Dimensions, titanMaxBatch, bedrockTimeout,
		),
		inputType: DefaultInputType,
	}
	for _, opt := range opts {
		opt(p)
	}

	dims, batch, err := familyDefaults(p.ProviderModel)
	if err != nil {
		return nil, err
	}
	p.BatchSize = batch
	if !p.dimsExplicit {
		p.Dimensions = dims
	}
	return p, nil
}

// familyDefaults returns the dimension and batch defaults for a model id, and
// an error when the model is not a known Bedrock embedding family.
func familyDefaults(model string) (dims, batch int, err error) {
	switch {
	case strings.HasPrefix(model, titanModelPrefix):
		if strings.Contains(model, titanV1ModelSuffix) {
			return titanV1Dimensions, titanMaxBatch, nil
		}
		return titanV2Dimensions, titanMaxBatch, nil
	case strings.HasPrefix(model, cohereModelPrefix):
		return cohereDimensions, cohereMaxBatch, nil
	default:
		return 0, 0, fmt.Errorf(
			"unsupported bedrock embedding model %q: expected an %s* or %s* model",
			model, titanModelPrefix, cohereModelPrefix)
	}
}

// invokeURL builds the Bedrock InvokeModel URL for a model. The model id is
// part of the path (and may contain ':'), which the SigV4 signer URI-encodes.
func (p *EmbeddingProvider) invokeURL(model string) string {
	return strings.TrimSuffix(p.BaseURL, "/") + "/model/" + model + "/invoke"
}

// titanRequest is Amazon Titan's InvokeModel body: exactly one text.
type titanRequest struct {
	InputText string `json:"inputText"`
}

type titanResponse struct {
	Embedding           []float32 `json:"embedding"`
	InputTextTokenCount int       `json:"inputTextTokenCount"`
}

// cohereRequest is Cohere's InvokeModel body. input_type is required.
type cohereRequest struct {
	Texts     []string `json:"texts"`
	InputType string   `json:"input_type"`
}

type cohereResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
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
	if strings.HasPrefix(model, cohereModelPrefix) {
		return p.embedCohere(ctx, texts, model)
	}
	return p.embedTitan(ctx, texts, model)
}

// embedTitan fans out one request per text and reassembles in order. Titan's
// body holds a single string, so batching cannot be pushed to the endpoint.
func (p *EmbeddingProvider) embedTitan(
	ctx context.Context, texts []string, model string,
) (providers.EmbeddingResponse, error) {
	start := time.Now()
	embeddings := make([][]float32, len(texts))
	total := 0

	for i, text := range texts {
		body, err := providers.MarshalRequest(titanRequest{InputText: text})
		if err != nil {
			return providers.EmbeddingResponse{}, err
		}
		respBytes, err := p.DoEmbeddingRequest(ctx, providers.HTTPRequestConfig{
			URL:  p.invokeURL(model),
			Body: body,
		})
		if err != nil {
			return providers.EmbeddingResponse{}, err
		}
		var resp titanResponse
		if err := providers.UnmarshalResponse(respBytes, &resp); err != nil {
			return providers.EmbeddingResponse{}, err
		}
		embeddings[i] = resp.Embedding
		total += resp.InputTextTokenCount
	}

	providers.LogEmbeddingRequestWithTokens("Bedrock Titan", model, len(texts), total, start)
	return providers.EmbeddingResponse{
		Embeddings: embeddings,
		Model:      model,
		Usage:      &providers.EmbeddingUsage{TotalTokens: total},
	}, nil
}

// embedCohere sends all texts in one call; Cohere returns them in input order.
func (p *EmbeddingProvider) embedCohere(
	ctx context.Context, texts []string, model string,
) (providers.EmbeddingResponse, error) {
	start := time.Now()
	inputType := p.inputType
	if inputType == "" {
		inputType = DefaultInputType
	}

	body, err := providers.MarshalRequest(cohereRequest{Texts: texts, InputType: inputType})
	if err != nil {
		return providers.EmbeddingResponse{}, err
	}
	respBytes, err := p.DoEmbeddingRequest(ctx, providers.HTTPRequestConfig{
		URL:  p.invokeURL(model),
		Body: body,
	})
	if err != nil {
		return providers.EmbeddingResponse{}, err
	}
	var resp cohereResponse
	if err := providers.UnmarshalResponse(respBytes, &resp); err != nil {
		return providers.EmbeddingResponse{}, err
	}

	providers.LogEmbeddingRequest("Bedrock Cohere", model, len(texts), start)
	return providers.EmbeddingResponse{Embeddings: resp.Embeddings, Model: model}, nil
}

// Verify interface compliance.
var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)
