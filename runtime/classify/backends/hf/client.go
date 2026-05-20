// Package hf implements the runtime/classify interfaces against the
// HuggingFace Inference API.
//
// One Client backs every task interface — audio, text, image, video,
// embedder — because they all share authentication, retry, and rate
// limiting. Each interface method routes to the right HF endpoint
// for the configured model.
package hf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

// DefaultBaseURL is the canonical HF Inference API endpoint. HF deprecated
// the older `api-inference.huggingface.co/models/{id}` URL in favor of the
// Inference Providers router; `hf-inference` is the provider that serves
// the same free-tier serverless models. Override via Config.BaseURL for HF
// Inference Endpoints (dedicated paid hosts) or to pin a different provider
// (e.g. replicate, fireworks).
const DefaultBaseURL = "https://router.huggingface.co/hf-inference"

// defaultContentType is the request Content-Type used when the caller
// didn't tag the payload — HF accepts most media as raw bytes under
// application/octet-stream and routes by Model / endpoint.
const defaultContentType = "application/octet-stream"

// defaultHTTPTimeout bounds a single HF Inference API request. HF
// model invocation occasionally takes tens of seconds on cold paths;
// 60s is a sensible upper bound that still surfaces hung requests.
const defaultHTTPTimeout = 60 * time.Second

// defaultLoadingRetryWait is used when HF returns a 503 without a
// parseable estimated_time. Short enough to keep tests fast; long
// enough that a cooperative server has time to warm up between
// attempts in practice.
const defaultLoadingRetryWait = 5 * time.Second

// maxLoadingWaitCap clamps HF's estimated_time so a misconfigured
// model doesn't trip a multi-minute sleep on the first call.
const maxLoadingWaitCap = 15 * time.Second

// errBodySnippetMax is the byte cap on response bodies included in
// error messages. Keeps logs readable when HF returns HTML on
// platform-level failures.
const errBodySnippetMax = 200

// modelLoadingMaxRetries bounds the wait for a cold HF model on
// first call. HF returns 503 with an estimated_time payload; we
// retry that many times then surface "model warming up" so the
// caller can treat it as a skipped eval rather than a real failure.
// The total attempt count is 1 initial + N retries.
const modelLoadingMaxRetries = 3

// ErrModelLoading is returned when HF reports the model is still
// warming up after exhausting retries. Eval handlers that see this
// should produce a skipped result, not a failed one — the model
// isn't broken, it just hasn't initialized yet.
var ErrModelLoading = errors.New("hf: model still loading after retries")

// Config holds construction parameters for Client.
type Config struct {
	// APIKey is the HF token. Falls back to HF_TOKEN /
	// HUGGING_FACE_HUB_TOKEN env vars in the factory wrapper; bare
	// Client construction requires the caller to pass it.
	APIKey string

	// BaseURL overrides DefaultBaseURL. Used for HF Inference
	// Endpoints (e.g. https://my-endpoint.xxx.endpoints.huggingface.cloud)
	// and the Inference Providers routing layer.
	BaseURL string

	// Dedicated, when true, treats BaseURL as a fully-specified
	// inference endpoint and skips the /models/{model_id} suffix
	// that the public Inference API requires. Set this when pointing
	// at HF Inference Endpoints (the paid dedicated host shape).
	// Default false matches the public Inference API.
	Dedicated bool

	// HTTPClient lets the caller provide a custom transport (test
	// httptest server, timeouts, retry middleware). Default
	// is a 60s-timeout client.
	HTTPClient *http.Client
}

// Client is a HuggingFace Inference API client. It satisfies every
// task interface in runtime/classify; methods that the configured
// model doesn't support fail at call time with an HF error, not at
// construction.
type Client struct {
	apiKey     string
	baseURL    string
	dedicated  bool
	httpClient *http.Client
}

// Compile-time check: Client must satisfy every classify task
// interface so the HF backend can be dropped into any registry slot.
var (
	_ classify.AudioClassifier = (*Client)(nil)
	_ classify.TextClassifier  = (*Client)(nil)
	_ classify.ImageClassifier = (*Client)(nil)
	_ classify.Embedder        = (*Client)(nil)
)

