// Package main demonstrates adding custom authentication to HTTP tools via the SDK.
//
// This example shows three patterns for auth injection:
//   - Static API key via WithHeader
//   - Environment-based headers via WithHeaderFromEnv
//   - Dynamic OAuth with token caching via WithPreRequest
//
// Usage:
//
//	go run ./sdk/examples/sdk-http-auth
//
// No API key required — uses a mock provider with canned responses.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

func main() {
	fmt.Println("=== SDK HTTP Auth Example ===")
	fmt.Println()

	// Use a mock provider so the example runs without API keys.
	repo := mock.NewInMemoryMockRepository(
		`{"response": "Customer John Smith was found: ID C-1234, email john@example.com."}`,
	)
	provider := mock.NewProviderWithRepository("mock", "mock-model", false, repo)

	conv, err := sdk.Open("./sdk/examples/sdk-http-auth/assistant.pack.json", "assistant",
		sdk.WithProvider(provider),
	)
	if err != nil {
		log.Fatalf("Failed to open conversation: %v", err)
	}
	defer conv.Close()

	// --- Pattern 1: Static API key ---
	staticCfg := sdktools.NewHTTPToolConfig("https://api.example.com/products",
		sdktools.WithMethod("GET"),
		sdktools.WithHeader("X-Api-Key", "pk_live_abc123"),
		sdktools.WithHeader("Accept", "application/json"),
	)
	conv.OnTool("search_products", staticCfg.Handler())
	fmt.Println("Registered search_products with static API key")

	// --- Pattern 2: Environment variable header ---
	envCfg := sdktools.NewHTTPToolConfig("https://api.example.com/analytics",
		sdktools.WithMethod("GET"),
		sdktools.WithHeaderFromEnv("Authorization=ANALYTICS_API_TOKEN"),
	)
	conv.OnTool("get_analytics", envCfg.Handler())
	fmt.Println("Registered get_analytics with env-based auth")

	// --- Pattern 3: Dynamic OAuth with token caching ---
	cache := &tokenCache{
		clientID:     "my-client-id",
		clientSecret: "my-client-secret",
	}

	oauthCfg := sdktools.NewHTTPToolConfig("https://api.example.com/customers",
		sdktools.WithMethod("GET"),
		sdktools.WithTimeout(10000),
		sdktools.WithPreRequest(func(req *http.Request) error {
			token, err := cache.getToken()
			if err != nil {
				return fmt.Errorf("oauth token refresh: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}),
		sdktools.WithPostProcess(func(resp []byte) ([]byte, error) {
			// Strip sensitive fields before sending to the LLM
			return redactFields(resp, "ssn", "credit_card")
		}),
		sdktools.WithRedact("ssn", "credit_card"),
	)

	conv.OnToolCtx("get_customer", oauthCfg.HandlerCtx())
	fmt.Println("Registered get_customer with OAuth + token caching")
	fmt.Println()

	// Send a message that would trigger tool use.
	ctx := context.Background()
	resp, err := conv.Send(ctx, "Look up customer John Smith")
	if err != nil {
		log.Fatalf("Send failed: %v", err)
	}

	fmt.Printf("Response: %s\n", resp.Text())
}

// tokenCache manages OAuth token refresh with automatic caching.
type tokenCache struct {
	mu           sync.Mutex
	clientID     string
	clientSecret string
	token        string
	expires      time.Time
}

func (tc *tokenCache) getToken() (string, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.token != "" && time.Now().Before(tc.expires) {
		return tc.token, nil
	}

	// In production, this would call your OAuth token endpoint.
	// Example: POST /oauth/token with client_credentials grant.
	tc.token = fmt.Sprintf("access-token-%d", time.Now().Unix())
	tc.expires = time.Now().Add(55 * time.Minute)

	fmt.Printf("  [tokenCache] refreshed token (expires %s)\n", tc.expires.Format("15:04:05"))
	return tc.token, nil
}

// redactFields removes sensitive keys from a JSON response.
func redactFields(data []byte, fields ...string) ([]byte, error) {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return data, nil // not JSON, pass through
	}

	for _, field := range fields {
		delete(obj, field)
	}

	return json.Marshal(obj)
}
