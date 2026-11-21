// Package bedrock provides embedding generation via AWS Bedrock's
// platform-native models. Unlike the OpenAI-compatible providers, Bedrock
// embeddings use per-family request and response bodies posted to
// /model/{modelId}/invoke, so they need their own provider type rather than a
// transport-level rewrite of the OpenAI wire format.
//
// Two families are supported:
//
//   - Amazon Titan (amazon.titan-embed-*): one text per call, {"inputText": …}
//   - Cohere (cohere.embed-*): natively batched, {"texts": […], "input_type": …}
//
// The family is the declared format (WithEmbeddingFormat, or
// additional_config.embedding_format) when there is one, so an inference
// profile, an ARN or any other model id works. Without one it is read from the
// model id's prefix, and a model id outside both prefixes is refused.
//
// Authentication is AWS SigV4, applied by the HTTP client's transport (see
// providers.ResolveEmbeddingTransport with platform "bedrock"), so this
// package never handles credentials directly.
package bedrock

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Model family prefixes used to select the wire format when none is declared.
const (
	titanModelPrefix  = "amazon.titan-embed"
	cohereModelPrefix = "cohere.embed"
)

// The declarable request formats (additional_config.embedding_format).
const (
	FormatTitan  = "titan"
	FormatCohere = "cohere"
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
	// titanV2ModelSuffix marks the only Titan model whose body accepts
	// dimensions; v1 and the image model reject it.
	titanV2ModelSuffix = "text-v2"
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
	// format is the declared request format, FormatTitan or FormatCohere; ""
	// reads the family from the model id.
	format string
	// batchSize is the declared batch size, 0 for the family's.
	batchSize int
	// dimsExplicit records that the caller set Dimensions, so the family
	// default must not overwrite it — tracking the intent rather than
	// comparing against a default value, which cannot distinguish "unset"
	// from "set to the same number".
	dimsExplicit bool
}

// EmbeddingOption configures the EmbeddingProvider.
type EmbeddingOption func(*EmbeddingProvider)

// WithInputType sets Cohere's input_type ("search_document" or
// "search_query"). Ignored by Titan, which has no equivalent.
func WithInputType(inputType string) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.inputType = inputType }
}

// WithEmbeddingFormat declares the request format, FormatTitan or
// FormatCohere. It wins over the model id, so a model id that does not start
// with a family prefix (an inference profile such as us.cohere.embed-v4:0, or
// an ARN) can be used. NewEmbeddingProvider rejects any other value.
func WithEmbeddingFormat(format string) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.format = format }
}

// WithBatchSize declares the most texts one request may carry, replacing the
// family default that MaxBatchSize reports. A size above the family's API
// limit is clamped to the limit, with a warning. Values below 1 are ignored.
func WithBatchSize(n int) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		if n > 0 {
			p.batchSize = n
		}
	}
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

// NewEmbeddingProvider creates a Bedrock embedding provider. Without a declared
// format it fails when the model is not a recognized embedding family, rather
// than guessing a wire format that the endpoint would reject at request time.
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

	dims, batch, err := p.familyDefaults(p.ProviderModel)
	if err != nil {
		return nil, err
	}
	// The family's batch size is the API's limit (Titan's body holds one text,
	// Cohere's at most 96), so a declared size may lower it, never raise it.
	p.BatchSize = batch
	p.ApplyDeclaredBatchSize(p.batchSize, batch)
	if !p.dimsExplicit {
		p.Dimensions = dims
	}
	return p, nil
}

// familyDefaults returns the dimension and batch defaults for a model id.
//
// A declared format sets the batch size. The dimensions are the model's known
// size when the id is one the prefix rule recognizes, else 0, so the size is
// taken from the first response. Without a declared format, a model id
// outside both prefixes is an error.
func (p *EmbeddingProvider) familyDefaults(model string) (dims, batch int, err error) {
	switch p.format {
	case "":
	case FormatTitan, FormatCohere:
		dims, _, err = familyDefaultsByName(model)
		if err != nil {
			dims = 0
		}
		if p.format == FormatCohere {
			return dims, cohereMaxBatch, nil
		}
		return dims, titanMaxBatch, nil
	default:
		return 0, 0, fmt.Errorf(
			"bedrock embedding: additional_config.embedding_format must be %s or %s, got %q",
			FormatTitan, FormatCohere, p.format)
	}
	return familyDefaultsByName(model)
}

// familyDefaultsByName returns the dimension and batch defaults the model id's
// prefix implies, and an error when the prefix is not a known family.
func familyDefaultsByName(model string) (dims, batch int, err error) {
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
			"unsupported bedrock embedding model %q: expected an %s* or %s* model, "+
				"or set additional_config.embedding_format to %s or %s",
			model, titanModelPrefix, cohereModelPrefix, FormatTitan, FormatCohere)
	}
}

// usesCohere reports whether a request for model takes the Cohere body: the
// declared format, else the model id's prefix.
func (p *EmbeddingProvider) usesCohere(model string) bool {
	if p.format != "" {
		return p.format == FormatCohere
	}
	return strings.HasPrefix(model, cohereModelPrefix)
}

// sendsDimensions reports whether a Titan request for model carries the
// declared size. Of the Titan ids, only text-v2 accepts it; v1 and the image
// model reject it. Any other id (a profile or ARN under a declared format) is
// sent the size the config declared.
func (p *EmbeddingProvider) sendsDimensions(model string) bool {
	if !p.dimsExplicit {
		return false
	}
	return !strings.Contains(model, titanModelPrefix) || strings.Contains(model, titanV2ModelSuffix)
}

// invokeURL builds the Bedrock InvokeModel URL for a model. The model id is
// part of the path (and may contain ':'), which the SigV4 signer URI-encodes.
func (p *EmbeddingProvider) invokeURL(model string) string {
	return strings.TrimSuffix(p.BaseURL, "/") + "/model/" + model + "/invoke"
}

// titanRequest is Amazon Titan's InvokeModel body: exactly one text.
type titanRequest struct {
	InputText  string `json:"inputText"`
	Dimensions int    `json:"dimensions,omitempty"`
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
	if p.usesCohere(model) {
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
		tr := titanRequest{InputText: text}
		if p.sendsDimensions(model) {
			tr.Dimensions = p.Dimensions
		}
		body, err := providers.MarshalRequest(tr)
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
