package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// httpServerErrorThreshold is the HTTP status code threshold for server errors.
const httpServerErrorThreshold = 500

// classifyHTTPStatus maps an HTTP status code to a (retryable, cause) tuple
// shared across TTS provider error handlers. notFoundErr is the cause assigned
// when the server returns 404 (typically ErrInvalidVoice for TTS providers).
// badRequestErr is the cause assigned when the server returns 400 (nil if the
// caller wants no specific cause).
func classifyHTTPStatus(statusCode int, notFoundErr, badRequestErr error) (retryable bool, cause error) {
	retryable = statusCode == http.StatusTooManyRequests ||
		statusCode >= httpServerErrorThreshold

	switch statusCode {
	case http.StatusTooManyRequests:
		cause = ErrRateLimited
	case http.StatusUnauthorized:
		cause = fmt.Errorf("invalid API key")
	case http.StatusBadRequest:
		cause = badRequestErr
	case http.StatusNotFound:
		cause = notFoundErr
	}
	return retryable, cause
}

// decodeErrorBody attempts to decode the response body into target. On decode
// failure it returns a generic SynthesisError tagged with providerName and the
// status code; the caller should return it directly. On success it returns nil
// so the caller can build a provider-specific error from the decoded fields.
func decodeErrorBody(providerName string, resp *http.Response, target any) error {
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return NewSynthesisError(
			providerName,
			fmt.Sprintf("%d", resp.StatusCode),
			"unknown error",
			err,
			resp.StatusCode >= httpServerErrorThreshold,
		)
	}
	return nil
}

// postJSONForAudio marshals body as JSON, POSTs it to url with the given
// headers, and returns the response body for streaming audio download. Non-200
// responses are routed through errorHandler. providerName is used to tag
// transport-level errors. This consolidates the request-build/dispatch boiler
// shared by ElevenLabs and Cartesia REST TTS calls.
func postJSONForAudio(
	ctx context.Context,
	client *http.Client,
	providerName, url string,
	body any,
	headers map[string]string,
	errorHandler func(*http.Response) error,
) (io.ReadCloser, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, NewSynthesisError(providerName, "", "request failed", err, true)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, errorHandler(resp)
	}
	return resp.Body, nil
}
