// Package systemone implements inference.Provider over the "typed decision"
// wire protocol: POST {base}/systemone with a shared state and a named typed
// question, answered with a probability distribution rather than generated
// text.
//
// One Provider covers every server that speaks it: TypeSafe's hosted Jev
// (directly, or fronted by the Vercel AI Gateway), and self-hosted
// logit-scoring servers such as simple-jev, which serve the same shape over
// any open model. Only the "choice" question type is implemented — the
// generic inference.Request/Response shape has no use for the noul or score
// question types the wire protocol also supports.
package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// DefaultModel is TypeSafe's rolling Jev model alias, used when Config.Model
// is empty.
const DefaultModel = "jev-latest"

const (
	endpointPath       = "/systemone"
	defaultHTTPTimeout = 20 * time.Second
	errBodySnippetMax  = 200
	// minLabels is the smallest label set a choice question can have: one
	// option is not a decision.
	minLabels = 2
	// choiceQuestionID is the fixed id of the single choice question sent
	// in every request; the caller-chosen ids classify.Question exposes on
	// the POC's Decider have no analog here since inference.Request only
	// ever asks one question.
	choiceQuestionID = "q"
	// choiceQuestionType is the wire value of Request.Questions[id].Type.
	choiceQuestionType = "choice"
	// providerName identifies this codec to providers.DoWithRetry's
	// logging and to ProviderHTTPError.
	providerName = "systemone"
)

// Config configures a Provider.
type Config struct {
	// BaseURL is the systemone server root (e.g.
	// https://api.typesafe.ai/v1, or a Vercel AI Gateway / self-hosted
	// simple-jev root). Required.
	BaseURL string
	// APIKey authenticates via "Authorization: Bearer <key>". May be empty
	// for a keyless self-hosted server (see register.go's loopback rule).
	APIKey string
	// Model overrides DefaultModel.
	Model string
	// Timeout bounds a single HTTP call. Ignored when HTTPClient is set.
	Timeout time.Duration
	// HTTPClient lets the caller provide a custom transport (test
	// httptest server, timeouts). Default is a Timeout-bounded client.
	HTTPClient *http.Client
}

// Provider answers inference.Request calls against the typed-decision wire
// protocol.
type Provider struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
	// retryPolicy governs post's 429/502/503/504/network retry via
	// providers.DoWithRetry. Defaults to providers.DefaultRetryPolicy() in
	// New(); same-package tests may override it with a faster policy so
	// exhausted-retry cases don't pay DefaultRetryPolicy's real backoff.
	retryPolicy pipeline.RetryPolicy
}

var _ inference.Provider = (*Provider)(nil)

// New builds a Provider from cfg. BaseURL is required.
func New(cfg Config) (*Provider, error) {
	base := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("systemone: base_url is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultModel
	}
	hc := cfg.HTTPClient
	if hc == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultHTTPTimeout
		}
		hc = &http.Client{Timeout: timeout}
	}
	return &Provider{
		baseURL:     base,
		apiKey:      cfg.APIKey,
		model:       model,
		http:        hc,
		retryPolicy: providers.DefaultRetryPolicy(),
	}, nil
}

// wireState is one turn of Request.Inputs, sent as the shared state a
// systemone server evaluates the question against.
type wireState struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// wireCriteria writes a choice question's candidate labels as a JSON object
// with each label mapped to null, in slice order. The order carries
// meaning (backends assign labels by position and break exact ties toward
// the earlier option), so it is written by hand rather than through a Go
// map, which does not preserve insertion order.
type wireCriteria []string

