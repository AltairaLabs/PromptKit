package streaming

import (
	"context"
	"fmt"
	"net/http"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
)

// CredentialDialHeaders returns a ConnConfig.DialHeaders func that applies
// cred to a scratch request for target and hands back the headers it set. A
// platform token is fetched at each dial, so a reconnect never reuses one
// that has expired.
func CredentialDialHeaders(cred credentials.Credential, target string) func(context.Context) (http.Header, error) {
	return func(ctx context.Context) (http.Header, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
		if err != nil {
			return nil, err
		}
		if err := cred.Apply(ctx, req); err != nil {
			return nil, fmt.Errorf("applying %s credential: %w", cred.Type(), err)
		}
		return req.Header, nil
	}
}