// NewClient constructs a Client. APIKey is required.
func NewClient(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("hf: api key is required")
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Client{
		apiKey:     cfg.APIKey,
		baseURL:    strings.TrimRight(base, "/"),
		dedicated:  cfg.Dedicated,
		httpClient: httpClient,
	}, nil
}

// modelURL builds the request URL for a given model id. The public
// Inference API routes via /models/{owner}/{name}; HF Inference
// Endpoints (the dedicated host shape) bake the model into the
// endpoint itself and skip the suffix — callers opt into that with
// Config.Dedicated. Detection by hostname was tried first but trips
// in httptest setups where the URL is neither shape; explicit flag is
// the simplest unambiguous answer.
func (c *Client) modelURL(model string) (string, error) {
	if model == "" {
		return "", fmt.Errorf("hf: model id is required")
	}
	if c.dedicated {
		return c.baseURL, nil
	}
	// PathEscape would turn "owner/model" into "owner%2Fmodel"; HF
	// expects the slash to be a path separator. Escape segments
	// individually so legitimate slashes pass through but stray
	// characters (e.g. spaces) get encoded.
	segments := strings.Split(model, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return c.baseURL + "/models/" + strings.Join(segments, "/"), nil
}

// do performs an HTTP request to the HF Inference API with model-
// loading retry baked in. Returns the response body on success, an
// ErrModelLoading on exhausted retries, or the wrapped HTTP error.
//
// Caller owns request encoding (audio bytes, JSON, etc.) — `do`
// only handles auth + retry + error decoding.
//
// Retry policy: 503 is the model-loading signal and is retried
// honoring HF's estimated_time (capped). 502/504 are NOT retried —
// the HF Inference API surfaces real gateway errors with those
// codes too, and a tight retry loop risks compounding upstream
// load. If you want bounded gateway retries, add them at a higher
// level via Config.HTTPClient transport middleware.
//
// TODO(#1214): no per-client rate limiting yet. HF free tier limits
// per IP; the proposal calls for a token bucket sized from arena
// config. Add when the first concurrent eval handler lands.
func (c *Client) do(ctx context.Context, modelURL, contentType string, body []byte) ([]byte, error) {
	// Total attempts = 1 initial + modelLoadingMaxRetries.
	totalAttempts := 1 + modelLoadingMaxRetries
	for attempt := 0; attempt < totalAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, modelURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("hf: build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", contentType)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("hf: send request: %w", err)
		}
		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("hf: read response: %w", readErr)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			return respBody, nil
		case http.StatusServiceUnavailable:
			// Model is loading. HF returns JSON with estimated_time
			// in seconds; we honor a small chunk of that (capped)
			// and retry. On the last attempt return ErrModelLoading
			// so the caller marks the eval skipped, not failed.
			if attempt == totalAttempts-1 {
				return nil, fmt.Errorf("%w: last 503 body: %s", ErrModelLoading, truncateBody(respBody))
			}
			wait := parseEstimatedWait(respBody, defaultLoadingRetryWait)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			// Loop to next attempt.
		default:
			return nil, fmt.Errorf("hf: status %d: %s", resp.StatusCode, truncateBody(respBody))
		}
	}
	// Unreachable: every branch in the switch above either returns
	// or continues the loop. Keep an explicit return so future
	// refactors that add a fallthrough don't compile to nothing.
	return nil, fmt.Errorf("hf: retry loop exited without resolution (internal bug)")
}

// parseEstimatedWait extracts {"estimated_time": <seconds>} from an
// HF 503 body, clamped to maxLoadingWaitCap. Falls back to fallback
// when the body isn't parseable.
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

// truncateBody trims a response body for inclusion in an error
// message. HF errors are usually short JSON, but model output can
// occasionally come back HTML-wrapped on platform errors. The cap
// comes from errBodySnippetMax — keeping it a package constant means
// every error message uses the same budget without each call site
// duplicating the literal.
func truncateBody(body []byte) string {
	s := string(body)
	if len(s) > errBodySnippetMax {
		return s[:errBodySnippetMax] + "…"
	}
	return s
}
