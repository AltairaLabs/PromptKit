// Multimodal HTTP response handling.
//
// This file detects binary (non-JSON) HTTP responses based on Content-Type
// and converts them into types.ContentPart entries suitable for LLM consumption.
// It supports image, audio, and video MIME types with base64 encoding.

package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Common MIME type prefixes for binary content detection.
var defaultMultimodalPrefixes = []string{
	"image/",
	"audio/",
	"video/",
}

// IsBinaryContentType returns true if the Content-Type indicates a binary
// response that should be handled as multimodal content rather than JSON.
// If acceptTypes is non-empty, only those specific types match.
// Otherwise, common image/audio/video prefixes are checked.
func IsBinaryContentType(contentType string, acceptTypes []string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	// Strip parameters (e.g. charset)
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}

	if len(acceptTypes) > 0 {
		for _, at := range acceptTypes {
			if strings.EqualFold(ct, at) {
				return true
			}
		}
		return false
	}

	for _, prefix := range defaultMultimodalPrefixes {
		if strings.HasPrefix(ct, prefix) {
			return true
		}
	}
	return false
}

// ContentTypeToMediaType maps an HTTP Content-Type to a types.ContentPart Type string.
func ContentTypeToMediaType(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}

	switch {
	case strings.HasPrefix(ct, "image/"):
		return "image"
	case strings.HasPrefix(ct, "audio/"):
		return "audio"
	case strings.HasPrefix(ct, "video/"):
		return "video"
	default:
		return "document"
	}
}

// ReadMultimodalResponse reads a binary HTTP response and returns it as
// a ContentPart with base64-encoded data. It enforces per-response and
// aggregate size limits.
func ReadMultimodalResponse(
	resp *http.Response,
	aggregateSize *atomic.Int64,
	maxAggregateSize int64,
) (json.RawMessage, []types.ContentPart, error) {
	// Check aggregate limit before reading.
	if maxAggregateSize > 0 && aggregateSize.Load() >= maxAggregateSize {
		return nil, nil, fmt.Errorf("%w: %d bytes consumed, limit is %d",
			ErrAggregateResponseSizeExceeded, aggregateSize.Load(), maxAggregateSize)
	}

	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read binary response: %w", err)
	}

	// Track cumulative size.
	newTotal := aggregateSize.Add(int64(len(data)))
	if maxAggregateSize > 0 && newTotal > maxAggregateSize {
		return nil, nil, fmt.Errorf("%w: %d bytes consumed, limit is %d",
			ErrAggregateResponseSizeExceeded, newTotal, maxAggregateSize)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("HTTP request returned status %d: %s",
			resp.StatusCode, string(data))
	}

	contentType := resp.Header.Get("Content-Type")
	mediaType := ContentTypeToMediaType(contentType)
	encoded := base64.StdEncoding.EncodeToString(data)

	// Strip parameters from content type for the media content field.
	mimeType := contentType
	if idx := strings.IndexByte(mimeType, ';'); idx >= 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}

	part := types.ContentPart{
		Type: mediaType,
		Media: &types.MediaContent{
			MIMEType: mimeType,
			Data:     &encoded,
		},
	}

	// JSON result is a summary object — the actual binary data is in Parts.
	summary := map[string]any{
		"type":         mediaType,
		"content_type": mimeType,
		"size_bytes":   len(data),
	}
	jsonResult, _ := json.Marshal(summary)

	return jsonResult, []types.ContentPart{part}, nil
}
