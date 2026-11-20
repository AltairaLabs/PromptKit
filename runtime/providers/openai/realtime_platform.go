package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/internal/streaming"
)

// azureRealtimePath is Azure's GA realtime route. It takes the deployment as
// ?model= and rejects an api-version, unlike the deployment-scoped REST paths.
const azureRealtimePath = "/openai/v1/realtime"

// realtimeDial is where a realtime session connects and how its handshake is
// authenticated. A nil dialHeaders means the static API key authenticates it.
type realtimeDial struct {
	endpoint    string
	dialHeaders func(ctx context.Context) (http.Header, error)
}

// realtimeDial resolves the session's endpoint and handshake auth from the
// provider's platform. Every platform either resolves here or is rejected:
// the alternative, dialing api.openai.com with a platform provider's empty
// static key, fails late with a misleading invalid_api_key (#2677).
func (p *Provider) realtimeDial() (realtimeDial, error) {
	switch p.platform {
	case "":
		return realtimeDial{endpoint: RealtimeAPIEndpoint}, nil
	case azurePlatform:
		return p.azureRealtimeDial()
	default:
		return realtimeDial{}, fmt.Errorf(
			"OpenAI realtime is not available on platform %q (supported: azure)", p.platform)
	}
}

// azureRealtimeDial targets the resource's GA realtime route and authenticates
// each handshake with the platform credential, fetched at dial time so an
// expiring Entra token is never reused from when the provider was built.
//
// The host is the provider's base URL, which is the resource's deployment URL
// unless a gateway (base_url) fronts it, so the session goes where chat does.
func (p *Provider) azureRealtimeDial() (realtimeDial, error) {
	resource := p.baseURL
	if resource == "" && p.platformConfig != nil {
		resource = p.platformConfig.Endpoint
	}
	if resource == "" {
		return realtimeDial{}, fmt.Errorf("OpenAI realtime on azure needs the platform endpoint")
	}
	if p.credential == nil {
		return realtimeDial{}, fmt.Errorf("OpenAI realtime on azure needs a platform credential")
	}
	endpoint, err := azureRealtimeEndpoint(resource)
	if err != nil {
		return realtimeDial{}, err
	}
	return realtimeDial{endpoint: endpoint, dialHeaders: streaming.CredentialDialHeaders(p.credential, endpoint)}, nil
}

// azureRealtimeEndpoint maps a resource endpoint (https://<res>.openai.azure.com,
// optionally with a deployment path) to its WebSocket realtime route. A
// Foundry project endpoint keeps its project path, so the session is
// authorized by the project, as chat is.
func azureRealtimeEndpoint(resource string) (string, error) {
	u, err := url.Parse(resource)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("OpenAI realtime on azure: invalid endpoint %q", resource)
	}
	scheme := "wss"
	if u.Scheme == "http" || u.Scheme == "ws" {
		scheme = "ws"
	}
	path := azureRealtimePath
	if project, ok := credentials.AzureFoundryProject(resource); ok {
		if pu, perr := url.Parse(project); perr == nil {
			path = pu.Path + azureRealtimePath
		}
	}
	return (&url.URL{Scheme: scheme, Host: u.Host, Path: path}).String(), nil
}
