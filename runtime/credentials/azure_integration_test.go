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
}

func (m *mockAzureTokenCredential) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.token, m.err
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

func TestAzureCredential_ConcurrentAccess(t *testing.T) {
	mock := &mockAzureTokenCredential{
		token: azcore.AccessToken{
			Token:     "concurrent-azure-token",
			ExpiresOn: time.Now().Add(1 * time.Hour),
		},
	}

	cred := &AzureCredential{
		endpoint: "https://my-resource.openai.azure.com",
		cred:     mock,
	}

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

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
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d failed", i)
	}

	// Despite concurrent access, token source should only be called once
	assert.Equal(t, 1, mock.getCalls())
}
