package systemone

import (
	"fmt"
	"net"
	"net/url"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// providerType is the `type` string a host writes in its *.provider.yaml
// alongside `role: inference`.
const providerType = "systemone"

//nolint:gochecknoinits // Factory registration requires init.
func init() {
	inference.RegisterFactory(providerType, func(spec inference.ProviderSpec) (inference.Provider, error) {
		apiKey := base.APIKeyFromCredential(spec.Credential)
		if apiKey == "" && !isLoopbackBaseURL(spec.BaseURL) {
			return nil, fmt.Errorf(
				"inference: %s: credential is required unless base_url is loopback (self-hosted simple-jev)",
				providerType)
		}
		return New(Config{
			APIKey:  apiKey,
			BaseURL: spec.BaseURL,
			Model:   spec.Model,
		})
	})
}

// isLoopbackBaseURL reports whether rawURL's host is 127.0.0.1, localhost or
// ::1 — the only hosts systemone.New is allowed to accept without a
// credential (self-hosted simple-jev stays keyless; every other host,
// including the Vercel AI Gateway and TypeSafe's hosted Jev, must present
// one).
func isLoopbackBaseURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
