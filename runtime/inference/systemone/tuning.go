package systemone

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"

// ApplyHTTPTuning replaces the provider's HTTP client with a tuned copy,
// implementing base.HTTPTunable so a provider file's headers,
// request_timeout and http_transport reach this backend.
func (p *Provider) ApplyHTTPTuning(t base.HTTPTuning) error {
	p.http = t.Client(p.http)
	return nil
}
