package providers

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// StreamRetryResult holds the successfully opened streaming response after
// the pre-first-chunk retry loop. The caller takes ownership of Body (which
// is a composite reader re-prepending the peeked first SSE event), and
// must close it.
type StreamRetryResult struct {
	// Response is the HTTP response of the successful attempt. Body has
	// already been wrapped; callers must not read directly from
	// Response.Body. Use Body instead.
	Response *http.Response
	// Body is a composite reader that first replays the SSE event bytes
	// consumed by the peek, then streams the remainder of the response.
	// Closing this closes the underlying Response.Body.
	Body io.ReadCloser
	// Attempts is the total number of attempts made (1 on first-try success).
	Attempts int
}

// StreamRetryRequest bundles the dependencies for a streaming retry
// attempt. This exists so OpenStreamWithRetryRequest can grow new
// parameters (budget, host label, etc.) without breaking every call site.
type StreamRetryRequest struct {
	Policy       StreamRetryPolicy
	Budget       *RetryBudget // nil means unbounded retries
	ProviderName string
	Host         string // metric label; may be empty
	IdleTimeout  time.Duration
	RequestFn    func(ctx context.Context) (*http.Request, error)
	Client       *http.Client
}

// OpenStreamWithRetry executes requestFn and peeks the first SSE data event
// on the response body. If Do() returns a retryable error, or the response
// status is retryable, or the body fails to produce a first SSE event
// within the idle window, the attempt is discarded and retried up to the
// policy's MaxAttempts. On success, the buffered bytes are replayed into
// the returned Body so downstream SSE parsers see a contiguous stream.
//
// This function only retries in the pre-first-chunk window — that is, only
// while no content bytes have been surfaced to the caller. It never reads
// past the end of the first SSE event payload.
//
// When policy.Enabled is false this is equivalent to a single Do() + peek:
// on success the body is still wrapped to replay the peeked bytes, but no
// retry is performed.
//
// Thin wrapper over OpenStreamWithRetryRequest for callers that don't need
// the budget or host-label parameters.
func OpenStreamWithRetry(
	ctx context.Context,
	policy StreamRetryPolicy,
	providerName string,
	idleTimeout time.Duration,
	requestFn func(ctx context.Context) (*http.Request, error),
	client *http.Client,
) (*StreamRetryResult, error) {
	return OpenStreamWithRetryRequest(ctx, &StreamRetryRequest{
		Policy:       policy,
		ProviderName: providerName,
		IdleTimeout:  idleTimeout,
		RequestFn:    requestFn,
		Client:       client,
	})
}

// OpenStreamWithRetryRequest is the full-featured form of OpenStreamWithRetry
// that accepts a budget and host label. Retries beyond the initial attempt
// must acquire a token from req.Budget (if non-nil) before re-dialing; an
// empty budget causes the function to return the last error immediately
// (fail-fast) rather than waiting for token refill.
//
//nolint:gocognit // Retry loop with classification, backoff, budget, and metric emission
func OpenStreamWithRetryRequest(ctx context.Context, req *StreamRetryRequest) (*StreamRetryResult, error) {
	maxAttempts := req.Policy.Attempts()
	metrics := DefaultStreamMetrics()
	start := time.Now()
	var lastErr error

	// Publish initial budget state so operators see the metric even when
	// no retries have happened yet. For nil budgets this is a no-op.
	metrics.ObserveRetryBudgetAvailable(req.ProviderName, req.Host, req.Budget)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		httpReq, err := req.RequestFn(ctx)
		if err != nil {
			// Request construction errors are never retried — they
			// indicate a caller bug, not a transient failure.
			return nil, err
		}

		resp, doErr := req.Client.Do(httpReq)
		result, retry, classifyErr := classifyStreamAttempt(resp, doErr, req.IdleTimeout)
		if result != nil {
			result.Attempts = attempt + 1
			metrics.ObserveFirstChunkLatency(req.ProviderName, time.Since(start))
			if attempt > 0 {
				metrics.RetryAttempt(req.ProviderName, "success")
			}
			return result, nil
		}

		lastErr = classifyErr
		if !retry || attempt >= maxAttempts-1 {
			if attempt >= maxAttempts-1 && retry {
				metrics.RetryAttempt(req.ProviderName, "exhausted")
			}
			break
		}

		// Before we commit to a retry, take a token from the budget.
		// Empty budget = fail fast. This is the load-bearing line of
		// Phase 2: when an upstream connection reset kills 100 streams
		// at once, the budget ensures only ~burst of them re-dial and
		// the rest fail fast with the original error, preserving the
		// upstream's remaining capacity for the retries that win the
		// race.
		if !req.Budget.TryAcquire() {
			metrics.RetryAttempt(req.ProviderName, "budget_exhausted")
			metrics.ObserveRetryBudgetAvailable(req.ProviderName, req.Host, req.Budget)
			logger.Warn("streaming retry budget exhausted, failing fast",
				"provider", req.ProviderName,
				"host", req.Host,
				"error", classifyErr,
			)
			break
		}
		metrics.ObserveRetryBudgetAvailable(req.ProviderName, req.Host, req.Budget)

		metrics.RetryAttempt(req.ProviderName, "failed")
		delay := req.Policy.BackoffFor(attempt)
		logger.Warn("retrying streaming request (pre-first-chunk)",
			"provider", req.ProviderName,
			"attempt", attempt+1,
			"max_attempts", maxAttempts,
			"delay", delay.String(),
			"error", classifyErr,
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, lastErr
}

// classifyStreamAttempt inspects a (*http.Response, error) pair and decides
// whether the attempt was a success (return non-nil result), a retryable
// failure (return retry=true), or a terminal failure.
//
// On success, the response body is wrapped so the peeked first SSE event
// is replayed to downstream consumers. On any non-terminal failure the
// body is closed before returning so connections are not leaked.
func classifyStreamAttempt(
	resp *http.Response,
	doErr error,
	idleTimeout time.Duration,
) (result *StreamRetryResult, retry bool, err error) {
	if doErr != nil {
		return nil, IsRetryableStreamError(doErr), doErr
	}

	// Non-200 responses: close the body and decide based on status.
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		body := ReadErrorBody(resp.Body)
		httpErr := fmt.Errorf(
			"API request failed with status %d: %s",
			resp.StatusCode, string(body),
		)
		return nil, IsRetryableStreamStatus(resp.StatusCode), httpErr
	}

	// Peek the first SSE data event. If this fails we treat it as a
	// retryable mid-stream error because by definition no content has
	// been surfaced to the caller.
	buffered, peekErr := peekFirstSSEEvent(resp.Body, idleTimeout)
	if peekErr != nil {
		_ = resp.Body.Close()
		return nil, IsRetryableStreamError(peekErr), peekErr
	}

	wrapped := &replayReadCloser{
		replay: bytes.NewReader(buffered),
		rest:   resp.Body,
	}
	return &StreamRetryResult{Response: resp, Body: wrapped}, false, nil
}

