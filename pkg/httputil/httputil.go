// Package httputil re-exports the runtime/httputil package for backward
// compatibility. New code should import runtime/httputil directly.
package httputil

import (
	"net/http"
	"time"

	runtimehttp "github.com/AltairaLabs/PromptKit/runtime/httputil"
)

// Re-exported constants.
const (
	DefaultProviderTimeout  = runtimehttp.DefaultProviderTimeout
	DefaultToolTimeout      = runtimehttp.DefaultToolTimeout
	DefaultStreamingTimeout = runtimehttp.DefaultStreamingTimeout
)

// NewHTTPClient returns an *http.Client configured with the given timeout.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return runtimehttp.NewHTTPClient(timeout)
}
