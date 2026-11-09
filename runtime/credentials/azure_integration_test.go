package credentials

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAzureTokenCredential implements azcore.TokenCredential for testing.
type mockAzureTokenCredential struct {
	mu    sync.Mutex
	token azcore.AccessToken
	err   error
	calls int
	// numbered makes each fetch return a distinct token, suffixed with the
	// fetch's number, so a test can tell which fetch a request's header came
	// from.
	numbered bool
}

func (m *mockAzureTokenCredential) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	token := m.token
	if m.numbered {
		token.Token = fmt.Sprintf("%s-%d", token.Token, m.calls)
	}
	return token, m.err
}

func (m *mockAzureTokenCredential) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestAzureCredential_Apply(t *testing.T) {
	mock := &mockAzureTokenCredential{
		token: azcore.AccessToken{
			Token:     "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.test-token",
			ExpiresOn: time.Now().Add(1 * time.Hour),
		},
	}

	cred := &AzureCredential{
		endpoint: "https://my-resource.openai.azure.com",
		cred:     mock,
	}

	req, err := http.NewRequest("POST", "https://my-resource.openai.azure.com/openai/deployments/gpt-4/chat/completions", nil)
	require.NoError(t, err)

	err = cred.Apply(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, "Bearer eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.test-token", req.Header.Get("Authorization"))
}

func TestAzureCredential_Apply_TokenError(t *testing.T) {
	mock := &mockAzureTokenCredential{
		err: fmt.Errorf("managed identity unavailable"),
	}

	cred := &AzureCredential{
		endpoint: "https://my-resource.openai.azure.com",
		cred:     mock,
	}

	req, err := http.NewRequest("POST", "https://example.com", nil)
	require.NoError(t, err)

	err = cred.Apply(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get Azure token")
	assert.Contains(t, err.Error(), "managed identity unavailable")
}

func TestAzureCredential_Type(t *testing.T) {
	cred := &AzureCredential{}
	assert.Equal(t, "azure", cred.Type())
}

func TestAzureCredential_Endpoint(t *testing.T) {
	cred := &AzureCredential{
		endpoint: "https://eastus.api.cognitive.microsoft.com",
	}
	assert.Equal(t, "https://eastus.api.cognitive.microsoft.com", cred.Endpoint())
}

func TestAzureCredential_TokenCaching(t *testing.T) {
	mock := &mockAzureTokenCredential{
		token: azcore.AccessToken{
			Token:     "cached-azure-token",
			ExpiresOn: time.Now().Add(1 * time.Hour),
		},
	}

	cred := &AzureCredential{
		endpoint: "https://my-resource.openai.azure.com",
		cred:     mock,
	}

	// Call Apply multiple times
	for i := 0; i < 5; i++ {
		req, err := http.NewRequest("POST", "https://example.com", nil)
		require.NoError(t, err)

		err = cred.Apply(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, "Bearer cached-azure-token", req.Header.Get("Authorization"))
	}

	// Token source should only be called once — subsequent calls use the cache
	assert.Equal(t, 1, mock.getCalls())
}

func TestAzureCredential_TokenRefresh(t *testing.T) {
	// Start with a token that's within the refresh buffer (expires in 2 minutes)
	mock := &mockAzureTokenCredential{
		token: azcore.AccessToken{
			Token:     "first-azure-token",
			ExpiresOn: time.Now().Add(2 * time.Minute),
		},
	}

	cred := &AzureCredential{
		endpoint: "https://my-resource.openai.azure.com",
		cred:     mock,
	}

	// First call — token is within refresh buffer so won't be cached
	req1, err := http.NewRequest("POST", "https://example.com", nil)
	require.NoError(t, err)
	err = cred.Apply(context.Background(), req1)
	require.NoError(t, err)
	assert.Equal(t, "Bearer first-azure-token", req1.Header.Get("Authorization"))

	// Update mock to return a new token
	mock.mu.Lock()
	mock.token = azcore.AccessToken{
		Token:     "second-azure-token",
		ExpiresOn: time.Now().Add(2 * time.Minute),
	}
	mock.mu.Unlock()

	// Second call — since first token was within refresh buffer, it re-fetches
	req2, err := http.NewRequest("POST", "https://example.com", nil)
	require.NoError(t, err)
	err = cred.Apply(context.Background(), req2)
	require.NoError(t, err)
	assert.Equal(t, "Bearer second-azure-token", req2.Header.Get("Authorization"))

	// Token source should be called twice
	assert.Equal(t, 2, mock.getCalls())
}

func TestAzureOpenAIEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		deployment string
		expected   string
	}{
		{
			name:       "default api version",
			endpoint:   "https://my-resource.openai.azure.com",
			deployment: "gpt-4o",
			expected:   "https://my-resource.openai.azure.com/openai/deployments/gpt-4o",
		},
		{
			name:       "trailing slash stripped",
			endpoint:   "https://my-resource.openai.azure.com/",
			deployment: "gpt-4o",
			expected:   "https://my-resource.openai.azure.com/openai/deployments/gpt-4o",
		},
		{
			// A caller that already addressed a deployment must not get a
			// second deployment path stacked underneath the first.
			name:       "endpoint already names a deployment",
			endpoint:   "https://my-resource.openai.azure.com/openai/deployments/gpt-4o",
			deployment: "gpt-4o",
			expected:   "https://my-resource.openai.azure.com/openai/deployments/gpt-4o",
		},
		{
			// The deployment already in the endpoint wins: it is the more
			// specific statement of intent.
			name:       "existing deployment differs from the model",
			endpoint:   "https://my-resource.openai.azure.com/openai/deployments/gpt-4o/",
			deployment: "gpt-4-1-mini",
			expected:   "https://my-resource.openai.azure.com/openai/deployments/gpt-4o",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AzureOpenAIEndpoint(tt.endpoint, tt.deployment)
			if got != tt.expected {
				t.Errorf("AzureOpenAIEndpoint(%q, %q) = %q, want %q",
					tt.endpoint, tt.deployment, got, tt.expected)
			}
		})
	}
}

func TestDefaultAzureAPIVersion(t *testing.T) {
	if DefaultAzureAPIVersion == "" {
		t.Error("DefaultAzureAPIVersion must not be empty")
	}
}

// Concurrent requests against a cold cache share one token: every request
// carries the token from the first fetch, not one fetched for itself.
func TestAzureCredential_ConcurrentAccess(t *testing.T) {
	mock := &mockAzureTokenCredential{
		token: azcore.AccessToken{
			Token:     "concurrent-azure-token",
			ExpiresOn: time.Now().Add(1 * time.Hour),
		},
		numbered: true,
	}

	cred := &AzureCredential{
		endpoint: "https://my-resource.openai.azure.com",
		cred:     mock,
	}

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	headers := make([]string, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			req, err := http.NewRequest("POST", "https://example.com", nil)
			if err != nil {
				errs[idx] = err
				return
			}
			errs[idx] = cred.Apply(context.Background(), req)
			headers[idx] = req.Header.Get("Authorization")
		}(i)
	}
	wg.Wait()

	want := make([]string, goroutines)
	for i := range want {
		want[i] = "Bearer concurrent-azure-token-1"
	}
	assert.Equal(t, make([]error, goroutines), errs)
	assert.Equal(t, want, headers, "every request must reuse the first fetched token")
}