// peekFirstSSEEvent reads from r until it has seen at least one complete
// SSE data event terminated by a blank line, then returns the bytes read.
// The returned slice contains exactly what was consumed from r, suitable
// for replay via a composite reader.
//
// The read is bounded by idleTimeout using an IdleTimeoutReader, so a
// stalled connection does not block this function indefinitely. A zero
// idleTimeout disables the idle guard (inherit caller deadline only).
//
// This function does not interpret the SSE payload — it only looks for
// the structural framing ("data: ..." line followed by a blank line) that
// all SSE producers emit. This is deliberately more forgiving than
// bufio.Scanner to handle provider-specific keepalives and comments.
func peekFirstSSEEvent(body io.Reader, idleTimeout time.Duration) ([]byte, error) {
	reader := body
	if idleTimeout > 0 {
		reader = NewIdleTimeoutReader(io.NopCloser(body), idleTimeout)
	}
	br := bufio.NewReader(reader)
	var buf bytes.Buffer
	sawData := false
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			buf.WriteString(line)
			if done, drainErr := processSSELine(br, &buf, line, &sawData); done {
				return buf.Bytes(), drainErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && buf.Len() > 0 && sawData {
				// Stream closed cleanly right after the first event
				// with no trailing blank line. Treat as a complete
				// peek so downstream can decide what to do.
				return buf.Bytes(), nil
			}
			return nil, err
		}
	}
}

// processSSELine classifies a single SSE framing line and, when the end of
// the first event is reached, drains any lookahead the bufio.Reader has
// pre-buffered so downstream reads resume contiguously. Returns done=true
// exactly when the first event boundary has been observed.
func processSSELine(br *bufio.Reader, buf *bytes.Buffer, line string, sawData *bool) (done bool, err error) {
	trimmed := strings.TrimRight(line, "\r\n")
	if strings.HasPrefix(trimmed, "data: ") || trimmed == "data:" {
		*sawData = true
		return false, nil
	}
	if trimmed == "" && *sawData {
		// Blank line after a data: line terminates the event.
		// bufio.Reader may have already read ahead past our boundary;
		// drain those bytes into the replay slice so downstream
		// readers see a contiguous stream.
		if drainErr := drainBufferedInto(br, buf); drainErr != nil {
			return true, drainErr
		}
		return true, nil
	}
	return false, nil
}

// drainBufferedInto copies any bytes sitting in the bufio.Reader's internal
// buffer (but not yet surfaced to callers) into dst. This is needed because
// bufio.Reader reads ahead from the underlying source, so bytes that arrived
// after our peek boundary would otherwise be silently lost when we stop
// using the bufio.Reader and read from the underlying source directly.
func drainBufferedInto(br *bufio.Reader, dst *bytes.Buffer) error {
	n := br.Buffered()
	if n == 0 {
		return nil
	}
	remaining, peekErr := br.Peek(n)
	if peekErr != nil {
		return peekErr
	}
	dst.Write(remaining)
	// Discard the bytes from the bufio reader so the underlying source
	// is logically at the same position as the end of our buffer.
	if _, discardErr := br.Discard(n); discardErr != nil {
		return discardErr
	}
	return nil
}

// replayReadCloser concatenates a bytes.Reader holding already-consumed
// bytes with the remainder of an underlying ReadCloser. It exists instead
// of io.MultiReader+io.NopCloser so Close() still reaches the underlying
// response body.
type replayReadCloser struct {
	replay *bytes.Reader
	rest   io.ReadCloser
}

// Read drains the replay buffer first, then falls through to the
// underlying ReadCloser. Once the replay is empty it is never re-read.
func (r *replayReadCloser) Read(p []byte) (int, error) {
	if r.replay.Len() > 0 {
		return r.replay.Read(p)
	}
	return r.rest.Read(p)
}

// Close closes the underlying ReadCloser. The replay bytes.Reader has no
// resources to release.
func (r *replayReadCloser) Close() error {
	return r.rest.Close()
}