// MarshalJSON writes the labels as a JSON object {label: null, ...} in
// slice order.
func (c wireCriteria) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, label := range c {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(label)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteString(":null")
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type wireQuestion struct {
	Type         string       `json:"type"`
	Instructions string       `json:"instructions"`
	Criteria     wireCriteria `json:"criteria,omitempty"`
}

type wireRequest struct {
	Model     string                  `json:"model"`
	State     []wireState             `json:"state"`
	Questions map[string]wireQuestion `json:"questions"`
}

type wireAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type wireGatewayMetadata struct {
	Cost       string `json:"cost"`
	MarketCost string `json:"marketCost"`
}

type wireResponse struct {
	Model   string                `json:"model"`
	Answers map[string]wireAnswer `json:"answers"`
	Usage   struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
	ProviderMetadata struct {
		Gateway wireGatewayMetadata `json:"gateway"`
	} `json:"provider_metadata"`
}

// Infer asks a single choice question — Request.Labels as the candidate
// answers, Request.Prompt as the instructions, Request.Inputs as the shared
// state — and returns the winning answer's probability distribution as
// Scores, highest first.
func (p *Provider) Infer(ctx context.Context, req inference.Request) (inference.Response, error) {
	if len(req.Labels) < minLabels {
		return inference.Response{}, fmt.Errorf("%w: got %d, need at least %d",
			inference.ErrLabelsRequired, len(req.Labels), minLabels)
	}

	model := req.Model
	if model == "" {
		model = p.model
	}

	body, err := json.Marshal(wireRequest{
		Model: model,
		State: wireStateFromInputs(req.Inputs),
		Questions: map[string]wireQuestion{
			choiceQuestionID: {
				Type:         choiceQuestionType,
				Instructions: req.Prompt,
				Criteria:     wireCriteria(req.Labels),
			},
		},
	})
	if err != nil {
		return inference.Response{}, fmt.Errorf("systemone: encode request: %w", err)
	}

	payload, err := p.post(ctx, body)
	if err != nil {
		return inference.Response{}, err
	}

	var decoded wireResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return inference.Response{}, fmt.Errorf("systemone: decode response: %w (body: %s)", err, truncateBody(payload))
	}

	answer, ok := decoded.Answers[choiceQuestionID]
	if !ok {
		return inference.Response{}, fmt.Errorf("systemone: no answer for question %q", choiceQuestionID)
	}
	if answer.Type != choiceQuestionType {
		return inference.Response{}, fmt.Errorf(
			"systemone: question %q asked for %s, answered %s", choiceQuestionID, choiceQuestionType, answer.Type)
	}
	if !containsLabel(req.Labels, answer.Choice) {
		return inference.Response{}, fmt.Errorf(
			"systemone: choice answer picked %q, not one of the requested labels %v", answer.Choice, req.Labels)
	}

	scores := make([]inference.LabelScore, 0, len(answer.Probabilities))
	for label, score := range answer.Probabilities {
		scores = append(scores, inference.LabelScore{Label: label, Score: score})
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].Score > scores[j].Score })

	usage := inference.Usage{
		InputTokens: decoded.Usage.InputTokens,
		Cost:        parseGatewayCost(decoded.ProviderMetadata.Gateway),
	}
	return inference.Response{Model: decoded.Model, Scores: scores, Usage: usage, Raw: string(payload)}, nil
}

// wireStateFromInputs renders req.Inputs as the wire state array, reading
// each message's text via GetContent so multimodal / tool messages degrade
// to their text content the same way every other inference provider does.
func wireStateFromInputs(inputs []types.Message) []wireState {
	state := make([]wireState, len(inputs))
	for i := range inputs {
		state[i] = wireState{Role: inputs[i].Role, Content: inputs[i].GetContent()}
	}
	return state
}

func containsLabel(labels []string, choice string) bool {
	for _, l := range labels {
		if l == choice {
			return true
		}
	}
	return false
}

// parseGatewayCost prefers Cost, falling back to MarketCost; a missing or
// unparseable value leaves the result 0 rather than erroring — cost is
// diagnostic, not required for a correct decision.
func parseGatewayCost(gw wireGatewayMetadata) float64 {
	if v, err := strconv.ParseFloat(gw.Cost, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseFloat(gw.MarketCost, 64); err == nil {
		return v
	}
	return 0
}

// post sends body to the systemone endpoint, retrying transient failures
// via providers.DoWithRetry under p.retryPolicy, and returns the response
// body on a 200. Any other outcome — retries exhausted on a retryable
// status, or an immediate non-200/non-retryable status — becomes a
// *providers.ProviderHTTPError.
func (p *Provider) post(ctx context.Context, body []byte) ([]byte, error) {
	url := p.baseURL + endpointPath
	doFn := func() (*http.Response, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("systemone: build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if p.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		return p.http.Do(httpReq)
	}

	resp, err := providers.DoWithRetry(ctx, p.retryPolicy, providerName, doFn)
	if err != nil {
		// A *providers.RetryableHTTPError means DoWithRetry exhausted its
		// own retries on a 429/502/503/504 — the response body was
		// already closed inside DoWithRetry's classification, so it
		// can't be recovered here.
		var retryErr *providers.RetryableHTTPError
		if errors.As(err, &retryErr) {
			return nil, &providers.ProviderHTTPError{StatusCode: retryErr.StatusCode, URL: url, Provider: providerName}
		}
		return nil, fmt.Errorf("systemone: call decider: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("systemone: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &providers.ProviderHTTPError{
			StatusCode: resp.StatusCode, URL: url, Body: string(payload), Provider: providerName,
		}
	}
	return payload, nil
}

func truncateBody(body []byte) string {
	s := string(body)
	if len(s) > errBodySnippetMax {
		return s[:errBodySnippetMax] + "…"
	}
	return s
}
