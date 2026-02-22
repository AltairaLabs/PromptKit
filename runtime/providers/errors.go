package providers

import (
	"encoding/json"
	"fmt"
)

// ParsePlatformHTTPError extracts a human-readable error from platform-specific
// HTTP error responses (Bedrock, Vertex, Azure). These platforms return JSON
// like {"message":"..."} on HTTP 4xx/5xx. Falls back to raw body if parsing fails.
// When platform is empty, returns a generic error with the raw body.
func ParsePlatformHTTPError(platform string, statusCode int, body []byte) error {
	if platform == "" {
		return fmt.Errorf("API error (HTTP %d): %s", statusCode, string(body))
	}

	var errResp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Message != "" {
		return fmt.Errorf("%s error (HTTP %d): %s", platform, statusCode, errResp.Message)
	}
	return fmt.Errorf("%s error (HTTP %d): %s", platform, statusCode, string(body))
}
