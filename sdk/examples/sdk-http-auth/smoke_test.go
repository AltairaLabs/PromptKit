package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// authServer records the headers each path received and answers with a
// customer record that carries the fields the example strips.
type authServer struct {
	mu      sync.Mutex
	headers map[string]http.Header
}

func newAuthServer(t *testing.T) (*authServer, string) {
	t.Helper()
	s := &authServer{headers: map[string]http.Header{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.headers[r.URL.Path] = r.Header.Clone()
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"C-1234","name":"John Smith","ssn":"123-45-6789","credit_card":"4111"}`))
	}))
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func (s *authServer) header(path, key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.headers[path]
	if !ok {
		return ""
	}
	return h.Get(key)
}

// The OAuth pattern only demonstrates anything if the token reaches the server.
func TestSmoke_OAuthTokenIsSent(t *testing.T) {
	s, base := newAuthServer(t)
	cache := &tokenCache{clientID: "id", clientSecret: "secret"}

	result, err := customerToolConfig(base, cache).HandlerCtx()(t.Context(), map[string]any{"name": "John Smith"})
	if err != nil {
		t.Fatalf("get_customer: %v", err)
	}

	got := s.header("/customers", "Authorization")
	if !strings.HasPrefix(got, "Bearer access-token-") {
		t.Errorf("Authorization = %q, want the cached OAuth bearer token", got)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result = %T, want a JSON object", result)
	}
	for _, f := range []string{"ssn", "credit_card"} {
		if _, present := m[f]; present {
			t.Errorf("result still has %q: %v", f, m)
		}
	}
	if m["name"] != "John Smith" {
		t.Errorf("name = %v, want John Smith", m["name"])
	}
}

func TestSmoke_StaticAndEnvHeadersAreSent(t *testing.T) {
	s, base := newAuthServer(t)
	t.Setenv("ANALYTICS_API_TOKEN", "Bearer analytics-token")

	if _, err := productsToolConfig(base).Handler()(map[string]any{"query": "shoes"}); err != nil {
		t.Fatalf("search_products: %v", err)
	}
	if _, err := analyticsToolConfig(base).Handler()(map[string]any{"metric": "dau"}); err != nil {
		t.Fatalf("get_analytics: %v", err)
	}

	if got := s.header("/products", "X-Api-Key"); got != "pk_live_abc123" {
		t.Errorf("X-Api-Key = %q, want pk_live_abc123", got)
	}
	if got := s.header("/analytics", "Authorization"); got != "Bearer analytics-token" {
		t.Errorf("Authorization = %q, want the env-provided token", got)
	}
}
