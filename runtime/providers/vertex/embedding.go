// Package vertex provides embedding generation via Google Vertex AI's
// text-embedding models.
//
// Vertex is not OpenAI-wire-compatible: embeddings are produced by POSTing to
// .../publishers/google/models/{model}:predict with
//
//	{"instances":[{"content":"…","task_type":"…"}],"parameters":{…}}
//
// and reading back
//
//	{"predictions":[{"embeddings":{"values":[…],"statistics":{"token_count":N}}}]}
//
// so it needs its own provider type rather than a transport-level rewrite of an
// OpenAI body (#1301).
//
// Authentication is a GCP OAuth2 bearer token applied by the HTTP client's
// transport (see providers.ResolveEmbeddingTransport with platform "vertex"),
// so this package never handles credentials directly.
package vertex

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Model family prefixes used to select defaults.
const (
	textEmbeddingPrefix   = "text-embedding"
	geckoEmbeddingPrefix  = "textembedding-gecko"
	geminiEmbeddingPrefix = "gemini-embedding"
)

// DefaultModel is Google's current general-purpose text embedding model.
const DefaultModel = "text-embedding-005"

// Family defaults. These are the documented defaults at time of writing and
// are overridable per provider: the wiring's Dimensions sets
// outputDimensionality, and
// the batch size is what the caller's batching loop is told to respect. They
// are not enforced by this package — the endpoint is the authority, and a
// wrong guess here surfaces as an API error rather than silent truncation.
const (
	textEmbeddingDimensions   = 768
	geminiEmbeddingDimensions = 3072
	textEmbeddingMaxBatch     = 250
	geminiEmbeddingMaxBatch   = 1
)

// DefaultTaskType is Vertex's task type for embedding documents to be indexed.
// Use "RETRIEVAL_QUERY" when embedding a search query; the two produce
// different vectors on purpose.
const DefaultTaskType = "RETRIEVAL_DOCUMENT"

const vertexTimeout = 60 * time.Second

// EmbeddingProvider implements embedding generation via Vertex AI.
type EmbeddingProvider struct {
	*providers.BaseEmbeddingProvider
	taskType string
	// dimsExplicit records that the caller set Dimensions, so the family
	// default must not overwrite it and outputDimensionality is only sent when
	// actually requested — tracking intent rather than comparing to a default,
	// which cannot tell "unset" from "set to the same number".
	dimsExplicit bool
}

// EmbeddingOption configures the EmbeddingProvider.
type EmbeddingOption func(*EmbeddingProvider)

// WithTaskType sets the Vertex task type ("RETRIEVAL_DOCUMENT",
// "RETRIEVAL_QUERY", "SEMANTIC_SIMILARITY", …).
func WithTaskType(taskType string) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.taskType = taskType }
}

// WithPlatformAuth marks the provider as authenticated by its HTTP client's
// transport. Vertex has no API-key mode, so this is always the case in real
// use; the flag exists for symmetry with the other embedding providers.
func WithPlatformAuth() EmbeddingOption {
	return func(p *EmbeddingProvider) { p.PlatformAuth = true }
}

// WithWiring applies the transport-derived settings the factory resolved.
func WithWiring(w providers.EmbeddingWiring) EmbeddingOption {
	return func(p *EmbeddingProvider) { p.dimsExplicit = p.ApplyWiring(w) }
}

// NewEmbeddingProvider creates a Vertex embedding provider. It fails when the
// model is not a recognized embedding family rather than sending a body the
// endpoint would reject at request time.
func NewEmbeddingProvider(opts ...EmbeddingOption) (*EmbeddingProvider, error) {
	p := &EmbeddingProvider{
		BaseEmbeddingProvider: providers.NewBaseEmbeddingProvider(
			"vertex-embedding", DefaultModel, "",
			textEmbeddingDimensions, textEmbeddingMaxBatch, vertexTimeout,
		),
		taskType: DefaultTaskType,
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
// an error when the model is not a known Vertex embedding family.
func familyDefaults(model string) (dims, batch int, err error) {
	switch {
	case strings.HasPrefix(model, geminiEmbeddingPrefix):
		return geminiEmbeddingDimensions, geminiEmbeddingMaxBatch, nil
	case strings.HasPrefix(model, textEmbeddingPrefix),
		strings.HasPrefix(model, geckoEmbeddingPrefix):
		return textEmbeddingDimensions, textEmbeddingMaxBatch, nil
	default:
		return 0, 0, fmt.Errorf(
			"unsupported vertex embedding model %q: expected a %s*, %s* or %s* model",
			model, textEmbeddingPrefix, geckoEmbeddingPrefix, geminiEmbeddingPrefix)
	}
}

// predictURL builds the :predict URL for a model. The model is part of the
// path, so a per-request override changes the URL rather than the body.
func (p *EmbeddingProvider) predictURL(model string) string {
	return strings.TrimSuffix(p.BaseURL, "/") + "/" + model + ":predict"
}

// predictInstance is one text to embed. task_type is omitted when unset so a
// model that does not accept it is not sent one.
type predictInstance struct {
	Content  string `json:"content"`
	TaskType string `json:"task_type,omitempty"`
}

// predictParameters carries request-level knobs. Omitted entirely when empty.
type predictParameters struct {
	OutputDimensionality int `json:"outputDimensionality,omitempty"`
}

type predictRequest struct {
	Instances  []predictInstance  `json:"instances"`
	Parameters *predictParameters `json:"parameters,omitempty"`
}

type predictResponse struct {
	Predictions []struct {
		Embeddings struct {
			Values     []float32 `json:"values"`
			Statistics struct {
				TokenCount int `json:"token_count"`
			} `json:"statistics"`
		} `json:"embeddings"`
	} `json:"predictions"`
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
	start := time.Now()

	instances := make([]predictInstance, len(texts))
	for i, text := range texts {
		instances[i] = predictInstance{Content: text, TaskType: p.taskType}
	}
	request := predictRequest{Instances: instances}
	if p.dimsExplicit && p.Dimensions > 0 {
		request.Parameters = &predictParameters{OutputDimensionality: p.Dimensions}
	}

	body, err := providers.MarshalRequest(request)
	if err != nil {
		return providers.EmbeddingResponse{}, err
	}
	respBytes, err := p.DoEmbeddingRequest(ctx, providers.HTTPRequestConfig{
		URL:  p.predictURL(model),
		Body: body,
	})
	if err != nil {
		return providers.EmbeddingResponse{}, err
	}

	var resp predictResponse
	if err := providers.UnmarshalResponse(respBytes, &resp); err != nil {
		return providers.EmbeddingResponse{}, err
	}
	// A short predictions array would otherwise be returned as fewer vectors
	// than texts, silently misaligning whatever index consumes them.
	if len(resp.Predictions) != len(texts) {
		return providers.EmbeddingResponse{}, fmt.Errorf(
			"vertex returned %d embeddings for %d texts", len(resp.Predictions), len(texts))
	}

	embeddings := make([][]float32, len(resp.Predictions))
	total := 0
	for i := range resp.Predictions {
		embeddings[i] = resp.Predictions[i].Embeddings.Values
		total += resp.Predictions[i].Embeddings.Statistics.TokenCount
	}

	providers.LogEmbeddingRequestWithTokens("Vertex", model, len(texts), total, start)
	return providers.EmbeddingResponse{
		Embeddings: embeddings,
		Model:      model,
		Usage:      &providers.EmbeddingUsage{TotalTokens: total},
	}, nil
}
