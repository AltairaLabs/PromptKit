package a2a

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// Default retry constants for A2A executor.
const (
	DefaultA2AMaxRetries   = 3
	DefaultA2AInitialDelay = 500 * time.Millisecond
	DefaultA2AMaxDelay     = 30 * time.Second

	a2aExponentialBase   = 2
	a2aMaxJitterFraction = 0.5
	a2aFloat64MantBits   = 53
	a2aUint64Bits        = 64
)

// RetryPolicy configures retry behavior for the A2A executor.
type RetryPolicy struct {
	MaxRetries   int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// DefaultRetryPolicy returns the default retry policy for A2A calls.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:   DefaultA2AMaxRetries,
		InitialDelay: DefaultA2AInitialDelay,
		MaxDelay:     DefaultA2AMaxDelay,
	}
}

// ExecutorOption configures an [Executor].
type ExecutorOption func(*Executor)

// WithRetryPolicy sets the retry policy for the A2A executor.
func WithRetryPolicy(policy RetryPolicy) ExecutorOption {
	return func(e *Executor) { e.retryPolicy = policy }
}

// WithNoRetry disables retry for the A2A executor.
func WithNoRetry() ExecutorOption {
	return func(e *Executor) { e.retryPolicy = RetryPolicy{MaxRetries: 0} }
}

// Executor implements tools.Executor for A2A agent tools.
// It dispatches tool calls to remote A2A agents via the A2A client.
type Executor struct {
	mu          sync.RWMutex
	clients     map[string]*Client
	retryPolicy RetryPolicy
}

