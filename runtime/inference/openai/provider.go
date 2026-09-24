// Package openai implements inference.Provider over any OpenAI-compatible
// chat-completions endpoint that returns token logprobs — OpenAI itself,
// NVIDIA's hosted NemoGuard models, vLLM, LiteLLM and most gateways.
//
// Each Infer call becomes one single-token completion: the candidate labels
// are the allowed answer set, the model is asked for exactly one of them,
// and the response is the softmax of the labels' logprobs, renormalized over
// the allowed set — read from the endpoint's top_logprobs rather than a
// dedicated logit-scoring API. Like any such approach the numbers are
// uncalibrated.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

const (
	// defaultBaseURL is OpenAI's API root, used when Config.BaseURL is empty.
	defaultBaseURL = "https://api.openai.com/v1"
	// NemoGuardModel is NVIDIA's hosted topic-control chat model, served
	// through an OpenAI-compatible chat-completions endpoint.
	NemoGuardModel = "nvidia/llama-3.1-nemoguard-8b-topic-control"

	defaultHTTPTimeout = 30 * time.Second
	// maxTopLogprobs is OpenAI's ceiling for top_logprobs.
	maxTopLogprobs = 20
	// minLabels/maxLabels bound Request.Labels: at least two to
	// discriminate between, and at most ten to stay inside the
	// top_logprobs window above — a label outside it would silently read
	// as probability zero.
	minLabels = 2
	maxLabels = 10

	// providerName identifies this codec to DoWithRetry's logging and to
	// ProviderHTTPError, regardless of which factory alias built it — both
	// factories call the same vendor API shape.
	providerName = "openai-inference"

	chatCompletionsPath = "/chat/completions"
)

// Config configures a Provider.
type Config struct {
	BaseURL    string
	APIKey     string
	Model      string // the chat model that answers; overridable per-Request
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Provider answers inference.Request calls using a chat model's logprobs
// over a single-token completion.
type Provider struct {
	baseURL, apiKey, model string
	http                   *http.Client
}

var _ inference.Provider = (*Provider)(nil)

// New builds a Provider from cfg.
func New(cfg Config) (*Provider, error) {
	base := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = defaultBaseURL
	}
	hc := cfg.HTTPClient
	if hc == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultHTTPTimeout
		}
		hc = &http.Client{Timeout: timeout}
	}
	return &Provider{baseURL: base, apiKey: cfg.APIKey, model: cfg.Model, http: hc}, nil
}

// HTTPTimeout reports the per-call timeout this Provider will apply.
// Exported for tests: the timeout is only observable otherwise by waiting
// for it to expire.
func (p *Provider) HTTPTimeout() time.Duration { return p.http.Timeout }

// Infer asks the configured model to answer with exactly one of req.Labels,
// and returns the renormalized label distribution read from the
// completion's top_logprobs.
func (p *Provider) Infer(ctx context.Context, req inference.Request) (inference.Response, error) {
	if len(req.Labels) < minLabels {
		return inference.Response{}, fmt.Errorf("%w: got %d, need at least %d",
			inference.ErrLabelsRequired, len(req.Labels), minLabels)
	}
	if len(req.Labels) > maxLabels {
		return inference.Response{}, fmt.Errorf(
			"inference: at most %d labels are supported (top_logprobs window), got %d", maxLabels, len(req.Labels))
	}

	model := req.Model
	if model == "" {
		model = p.model
	}

	top, usage, raw, err := p.complete(ctx, model, req)
	if err != nil {
		return inference.Response{}, err
	}

	scores, err := distribution(top, req.Labels)
	if err != nil {
		return inference.Response{}, err
	}

	return inference.Response{Model: model, Scores: scores, Usage: usage, Raw: raw}, nil
}

