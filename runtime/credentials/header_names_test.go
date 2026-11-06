package credentials

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// HeaderNames is a promise about Apply: every header Apply sets is named, so a
// provider that checks custom headers by name, without applying the
// credential, refuses every header the credential would replace.
func TestHeaderNames_CoverEveryHeaderApplySets(t *testing.T) {
	creds := map[string]Credential{
		"api key bearer": NewAPIKeyCredential("k"),
		"api key custom": NewAPIKeyCredential("k", WithHeaderName("X-API-Key"), WithPrefix("")),
		"aws sigv4 with session token": &AWSCredential{region: "us-east-1", cfg: aws.Config{
			Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "AKID", SecretAccessKey: "s", SessionToken: "t"}, nil
			}),
		}},
		"gcp": &GCPCredential{tokenSource: &mockTokenSource{
			token: &oauth2.Token{AccessToken: "ya29", Expiry: time.Now().Add(time.Hour)},
		}},
		"azure": &AzureCredential{cred: &mockAzureTokenCredential{
			token: azcore.AccessToken{Token: "tok", ExpiresOn: time.Now().Add(time.Hour)},
		}},
	}
	for name, cred := range creds {
		t.Run(name, func(t *testing.T) {
			namer, ok := cred.(HeaderNamer)
			require.True(t, ok, "%T does not name its headers", cred)
			named := http.Header{}
			for _, n := range namer.HeaderNames() {
				named.Set(n, "x")
			}

			req, err := http.NewRequest(http.MethodPost, "https://example.com/v1", strings.NewReader(`{}`))
			require.NoError(t, err)
			require.NoError(t, cred.Apply(context.Background(), req))
			require.NotEmpty(t, req.Header, "Apply set no headers")
			for key := range req.Header {
				assert.NotEmpty(t, named.Get(key), "Apply sets %s, which HeaderNames omits", key)
			}
		})
	}
}

// Credentials that set nothing name nothing.
func TestHeaderNames_EmptyWhenNothingIsSet(t *testing.T) {
	assert.Empty(t, NewAPIKeyCredential("").HeaderNames())
	assert.Empty(t, (&NoOpCredential{}).HeaderNames())
}
