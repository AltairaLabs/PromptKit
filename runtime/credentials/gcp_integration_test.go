package credentials

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// mockTokenSource implements oauth2.TokenSource for testing.
type mockTokenSource struct {
	mu    sync.Mutex
	token *oauth2.Token
	err   error
	calls int
}

func (m *mockTokenSource) Token() (*oauth2.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.token, m.err
}

func (m *mockTokenSource) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestVertexEndpoint(t *testing.T) {
	tests := []struct {
		project  string
		region   string
		expected string
	}{
		{
			project:  "my-project",
			region:   "us-central1",
			expected: "https://us-central1-aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/publishers/anthropic/models",
		},
		{
			project:  "prod-123",
			region:   "europe-west4",
			expected: "https://europe-west4-aiplatform.googleapis.com/v1/projects/prod-123/locations/europe-west4/publishers/anthropic/models",
		},
		{
			project:  "test",
			region:   "asia-southeast1",
			expected: "https://asia-southeast1-aiplatform.googleapis.com/v1/projects/test/locations/asia-southeast1/publishers/anthropic/models",
		},
	}

	for _, tt := range tests {
		t.Run(tt.project+"/"+tt.region, func(t *testing.T) {
			got := VertexEndpoint(tt.project, tt.region)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestGCPCredential_Apply(t *testing.T) {
	mock := &mockTokenSource{
		token: &oauth2.Token{
			AccessToken: "ya29.test-token",
			Expiry:      time.Now().Add(1 * time.Hour),
		},
	}

	cred := &GCPCredential{
		project:     "my-project",
		region:      "us-central1",
		tokenSource: mock,
	}

	req, err := http.NewRequest("POST", "https://us-central1-aiplatform.googleapis.com/v1/test", nil)
	require.NoError(t, err)

	err = cred.Apply(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, "Bearer ya29.test-token", req.Header.Get("Authorization"))
}

func TestGCPCredential_Apply_TokenError(t *testing.T) {
	mock := &mockTokenSource{
		err: fmt.Errorf("token source unavailable"),
	}

	cred := &GCPCredential{
		project:     "my-project",
		region:      "us-central1",
		tokenSource: mock,
	}

	req, err := http.NewRequest("POST", "https://example.com", nil)
	require.NoError(t, err)

	err = cred.Apply(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get GCP token")
	assert.Contains(t, err.Error(), "token source unavailable")
}

func TestGCPCredential_Type(t *testing.T) {
	cred := &GCPCredential{}
	assert.Equal(t, "gcp", cred.Type())
}

func TestGCPCredential_ProjectAndRegion(t *testing.T) {
	cred := &GCPCredential{
		project: "my-gcp-project",
		region:  "europe-west1",
	}

	assert.Equal(t, "my-gcp-project", cred.Project())
	assert.Equal(t, "europe-west1", cred.Region())
}

func TestGCPCredential_TokenCaching(t *testing.T) {
	mock := &mockTokenSource{
		token: &oauth2.Token{
			AccessToken: "ya29.cached-token",
			Expiry:      time.Now().Add(1 * time.Hour),
		},
	}

	cred := &GCPCredential{
		project:     "my-project",
		region:      "us-central1",
		tokenSource: mock,
	}

	// Call Apply multiple times
	for i := 0; i < 5; i++ {
		req, err := http.NewRequest("POST", "https://example.com", nil)
		require.NoError(t, err)

		err = cred.Apply(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, "Bearer ya29.cached-token", req.Header.Get("Authorization"))
	}

	// Token source should only be called once — subsequent calls use the cache
	assert.Equal(t, 1, mock.getCalls())
}

func TestGCPCredential_TokenRefresh(t *testing.T) {
	// Start with an expired token so the first call fetches, then we swap to a new one
	mock := &mockTokenSource{
		token: &oauth2.Token{
			AccessToken: "ya29.first-token",
			// Expiry within the refresh buffer — will NOT be cached
			Expiry: time.Now().Add(2 * time.Minute),
		},
	}

	cred := &GCPCredential{
		project:     "my-project",
		region:      "us-central1",
		tokenSource: mock,
	}

	// First call — token is within refresh buffer so it won't be cached
	req1, err := http.NewRequest("POST", "https://example.com", nil)
	require.NoError(t, err)
	err = cred.Apply(context.Background(), req1)
	require.NoError(t, err)
	assert.Equal(t, "Bearer ya29.first-token", req1.Header.Get("Authorization"))

	// Update mock to return a new token
	mock.mu.Lock()
	mock.token = &oauth2.Token{
		AccessToken: "ya29.second-token",
		Expiry:      time.Now().Add(2 * time.Minute),
	}
	mock.mu.Unlock()

	// Second call — since first token was not cached, it fetches again
	req2, err := http.NewRequest("POST", "https://example.com", nil)
	require.NoError(t, err)
	err = cred.Apply(context.Background(), req2)
	require.NoError(t, err)
	assert.Equal(t, "Bearer ya29.second-token", req2.Header.Get("Authorization"))

	// Token source should be called twice
	assert.Equal(t, 2, mock.getCalls())
}

func TestGCPCredential_ConcurrentAccess(t *testing.T) {
	mock := &mockTokenSource{
		token: &oauth2.Token{
			AccessToken: "ya29.concurrent-token",
			Expiry:      time.Now().Add(1 * time.Hour),
		},
	}

	cred := &GCPCredential{
		project:     "my-project",
		region:      "us-central1",
		tokenSource: mock,
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
