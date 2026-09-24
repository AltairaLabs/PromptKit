// Package huggingface implements inference.Provider over the HuggingFace
// Inference API: HF's serverless router (router.huggingface.co/hf-inference)
// or a dedicated HF Inference Endpoint.
//
// A single Provider serves three shapes of call, chosen by the Request's
// content: a candidate-label list routes to HF's zero-shot-classification
// pipeline, an audio or image part in Inputs routes to the model's own
// classification endpoint with the raw media bytes, and plain text with no
// labels routes to the model's own classification endpoint with a JSON
// body. Embeddings are a separate provider (role: embedding) — HF's
// feature-extraction API has its own wire shape and does not belong here.
package huggingface

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// DefaultBaseURL is the canonical HF Inference API endpoint. HF deprecated
// the older `api-inference.huggingface.co/models/{id}` URL in favor of the
// Inference Providers router; `hf-inference` is the provider that serves
// the same free-tier serverless models. Override via Config.BaseURL for HF
// Inference Endpoints (dedicated paid hosts) or to pin a different provider
// (e.g. replicate, fireworks).
const DefaultBaseURL = "https://router.huggingface.co/hf-inference"

// defaultContentType is the request Content-Type used when the caller
// didn't tag the media payload — HF accepts most media as raw bytes under
// application/octet-stream and routes by model / endpoint.
const defaultContentType = "application/octet-stream"

// defaultHTTPTimeout bounds a single HF Inference API request. HF model
// invocation occasionally takes tens of seconds on cold paths; 60s is a
// sensible upper bound that still surfaces hung requests.
const defaultHTTPTimeout = 60 * time.Second

// defaultLoadingRetryWait is used when HF returns a 503 without a
// parseable estimated_time. Short enough to keep tests fast; long enough
// that a cooperative server has time to warm up between attempts in
// practice.
const defaultLoadingRetryWait = 5 * time.Second

// maxLoadingWaitCap clamps HF's estimated_time so a misconfigured model
// doesn't trip a multi-minute sleep on the first call.
const maxLoadingWaitCap = 15 * time.Second

// errBodySnippetMax is the byte cap on response bodies included in error
// messages. Keeps logs readable when HF returns HTML on platform-level
// failures.
const errBodySnippetMax = 200

// modelLoadingMaxRetries bounds the wait for a cold HF model on first
// call. HF returns 503 with an estimated_time payload; we retry that many
// times then surface inference.ErrModelLoading. The total attempt count is
// 1 initial + N retries.
const modelLoadingMaxRetries = 3

// providerName identifies this codec to ProviderHTTPError regardless of
// which HF surface (router or dedicated endpoint) served the call.
const providerName = "huggingface"

// zeroShotSuffix is appended to the model URL when Request.Labels is set,
// routing the call to HF's zero-shot-classification pipeline instead of
// the model's own default pipeline.
const zeroShotSuffix = "/pipeline/zero-shot-classification"

// multiLabelParamKey is the Request.Params key that, when true, asks a
// non-zero-shot text classification call to return every label's score
// instead of only the top one.
const multiLabelParamKey = "multi_label"

// Config configures a Provider.
type Config struct {
	// APIKey is the HF token.
	APIKey string

	// BaseURL overrides DefaultBaseURL. Used for HF Inference Endpoints
	// (e.g. https://my-endpoint.xxx.endpoints.huggingface.cloud) and the
	// Inference Providers routing layer.
	BaseURL string

	// Dedicated, when true, treats BaseURL as a fully-specified inference
	// endpoint and skips the /models/{model_id} suffix that the public
	// Inference API requires. Set this when pointing at HF Inference
	// Endpoints (the paid dedicated host shape).
	Dedicated bool

	// HTTPClient lets the caller provide a custom transport (test
	// httptest server, timeouts, retry middleware). Default is a
	// 60s-timeout client.
	HTTPClient *http.Client
}

// Provider answers inference.Request calls against the HF Inference API.
type Provider struct {
	apiKey    string
	baseURL   string
	dedicated bool
	// model is the configured default model (from a provider spec),
	// used when a Request doesn't carry its own Model. Set directly by
	// register.go's factory, since Config deliberately doesn't carry a
	// per-provider default model — HF's classify backends always took
	// the model per call.
	model string
	http  *http.Client
}

var _ inference.Provider = (*Provider)(nil)

// New builds a Provider from cfg. APIKey is required.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("huggingface: api key is required")
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Provider{
		apiKey:    cfg.APIKey,
		baseURL:   strings.TrimRight(base, "/"),
		dedicated: cfg.Dedicated,
		http:      httpClient,
	}, nil
}

