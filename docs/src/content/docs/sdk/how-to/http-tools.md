---
title: HTTP Tools
sidebar:
  order: 4
---
Make HTTP API calls from tool handlers using `OnToolHTTP` and `HTTPToolConfig`.

## Basic HTTP Tool

```go
cfg := tools.NewHTTPToolConfig("https://api.example.com/search",
    tools.WithMethod("GET"),
)

conv.OnTool("search", cfg.Handler())
```

## Adding Authentication

### Static Headers

```go
cfg := tools.NewHTTPToolConfig("https://api.example.com/data",
    tools.WithHeader("Authorization", "Bearer my-token"),
    tools.WithHeader("X-Api-Key", "abc123"),
)
```

### Headers from Environment Variables

```go
cfg := tools.NewHTTPToolConfig("https://api.example.com/data",
    tools.WithHeaderFromEnv("Authorization=API_TOKEN"),
)
```

At runtime, the `Authorization` header is set to the value of `$API_TOKEN`.

### Dynamic Auth with PreRequest

Use `WithPreRequest` to add authentication that requires runtime logic — OAuth token refresh, request signing, or rotating credentials:

```go
cfg := tools.NewHTTPToolConfig("https://api.example.com/data",
    tools.WithMethod("GET"),
    tools.WithPreRequest(func(req *http.Request) error {
        token, err := getOAuthToken() // your token refresh logic
        if err != nil {
            return fmt.Errorf("auth failed: %w", err)
        }
        req.Header.Set("Authorization", "Bearer "+token)
        return nil
    }),
)

conv.OnToolCtx("fetch_data", cfg.HandlerCtx())
```

## Transform Arguments

Reshape LLM arguments before the HTTP request:

```go
cfg := tools.NewHTTPToolConfig("https://api.example.com/users",
    tools.WithTransform(func(args map[string]any) (map[string]any, error) {
        // Merge first_name and last_name into full_name
        first, _ := args["first_name"].(string)
        last, _ := args["last_name"].(string)
        return map[string]any{
            "full_name": first + " " + last,
            "email":     args["email"],
        }, nil
    }),
)
```

## Post-Process Responses

Filter or reshape the raw API response before it reaches the LLM:

```go
cfg := tools.NewHTTPToolConfig("https://api.example.com/search",
    tools.WithMethod("GET"),
    tools.WithPostProcess(func(resp []byte) ([]byte, error) {
        var data map[string]any
        if err := json.Unmarshal(resp, &data); err != nil {
            return resp, nil
        }
        // Extract just the results array
        results, _ := data["results"]
        return json.Marshal(results)
    }),
)
```

## Configuration Options

| Option | Description |
|--------|-------------|
| `WithMethod(method)` | HTTP method (default: POST) |
| `WithHeader(key, value)` | Static header |
| `WithHeaderFromEnv(spec)` | Header from env var (`Header-Name=ENV_VAR`) |
| `WithTimeout(ms)` | Request timeout in milliseconds |
| `WithRedact(fields...)` | Fields to redact from the response |
| `WithTransform(fn)` | Transform arguments before request |
| `WithPreRequest(fn)` | Modify the `*http.Request` before sending |
| `WithPostProcess(fn)` | Process raw response bytes after receiving |

## Complete Example: OAuth-Protected API

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "sync"
    "time"

    "github.com/AltairaLabs/PromptKit/sdk"
    sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

// tokenCache manages OAuth token refresh with caching.
type tokenCache struct {
    mu      sync.Mutex
    token   string
    expires time.Time
}

func (tc *tokenCache) getToken() (string, error) {
    tc.mu.Lock()
    defer tc.mu.Unlock()

    if tc.token != "" && time.Now().Before(tc.expires) {
        return tc.token, nil
    }

    // Replace with your OAuth token endpoint
    token, err := refreshOAuthToken(os.Getenv("OAUTH_CLIENT_ID"), os.Getenv("OAUTH_CLIENT_SECRET"))
    if err != nil {
        return "", err
    }

    tc.token = token
    tc.expires = time.Now().Add(55 * time.Minute) // refresh 5 min early
    return tc.token, nil
}

func main() {
    conv, err := sdk.Open("./assistant.pack.json", "assistant")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    cache := &tokenCache{}

    // Register an HTTP tool with OAuth auth
    cfg := sdktools.NewHTTPToolConfig("https://api.example.com/customers",
        sdktools.WithMethod("GET"),
        sdktools.WithTimeout(10000),
        sdktools.WithPreRequest(func(req *http.Request) error {
            token, err := cache.getToken()
            if err != nil {
                return fmt.Errorf("oauth token refresh: %w", err)
            }
            req.Header.Set("Authorization", "Bearer "+token)
            req.Header.Set("X-Request-ID", generateRequestID())
            return nil
        }),
        sdktools.WithPostProcess(func(resp []byte) ([]byte, error) {
            // Filter response to only include fields the LLM needs
            return filterCustomerFields(resp)
        }),
        sdktools.WithRedact("ssn", "credit_card"),
    )

    conv.OnToolCtx("get_customer", cfg.HandlerCtx())

    ctx := context.Background()
    resp, err := conv.Send(ctx, "Look up customer John Smith")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Text())
}

func refreshOAuthToken(clientID, clientSecret string) (string, error) {
    // Your OAuth2 client credentials flow here
    return "access-token-xyz", nil
}

func generateRequestID() string {
    return fmt.Sprintf("req-%d", time.Now().UnixNano())
}

func filterCustomerFields(resp []byte) ([]byte, error) {
    // Your response filtering logic
    return resp, nil
}
```

## Custom RequestMapper (Runtime-Level)

For advanced use cases, implement the `RequestMapper` interface to fully control how arguments map to HTTP requests:

```go
import rttools "github.com/AltairaLabs/PromptKit/runtime/tools"

type HMACRequestMapper struct {
    rttools.DefaultRequestMapper // embed default behavior
    secretKey string
}

func (m *HMACRequestMapper) RenderHeaders(
    templates map[string]string, args map[string]any,
) (map[string]string, error) {
    // Call the default implementation first
    headers, err := m.DefaultRequestMapper.RenderHeaders(templates, args)
    if err != nil {
        return nil, err
    }
    if headers == nil {
        headers = make(map[string]string)
    }

    // Add HMAC signature
    headers["X-Signature"] = computeHMAC(args, m.secretKey)
    return headers, nil
}

// Wire into the executor
executor := rttools.NewHTTPExecutor()
executor.RequestMapper = &HMACRequestMapper{secretKey: os.Getenv("HMAC_SECRET")}
```

## See Also

- [Register Tools](register-tools) - Basic tool registration
- [HTTP Tool Mapping](../../runtime/how-to/http-tool-mapping) - YAML-based declarative mapping
- [Client-Side Tools](client-tools) - Tools that run on the client device