// NewExecutor creates a new A2A executor with optional configuration.
func NewExecutor(opts ...ExecutorOption) *Executor {
	e := &Executor{
		retryPolicy: DefaultRetryPolicy(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Name returns "a2a" to match the Mode on A2A tool descriptors.
func (e *Executor) Name() string { return "a2a" }

// Execute calls a remote A2A agent with the tool arguments and returns the response.
func (e *Executor) Execute(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	if descriptor.A2AConfig == nil {
		return nil, fmt.Errorf("a2a executor: tool %q has no A2AConfig", descriptor.Name)
	}

	cfg := descriptor.A2AConfig
	client := e.getOrCreateClient(cfg.AgentURL)

	// Parse arguments
	var input struct {
		Query     string `json:"query"`
		ImageURL  string `json:"image_url,omitempty"`
		ImageData string `json:"image_data,omitempty"`
		AudioData string `json:"audio_data,omitempty"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("a2a executor: parse args: %w", err)
	}

	// Build message parts
	text := input.Query
	parts := []Part{{Text: &text}}

	if input.ImageURL != "" {
		parts = append(parts, Part{URL: &input.ImageURL, MediaType: "image/*"})
	}
	if input.ImageData != "" {
		parts = append(parts, Part{Raw: []byte(input.ImageData), MediaType: "image/*"})
	}
	if input.AudioData != "" {
		parts = append(parts, Part{Raw: []byte(input.AudioData), MediaType: "audio/*"})
	}

	// Build metadata with skillId for mock server routing.
	var metadata map[string]any
	if cfg.SkillID != "" {
		metadata = map[string]any{"skillId": cfg.SkillID}
	}

	req := &SendMessageRequest{
		Message: Message{
			Role:     RoleUser,
			Parts:    parts,
			Metadata: metadata,
		},
	}

	// Apply timeout on top of the caller's context
	if cfg.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	task, err := e.sendWithRetry(ctx, client, req, cfg.AgentURL)
	if err != nil {
		return nil, fmt.Errorf("a2a executor: send message: %w", err)
	}

	responseText := ExtractResponseText(task)
	result := map[string]string{"response": responseText}
	return json.Marshal(result)
}

// sendWithRetry wraps client.SendMessage with exponential backoff retry logic.
func (e *Executor) sendWithRetry(
	ctx context.Context, client *Client, req *SendMessageRequest, agentURL string,
) (*Task, error) {
	maxAttempts := e.retryPolicy.MaxRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		task, err := client.SendMessage(ctx, req)
		if err == nil {
			return task, nil
		}

		if !isA2ARetryableError(err) {
			return nil, err
		}

		lastErr = err

		// Don't sleep after the last attempt.
		if attempt >= maxAttempts-1 {
			break
		}

		delay := a2aCalculateBackoff(e.retryPolicy, attempt)
		logger.Warn("retrying a2a request",
			"agent_url", agentURL,
			"attempt", attempt+1,
			"max_retries", e.retryPolicy.MaxRetries,
			"delay", delay.String(),
			"error", err,
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, lastErr
}

// isA2ARetryableError determines whether an error from an A2A call
// should be retried. It retries on transient network errors and
// HTTP 429/502/503/504. It does NOT retry on context cancellation,
// other 4xx errors, or JSON-RPC application errors.
func isA2ARetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Never retry context cancellation or deadline exceeded.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check for HTTP status errors from the A2A client.
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		return isA2ARetryableStatusCode(httpErr.StatusCode)
	}

	// Check for JSON-RPC errors — these are application-level and not retryable.
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return false
	}

	// Check for transient network errors (connection refused, reset, timeout).
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// isA2ARetryableStatusCode returns true for HTTP status codes that
// indicate a transient error worth retrying: 429, 502, 503, 504.
func isA2ARetryableStatusCode(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// a2aCalculateBackoff computes the delay for a given retry attempt
// using exponential backoff with jitter, capped at MaxDelay.
func a2aCalculateBackoff(policy RetryPolicy, attempt int) time.Duration {
	initialDelay := policy.InitialDelay
	if initialDelay <= 0 {
		initialDelay = DefaultA2AInitialDelay
	}

	maxDelay := policy.MaxDelay
	if maxDelay <= 0 {
		maxDelay = DefaultA2AMaxDelay
	}

	multiplier := math.Pow(a2aExponentialBase, float64(attempt))
	delay := time.Duration(float64(initialDelay) * multiplier)

	if delay > maxDelay {
		delay = maxDelay
	}

	// Add jitter: uniform random in [0, delay * maxJitterFraction].
	jitter := time.Duration(a2aCryptoRandFloat64() * a2aMaxJitterFraction * float64(delay))
	delay += jitter

	return delay
}

// a2aCryptoRandFloat64 returns a cryptographically secure random float64
// in [0.0, 1.0) using crypto/rand.
func a2aCryptoRandFloat64() float64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0
	}
	const maxMantissa = 1 << a2aFloat64MantBits
	bitsToDiscard := a2aUint64Bits - a2aFloat64MantBits
	n := binary.BigEndian.Uint64(buf[:]) >> bitsToDiscard
	return float64(n) / float64(maxMantissa)
}

// getOrCreateClient returns a cached client or creates a new one.
func (e *Executor) getOrCreateClient(agentURL string) *Client {
	e.mu.RLock()
	if c, ok := e.clients[agentURL]; ok {
		e.mu.RUnlock()
		return c
	}
	e.mu.RUnlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	// Double-check after acquiring write lock
	if c, ok := e.clients[agentURL]; ok {
		return c
	}
	if e.clients == nil {
		e.clients = make(map[string]*Client)
	}
	c := NewClient(agentURL)
	e.clients[agentURL] = c
	return c
}

// ExtractResponseText extracts text from a completed A2A task.
// It checks the status message first, then artifacts.
func ExtractResponseText(task *Task) string {
	if task.Status.Message != nil {
		for _, part := range task.Status.Message.Parts {
			if part.Text != nil {
				return *part.Text
			}
		}
	}

	var texts []string
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			if part.Text != nil {
				texts = append(texts, *part.Text)
			}
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n")
	}

	return ""
}