// Infer routes req to the right HF pipeline: a media part (audio or image)
// in Inputs goes to the model's classification endpoint as raw bytes;
// otherwise the last Inputs message's text goes to the model's own
// pipeline (Labels empty) or the zero-shot-classification pipeline
// (Labels set).
func (p *Provider) Infer(ctx context.Context, req inference.Request) (inference.Response, error) {
	if strings.TrimSpace(req.Prompt) != "" {
		return inference.Response{}, errors.New(
			"huggingface: does not accept a Prompt; HF classification pipelines take none")
	}

	model := req.Model
	if model == "" {
		model = p.model
	}

	if media, mimeType, ok := firstMediaPart(req.Inputs); ok {
		return p.inferMedia(ctx, model, media, mimeType)
	}

	text := lastMessageText(req.Inputs)
	if strings.TrimSpace(text) == "" {
		return inference.Response{}, errors.New("huggingface: empty text")
	}
	if len(req.Labels) > 0 {
		return p.inferZeroShot(ctx, model, text, req.Labels)
	}
	return p.inferText(ctx, model, text, req.Params)
}

// lastMessageText returns the last Inputs message's text, per GetContent().
// Empty when inputs is empty.
func lastMessageText(inputs []types.Message) string {
	if len(inputs) == 0 {
		return ""
	}
	last := &inputs[len(inputs)-1]
	return last.GetContent()
}

// firstMediaPart returns the first audio or image part found across
// inputs, scanning messages then parts in order, plus its MIME type
// (falling back to defaultContentType when untagged).
func firstMediaPart(inputs []types.Message) (media *types.MediaContent, mimeType string, ok bool) {
	for i := range inputs {
		for j := range inputs[i].Parts {
			part := &inputs[i].Parts[j]
			if part.Type != types.ContentTypeAudio && part.Type != types.ContentTypeImage {
				continue
			}
			if part.Media == nil {
				continue
			}
			mt := part.Media.MIMEType
			if mt == "" {
				mt = defaultContentType
			}
			return part.Media, mt, true
		}
	}
	return nil, "", false
}

// inferMedia posts raw media bytes to the model's classification endpoint.
func (p *Provider) inferMedia(
	ctx context.Context, model string, media *types.MediaContent, mimeType string,
) (inference.Response, error) {
	reader, err := media.ReadData()
	if err != nil {
		return inference.Response{}, fmt.Errorf("huggingface: read media: %w", err)
	}
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(reader)
	if err != nil {
		return inference.Response{}, fmt.Errorf("huggingface: read media bytes: %w", err)
	}
	if len(body) == 0 {
		return inference.Response{}, errors.New("huggingface: media part has no data")
	}

	endpoint, err := p.endpointURL(model, "")
	if err != nil {
		return inference.Response{}, err
	}
	respBody, err := p.do(ctx, endpoint, mimeType, body)
	if err != nil {
		return inference.Response{}, err
	}
	scores, err := decodeLabelScores(respBody)
	if err != nil {
		return inference.Response{}, err
	}
	return inference.Response{Model: model, Scores: scores, Raw: string(respBody)}, nil
}

