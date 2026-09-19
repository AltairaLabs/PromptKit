package topiccontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
)

// DefaultModel is the TopicControl model id. NIM accepts it as the `model`
// field on the OpenAI-compatible endpoint it serves.
const DefaultModel = "nvidia/llama-3.1-nemoguard-8b-topic-control"

// defaultHTTPTimeout bounds one classification. This runs in the request path
// ahead of the agent's own call, so it must fail fast rather than hang a turn.
//
// It is a default rather than a constant ceiling because it is short enough to
// be wrong for some deployments: a NIM answering its first request from cold,
// or a shared endpoint under load, can exceed it. A timeout here is an error,
// and on_error denies, so a too-short timeout does not leak traffic — it blocks
// the conversation. Raise it with Config.Timeout (additional_config.timeout_seconds).
const defaultHTTPTimeout = 20 * time.Second

// maxLabelTokens caps the completion. The answer is one of two short labels;
// anything longer is a model that ignored its instruction.
const maxLabelTokens = 16

// errBodySnippetMax bounds how much of an error body reaches a message.
const errBodySnippetMax = 200

// extraMessages accounts for the system instruction plus the final user
// message wrapped around the replayed history.
const extraMessages = 2

// The two labels the model is instructed to emit.
const (
	labelOnTopic  = "on-topic"
	labelOffTopic = "off-topic"
)

// Config configures a Client.
type Config struct {
	// BaseURL is the OpenAI-compatible root of the NIM deployment, e.g.
	// http://topic-control:8000/v1. Required — there is no public default
	// endpoint for a self-hosted NIM, and guessing one would turn a
	// misconfiguration into a confusing timeout.
	BaseURL string
	// APIKey is optional: a locally deployed NIM usually needs none, while
	// NVIDIA-hosted endpoints do.
	APIKey string
	// Model overrides DefaultModel.
	Model string
	// Timeout bounds a single classification call. Zero means
	// defaultHTTPTimeout. Note this caps the call regardless of the deadline
	// on the context passed to ClassifyTopic: whichever is shorter wins, so a
	// caller cannot extend it by supplying a longer context.
	Timeout time.Duration
	// HTTPClient overrides the default client entirely, Timeout included.
	// Mainly for tests.
	HTTPClient *http.Client
}

// Client implements classify.TopicClassifier against a TopicControl NIM.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// Compile-time check: this backend's whole purpose is to satisfy this one task.
var _ classify.TopicClassifier = (*Client)(nil)

// New builds a Client.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("topiccontrol: base_url is required (the NIM's OpenAI-compatible root)")
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultHTTPTimeout
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   model,
		http:    httpClient,
	}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// ClassifyTopic renders the policy as the system instruction, replays history
// for reference resolution, and puts the message under judgment last.
func (c *Client) ClassifyTopic(
	ctx context.Context, req classify.TopicRequest,
) (classify.TopicResult, error) {
	msgs := make([]chatMessage, 0, len(req.History)+extraMessages)
	msgs = append(msgs, chatMessage{Role: "system", Content: RenderPolicy(req.Policy)})
	for _, turn := range req.History {
		if strings.TrimSpace(turn.Text) == "" {
			continue
		}
		msgs = append(msgs, chatMessage{Role: turn.Role, Content: turn.Text})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: req.Message})

	body, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    msgs,
		Temperature: 0,
		MaxTokens:   maxLabelTokens,
	})
	if err != nil {
		return classify.TopicResult{}, fmt.Errorf("topiccontrol: encode request: %w", err)
	}

	raw, err := c.post(ctx, body)
	if err != nil {
		return classify.TopicResult{}, err
	}

	return classify.TopicResult{Decision: ParseLabel(raw), Raw: raw}, nil
}

func (c *Client) post(ctx context.Context, body []byte) (string, error) {
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("topiccontrol: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("topiccontrol: call classifier: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("topiccontrol: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("topiccontrol: classifier returned %d: %s",
			resp.StatusCode, snippet(payload))
	}

	var decoded chatResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("topiccontrol: decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", errors.New("topiccontrol: classifier returned no choices")
	}
	return decoded.Choices[0].Message.Content, nil
}

// ParseLabel maps the model's answer onto a decision. Anything that is not one
// of the two labels is unknown — never silently allow, because "the classifier
// said something unexpected" and "the message is in scope" are different facts
// and the policy decides what to do about each.
func ParseLabel(raw string) classify.TopicDecision {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.Trim(normalized, `"'.`)
	switch normalized {
	case labelOnTopic:
		return classify.TopicAllow
	case labelOffTopic:
		return classify.TopicDeny
	default:
		return classify.TopicUnknown
	}
}

func snippet(b []byte) string {
	if len(b) > errBodySnippetMax {
		return string(b[:errBodySnippetMax]) + "…"
	}
	return string(b)
}

// HTTPTimeout reports the per-call timeout this client will apply. Exported for
// tests: the timeout is only observable otherwise by waiting for it to expire.
func (c *Client) HTTPTimeout() time.Duration { return c.http.Timeout }