// firstTokenPrefix returns the text before the first '-', ' ' or '_' in
// label, trimmed and lowercased. A multi-token label like "on-topic"
// tokenizes as "on" + "-topic", so this is what a single returned token is
// compared against.
func firstTokenPrefix(label string) string {
	s := label
	if i := strings.IndexAny(s, "-_ "); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// distribution renormalizes the labels' probabilities over the returned
// top_logprobs, matching each label by its first-token prefix. It errors
// when two labels share a prefix (matching could not tell them apart) or
// when no label's prefix appears at all — reading that as a uniform guess
// would invent an answer.
func distribution(top map[string]float64, labels []string) ([]inference.LabelScore, error) {
	prefixes := make([]string, len(labels))
	byPrefix := make(map[string][]string, len(labels))
	for i, label := range labels {
		prefixes[i] = firstTokenPrefix(label)
		byPrefix[prefixes[i]] = append(byPrefix[prefixes[i]], label)
	}
	for prefix, sharing := range byPrefix {
		if len(sharing) > 1 {
			return nil, fmt.Errorf(
				"inference: labels %s share the first-token prefix %q; first-token matching cannot tell them apart",
				strings.Join(sharing, ", "), prefix)
		}
	}

	probs := make([]float64, len(labels))
	var total float64
	for tok, lp := range top {
		norm := strings.ToLower(strings.TrimSpace(tok))
		for i, prefix := range prefixes {
			if norm == prefix {
				p := math.Exp(lp)
				probs[i] += p
				total += p
			}
		}
	}
	if total == 0 {
		return nil, fmt.Errorf("inference: none of the labels %v appeared in the model's top logprobs", labels)
	}

	scores := make([]inference.LabelScore, len(labels))
	for i, label := range labels {
		scores[i] = inference.LabelScore{Label: label, Score: probs[i] / total}
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].Score > scores[j].Score })
	return scores, nil
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	Logprobs    bool          `json:"logprobs"`
	TopLogprobs int           `json:"top_logprobs"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Logprobs struct {
			Content []struct {
				Token       string `json:"token"`
				TopLogprobs []struct {
					Token   string  `json:"token"`
					Logprob float64 `json:"logprob"`
				} `json:"top_logprobs"`
			} `json:"content"`
		} `json:"logprobs"`
	} `json:"choices"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
	} `json:"usage"`
}

// extraMessages is the count of chat turns buildMessages adds beyond
// req.Inputs: the system prompt and the final label-instruction turn.
const extraMessages = 2

// buildMessages assembles the chat turns: an optional system prompt, the
// input messages verbatim, then a final instruction naming the allowed
// labels.
func buildMessages(req inference.Request) []chatMessage {
	msgs := make([]chatMessage, 0, len(req.Inputs)+extraMessages)
	if req.Prompt != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: req.Prompt})
	}
	for i := range req.Inputs {
		m := &req.Inputs[i]
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.GetContent()})
	}
	msgs = append(msgs, chatMessage{
		Role:    "user",
		Content: "Answer with exactly one of: " + strings.Join(req.Labels, ", "),
	})
	return msgs
}

// complete sends the single-token completion request, retrying transient
// failures, and returns the raw top_logprobs token->logprob map from the
// first (only) sampled position.
func (p *Provider) complete(
	ctx context.Context, model string, req inference.Request,
) (map[string]float64, inference.Usage, string, error) {
	body, err := json.Marshal(chatRequest{
		Model:       model,
		Messages:    buildMessages(req),
		MaxTokens:   1,
		Temperature: 0,
		Logprobs:    true,
		TopLogprobs: maxTopLogprobs,
	})
	if err != nil {
		return nil, inference.Usage{}, "", fmt.Errorf("inference: encode request: %w", err)
	}
	url := p.baseURL + chatCompletionsPath

	doFn := func() (*http.Response, error) {
		httpReq, buildErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if buildErr != nil {
			return nil, fmt.Errorf("inference: build request: %w", buildErr)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if p.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		return p.http.Do(httpReq)
	}

	resp, err := providers.DoWithRetry(ctx, providers.DefaultRetryPolicy(), providerName, doFn)
	if err != nil {
		return nil, inference.Usage{}, "", fmt.Errorf("inference: call model: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, inference.Usage{}, "", fmt.Errorf("inference: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, inference.Usage{}, "", &providers.ProviderHTTPError{
			StatusCode: resp.StatusCode,
			URL:        url,
			Body:       string(payload),
			Provider:   providerName,
		}
	}

	var decoded chatResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, inference.Usage{}, "", fmt.Errorf("inference: decode response: %w", err)
	}
	if len(decoded.Choices) == 0 || len(decoded.Choices[0].Logprobs.Content) == 0 {
		return nil, inference.Usage{}, "",
			errors.New("inference: response carried no logprobs; the endpoint must support logprobs")
	}

	sampled := decoded.Choices[0].Logprobs.Content[0]
	top := make(map[string]float64, len(sampled.TopLogprobs))
	for _, t := range sampled.TopLogprobs {
		top[t.Token] = t.Logprob
	}
	usage := inference.Usage{InputTokens: decoded.Usage.PromptTokens}
	return top, usage, sampled.Token, nil
}