// textRequest is the wire body for a non-zero-shot text classification
// call: HF's default pipeline for the configured model.
type textRequest struct {
	Inputs     string         `json:"inputs"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// inferText posts text to the model's own default pipeline. multi_label:
// true in params requests every label's score (return_all_scores) instead
// of only the top one.
func (p *Provider) inferText(
	ctx context.Context, model, text string, params map[string]any,
) (inference.Response, error) {
	endpoint, err := p.endpointURL(model, "")
	if err != nil {
		return inference.Response{}, err
	}

	reqBody := textRequest{Inputs: text}
	if multiLabel, _ := params[multiLabelParamKey].(bool); multiLabel {
		reqBody.Parameters = map[string]any{"return_all_scores": true}
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return inference.Response{}, fmt.Errorf("huggingface: encode text request: %w", err)
	}

	respBody, err := p.do(ctx, endpoint, "application/json", payload)
	if err != nil {
		return inference.Response{}, err
	}
	scores, err := decodeLabelScores(respBody)
	if err != nil {
		return inference.Response{}, err
	}
	return inference.Response{Model: model, Scores: scores, Raw: string(respBody)}, nil
}

// zeroShotRequest is the wire body for HF's zero-shot-classification
// pipeline.
type zeroShotRequest struct {
	Inputs     string         `json:"inputs"`
	Parameters map[string]any `json:"parameters"`
}

// zeroShotResponse is HF's zero-shot-classification response shape:
// parallel labels/scores arrays, typically already sorted highest first.
type zeroShotResponse struct {
	Sequence string    `json:"sequence"`
	Labels   []string  `json:"labels"`
	Scores   []float64 `json:"scores"`
}

// inferZeroShot posts text plus candidate labels to HF's
// zero-shot-classification pipeline and maps the parallel labels/scores
// arrays pairwise into LabelScore.
func (p *Provider) inferZeroShot(
	ctx context.Context, model, text string, labels []string,
) (inference.Response, error) {
	endpoint, err := p.endpointURL(model, zeroShotSuffix)
	if err != nil {
		return inference.Response{}, err
	}

	payload, err := json.Marshal(zeroShotRequest{
		Inputs:     text,
		Parameters: map[string]any{"candidate_labels": labels},
	})
	if err != nil {
		return inference.Response{}, fmt.Errorf("huggingface: encode zero-shot request: %w", err)
	}

	respBody, err := p.do(ctx, endpoint, "application/json", payload)
	if err != nil {
		return inference.Response{}, err
	}

	var decoded zeroShotResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return inference.Response{}, fmt.Errorf(
			"huggingface: decode zero-shot response: %w (body: %s)", err, truncateBody(respBody))
	}
	if len(decoded.Labels) != len(decoded.Scores) {
		return inference.Response{}, fmt.Errorf(
			"huggingface: zero-shot response labels/scores length mismatch (%d vs %d)",
			len(decoded.Labels), len(decoded.Scores))
	}

	scores := make([]inference.LabelScore, len(decoded.Labels))
	for i := range decoded.Labels {
		scores[i] = inference.LabelScore{Label: decoded.Labels[i], Score: decoded.Scores[i]}
	}
	sortScoresDescending(scores)
	return inference.Response{Model: model, Scores: scores, Raw: string(respBody)}, nil
}

// endpointURL builds the request URL for a given model id plus an
// optional pipeline suffix (e.g. zeroShotSuffix). The public Inference
// API routes via /models/{owner}/{name}[suffix]; HF Inference Endpoints
// (the dedicated host shape) bake the model into the endpoint itself and
// skip that prefix — callers opt into that with Config.Dedicated. A model
// id is required even when dedicated, since the caller's Request.Model /
// spec.Model contract doesn't change based on how the URL is built.
func (p *Provider) endpointURL(model, suffix string) (string, error) {
	if model == "" {
		return "", errors.New("huggingface: model id is required")
	}
	if p.dedicated {
		return p.baseURL + suffix, nil
	}
	// PathEscape would turn "owner/model" into "owner%2Fmodel"; HF expects
	// the slash to be a path separator. Escape segments individually so
	// legitimate slashes pass through but stray characters (e.g. spaces)
	// get encoded.
	segments := strings.Split(model, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return p.baseURL + "/models/" + strings.Join(segments, "/") + suffix, nil
}

// do performs an HTTP request to the HF Inference API with model-loading
// retry baked in. Returns the response body on success, an
// inference.ErrModelLoading on exhausted retries, an
// inference.ErrModelNotSupported when HF rejects the model for the
// configured inference path, or a *providers.ProviderHTTPError for any
// other non-200.
//
// Retry policy: 503 is the model-loading signal and is retried honoring
// HF's estimated_time (capped). This is HF-specific behavior distinct
// from providers.DoWithRetry's generic 429/502/503/504 retry: a 503 here
// means "warming up, try later with this exact wait", not "transient
// infra blip, back off and retry the same call". 502/504 are NOT
// retried — the HF Inference API surfaces real gateway errors with those
// codes too, and a tight retry loop risks compounding upstream load.
func (p *Provider) do(ctx context.Context, endpoint, contentType string, body []byte) ([]byte, error) {
	// Total attempts = 1 initial + modelLoadingMaxRetries.
	totalAttempts := 1 + modelLoadingMaxRetries
	for attempt := 0; attempt < totalAttempts; attempt++ {
		respBody, statusCode, err := p.sendOnce(ctx, endpoint, contentType, body)
		if err != nil {
			return nil, err
		}
		if statusCode == http.StatusOK {
			return respBody, nil
		}
		if statusCode != http.StatusServiceUnavailable {
			return nil, classifyHTTPError(endpoint, respBody, statusCode)
		}
		// Model is loading. HF returns JSON with estimated_time in
		// seconds; we honor a small chunk of that (capped) and retry.
		// On the last attempt, surface ErrModelLoading.
		if attempt == totalAttempts-1 {
			return nil, fmt.Errorf("%w: last 503 body: %s", inference.ErrModelLoading, truncateBody(respBody))
		}
		if waitErr := waitOrCancel(ctx, parseEstimatedWait(respBody, defaultLoadingRetryWait)); waitErr != nil {
			return nil, waitErr
		}
		// Loop to next attempt.
	}
	// Unreachable: every branch above either returns or continues the
	// loop. Keep an explicit return so future refactors that add a
	// fallthrough don't compile to nothing.
	return nil, errors.New("huggingface: retry loop exited without resolution (internal bug)")
}

// sendOnce performs a single HTTP round trip and returns the response body
// and status code. Retry/error-classification decisions are the caller's.
func (p *Provider) sendOnce(ctx context.Context, endpoint, contentType string, body []byte) ([]byte, int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("huggingface: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", contentType)

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("huggingface: send request: %w", err)
	}
	respBody, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, 0, fmt.Errorf("huggingface: read response: %w", readErr)
	}
	return respBody, resp.StatusCode, nil
}

// classifyHTTPError turns a non-200, non-503 response into either
// inference.ErrModelNotSupported (HF rejects the model for this inference
// path) or a *providers.ProviderHTTPError for anything else.
func classifyHTTPError(endpoint string, body []byte, statusCode int) error {
	if isUnsupportedModelResponse(body) {
		return fmt.Errorf("%w: %s", inference.ErrModelNotSupported, truncateBody(body))
	}
	return &providers.ProviderHTTPError{
		StatusCode: statusCode,
		URL:        endpoint,
		Body:       string(body),
		Provider:   providerName,
	}
}

// waitOrCancel blocks for d, or returns ctx.Err() if ctx is canceled first.
func waitOrCancel(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// parseEstimatedWait extracts {"estimated_time": <seconds>} from an HF
// 503 body, clamped to maxLoadingWaitCap. Falls back to fallback when the
// body isn't parseable.
func parseEstimatedWait(body []byte, fallback time.Duration) time.Duration {
	var parsed struct {
		EstimatedTime float64 `json:"estimated_time"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.EstimatedTime <= 0 {
		return fallback
	}
	wait := time.Duration(parsed.EstimatedTime * float64(time.Second))
	if wait > maxLoadingWaitCap {
		return maxLoadingWaitCap
	}
	return wait
}

