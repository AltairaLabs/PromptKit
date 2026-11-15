package base

import (
	"fmt"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
)

// PlatformAzure is the PlatformConfig.Type of an Azure-hosted provider.
const PlatformAzure = "azure"

// CredentialTransport applies a credential (and, for Azure, the api-version
// query parameter) to every request before delegating to Base. It is how a
// platform-hosted provider authenticates: the credential is applied per
// request, so a token that expires is refreshed by the credential itself.
//
// It implements layeredTransport, so HTTPTuning replaces or wraps only Base
// and the credential stays the outermost layer.
type CredentialTransport struct {
	Base       http.RoundTripper
	Cred       credentials.Credential
	APIVersion string // set only for Azure
}

// BaseTransport returns the transport the credential-applying layer delegates to.
func (rt *CredentialTransport) BaseTransport() http.RoundTripper { return rt.Base }

// WithBaseTransport returns a copy of rt that delegates to next.
func (rt *CredentialTransport) WithBaseTransport(next http.RoundTripper) http.RoundTripper {
	out := *rt
	out.Base = next
	return &out
}

// RoundTrip applies the api-version query param (Azure) and the credential
// to a cloned request before delegating to the base transport.
func (rt *CredentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone before mutating: the RoundTripper contract forbids modifying
	// the caller's request.
	r := req.Clone(req.Context())
	if rt.APIVersion != "" && r.URL.Query().Get("api-version") == "" {
		q := r.URL.Query()
		q.Set("api-version", rt.APIVersion)
		r.URL.RawQuery = q.Encode()
	}
	// A redirect to another host never gets the credential: the client's
	// CredentialRedirectPolicy strips what http.Client copied, and the
	// credential is not applied again here.
	if rt.Cred != nil && SameOriginAsFirstHop(req) {
		if err := rt.Cred.Apply(r.Context(), r); err != nil {
			return nil, err
		}
	}
	next := rt.Base
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(r)
}

// AzureAPIVersion returns the platform's additional_config.api_version, or
// credentials.DefaultAzureAPIVersion when it sets none.
func AzureAPIVersion(pc *credentials.PlatformConfig) string {
	if pc != nil {
		if v, ok := pc.AdditionalConfig["api_version"].(string); ok && v != "" {
			return v
		}
	}
	return credentials.DefaultAzureAPIVersion
}

// AzureHTTPClient returns a client that authenticates every request with cred
// and adds the platform's api-version, for a service hosted on Azure.
func AzureHTTPClient(cred credentials.Credential, pc *credentials.PlatformConfig, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     &CredentialTransport{Base: http.DefaultTransport, Cred: cred, APIVersion: AzureAPIVersion(pc)},
		CheckRedirect: CredentialRedirectPolicy(nil, nil),
	}
}

// AzureOpenAIServiceOptions hosts an OpenAI-wire HTTP service (TTS, STT) on
// the Azure resource spec.Platform names. The deployment is spec.Model; the
// base URL is its deployment path unless spec.BaseURL overrides it, and the
// client applies the platform credential and api-version to every request.
func AzureOpenAIServiceOptions(spec *CapabilitySpec, timeout time.Duration) ([]HTTPServiceOption, error) {
	if spec.Platform == nil || spec.Platform.Endpoint == "" {
		return nil, fmt.Errorf("%s %q on azure needs platform.endpoint", spec.Type, spec.ID)
	}
	if spec.Model == "" {
		return nil, fmt.Errorf("%s %q on azure needs model: the deployment name", spec.Type, spec.ID)
	}
	if spec.Credential == nil {
		return nil, fmt.Errorf("%s %q on azure needs a platform credential", spec.Type, spec.ID)
	}
	baseURL := spec.BaseURL
	if baseURL == "" {
		baseURL = credentials.AzureOpenAIEndpoint(spec.Platform.Endpoint, spec.Model)
	}
	return []HTTPServiceOption{
		WithClient(AzureHTTPClient(spec.Credential, spec.Platform, timeout)),
		WithBaseURL(baseURL),
	}, nil
}