// isUnsupportedModelResponse heuristically detects HF responses that mean
// "this model isn't routable on the configured inference path" rather
// than "this is a real backend error". The known shapes (as of 2026-05)
// are 4xx responses with a JSON body whose `error` field contains "not
// supported" — for retired-from-serverless models this is the canonical
// HF reply on the public router endpoint. Matched case-insensitively to
// absorb minor wording drift.
//
// Conservative on purpose: real backend errors (5xx, timeouts, auth
// failures) don't carry "not supported" in the body and continue to
// surface as a ProviderHTTPError so the user sees them. Two heuristics
// share the detection budget — the JSON-typed `error` field and a
// raw-text fallback for non-JSON bodies (HF occasionally returns
// plaintext on platform errors).
func isUnsupportedModelResponse(body []byte) bool {
	const marker = "not supported"
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error != "" {
		return strings.Contains(strings.ToLower(parsed.Error), marker)
	}
	return strings.Contains(strings.ToLower(string(body)), marker)
}

// truncateBody trims a response body for inclusion in an error message.
// HF errors are usually short JSON, but model output can occasionally
// come back HTML-wrapped on platform errors.
func truncateBody(body []byte) string {
	s := string(body)
	if len(s) > errBodySnippetMax {
		return s[:errBodySnippetMax] + "…"
	}
	return s
}

// labelScore is the wire shape HF uses for both its flat and nested
// label/score array responses.
type labelScore struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

// decodeLabelScores parses HF's label/score array response, used by the
// audio, image and non-zero-shot text paths. HF returns either
// `[[{label,score},...]]` (the nested/batch shape — one inner array per
// input; we always send one input so the outer array has length 1) or a
// flat `[{label,score},...]` (single input, no return_all_scores). The
// nested shape is tried first, falling back to flat. Results are sorted
// highest score first regardless of the order HF returned them in.
func decodeLabelScores(body []byte) ([]inference.LabelScore, error) {
	var nested [][]labelScore
	if err := json.Unmarshal(body, &nested); err == nil && len(nested) > 0 {
		return toLabelScores(nested[0]), nil
	}
	var flat []labelScore
	if err := json.Unmarshal(body, &flat); err != nil {
		return nil, fmt.Errorf("huggingface: decode label-score array: %w (body: %s)", err, truncateBody(body))
	}
	return toLabelScores(flat), nil
}

func toLabelScores(raw []labelScore) []inference.LabelScore {
	out := make([]inference.LabelScore, len(raw))
	for i, r := range raw {
		out[i] = inference.LabelScore{Label: r.Label, Score: r.Score}
	}
	sortScoresDescending(out)
	return out
}

func sortScoresDescending(scores []inference.LabelScore) {
	sort.Slice(scores, func(i, j int) bool { return scores[i].Score > scores[j].Score })
}
