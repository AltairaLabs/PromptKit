---
title: 'Tutorial 5: Production Deployment'
sidebar:
  order: 5
---
Build production-ready LLM applications with proper error handling, monitoring, and scalability.

**Time**: 40 minutes  
**Level**: Advanced

## What You'll Build

A production-ready API server with error handling, monitoring, cost tracking, and multiple providers.

## What You'll Learn

- Error handling strategies
- Multi-provider fallback
- Cost monitoring
- Logging and metrics
- Health checks
- Graceful shutdown

## Prerequisites

- Completed [Tutorial 1](01-first-pipeline)
- Multiple LLM API keys (OpenAI, Claude recommended)

## Production Architecture

```d2
direction: down

server: HTTP Server
validation: Request Validation
pipeline: Pipeline (with fallback providers) {
  state: State Middleware (Redis)
  valid: Validation Middleware
  provider: Provider Middleware
}
response: Response + Metrics

server -> validation -> pipeline -> response
```

## Step 1: Production Pipeline

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "sync"
    "sync/atomic"
    "syscall"
    "time"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/anthropic"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/types"
    "github.com/AltairaLabs/PromptKit/runtime/validators"
)

type Server struct {
    pipes         []pipeline.Pipeline
    store         statestore.StateStore
    metrics       *Metrics
    currentPipe   int32
}

type Metrics struct {
    mu           sync.Mutex
    totalReqs    int64
    totalErrors  int64
    totalCost    float64
    providerUse  map[string]int64
}

func (m *Metrics) RecordRequest(provider string, cost float64, err error) {
    atomic.AddInt64(&m.totalReqs, 1)
    
    if err != nil {
        atomic.AddInt64(&m.totalErrors, 1)
    }
    
    m.mu.Lock()
    m.totalCost += cost
    m.providerUse[provider]++
    m.mu.Unlock()
}

func (m *Metrics) Report() map[string]interface{} {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    return map[string]interface{}{
        "total_requests": atomic.LoadInt64(&m.totalReqs),
        "total_errors":   atomic.LoadInt64(&m.totalErrors),
        "total_cost":     fmt.Sprintf("$%.4f", m.totalCost),
        "provider_usage": m.providerUse,
        "error_rate":     float64(atomic.LoadInt64(&m.totalErrors)) / float64(atomic.LoadInt64(&m.totalReqs)),
    }
}

func NewServer() (*Server, error) {
    // Create state store
    store, err := statestore.NewRedisStateStore("localhost:6379", "", 0)
    if err != nil {
        log.Printf("Redis unavailable, using in-memory: %v", err)
        store = statestore.NewInMemoryStateStore()
    }
    
    // Create providers
    openaiProvider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        os.Getenv("OPENAI_API_KEY"),
        openai.DefaultProviderDefaults(),
        false,
    )
    
    claudeProvider := anthropic.NewAnthropicProvider(
        "claude",
        "claude-3-5-haiku-20241022",
        os.Getenv("ANTHROPIC_API_KEY"),
        anthropic.DefaultProviderDefaults(),
        false,
    )
    
    // Validators
    bannedWords := validators.NewBannedWordsValidator([]string{"spam", "hack"})
    lengthValidator := validators.NewLengthValidator(1, 1000)
    
    // Pipeline config
    config := &middleware.ProviderMiddlewareConfig{
        MaxTokens:   1000,
        Temperature: 0.7,
    }
    
    // Create pipelines for each provider
    openaiPipe := pipeline.NewPipeline(
        middleware.StateMiddleware(store),
        middleware.ValidatorMiddleware(bannedWords, lengthValidator),
        middleware.ProviderMiddleware(openaiProvider, nil, nil, config),
    )
    
    claudePipe := pipeline.NewPipeline(
        middleware.StateMiddleware(store),
        middleware.ValidatorMiddleware(bannedWords, lengthValidator),
        middleware.ProviderMiddleware(claudeProvider, nil, nil, config),
    )
    
    return &Server{
        pipes: []pipeline.Pipeline{openaiPipe, claudePipe},
        store: store,
        metrics: &Metrics{
            providerUse: make(map[string]int64),
        },
    }, nil
}

func (s *Server) Execute(ctx context.Context, sessionID, role, content string) (*pipeline.PipelineResult, error) {
    var lastErr error
    
    // Try each provider
    for i := 0; i < len(s.pipes); i++ {
        pipeIdx := int(atomic.LoadInt32(&s.currentPipe)) % len(s.pipes)
        pipe := s.pipes[pipeIdx]
        
        result, err := pipe.ExecuteWithContext(ctx, sessionID, role, content)
        
        providerName := fmt.Sprintf("provider_%d", pipeIdx)
        if result != nil {
            s.metrics.RecordRequest(providerName, result.Cost.TotalCost, err)
        } else {
            s.metrics.RecordRequest(providerName, 0, err)
        }
        
        if err == nil {
            atomic.StoreInt32(&s.currentPipe, int32(pipeIdx))
            return result, nil
        }
        
        lastErr = err
        log.Printf("Provider %d failed: %v", pipeIdx, err)
        atomic.StoreInt32(&s.currentPipe, int32((pipeIdx+1)%len(s.pipes)))
    }
    
    return nil, fmt.Errorf("all providers failed: %w", lastErr)
}

func (s *Server) Close() {
    ctx := context.Background()
    for _, pipe := range s.pipes {
        pipe.Shutdown(ctx)
    }
    if s.store != nil {
        s.store.Close()
    }
}

type ChatRequest struct {
    SessionID string `json:"session_id"`
    Message   string `json:"message"`
}

type ChatResponse struct {
    Response string  `json:"response"`
    Tokens   int     `json:"tokens"`
    Cost     float64 `json:"cost"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    
    var req ChatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }
    
    if req.SessionID == "" || req.Message == "" {
        http.Error(w, "session_id and message required", http.StatusBadRequest)
        return
    }
    
    ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
    defer cancel()
    
    result, err := s.Execute(ctx, req.SessionID, "user", req.Message)
    if err != nil {
        log.Printf("Request failed: %v", err)
        http.Error(w, "Request failed", http.StatusInternalServerError)
        return
    }
    
    resp := ChatResponse{
        Response: result.Response.Content,
        Tokens:   result.Response.Usage.TotalTokens,
        Cost:     result.Cost.TotalCost,
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(s.metrics.Report())
}

func main() {
    server, err := NewServer()
    if err != nil {
        log.Fatal(err)
    }
    defer server.Close()
    
    http.HandleFunc("/chat", server.handleChat)
    http.HandleFunc("/health", server.handleHealth)
    http.HandleFunc("/metrics", server.handleMetrics)
    
    httpServer := &http.Server{
        Addr:         ":8080",
        ReadTimeout:  10 * time.Second,
        WriteTimeout: 60 * time.Second,
    }
    
    // Graceful shutdown
    stop := make(chan os.Signal, 1)
    signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
    
    go func() {
        log.Println("Server starting on :8080")
        if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatal(err)
        }
    }()
    
    <-stop
    log.Println("Shutting down gracefully...")
    
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    if err := httpServer.Shutdown(ctx); err != nil {
        log.Printf("Shutdown error: %v", err)
    }
    
    log.Println("Server stopped")
}
```

## Step 2: Test the API

Start the server:

```bash
go run main.go
```

Send requests:

```bash
# Chat
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"session_id": "user-1", "message": "Hello!"}'

# Health check
curl http://localhost:8080/health

# Metrics
curl http://localhost:8080/metrics
```

## Production Patterns

### 1. Retry with Exponential Backoff

```go
func executeWithRetry(pipe *pipeline.Pipeline, ctx context.Context, sessionID, role, content string, maxRetries int) (*pipeline.PipelineResult, error) {
    for i := 0; i < maxRetries; i++ {
        result, err := pipe.ExecuteWithContext(ctx, sessionID, role, content)
        if err == nil {
            return result, nil
        }
        
        if !isRetryable(err) {
            return nil, err
        }
        
        backoff := time.Duration(1<<uint(i)) * time.Second
        time.Sleep(backoff)
    }
    
    return nil, fmt.Errorf("failed after %d retries", maxRetries)
}

func isRetryable(err error) bool {
    errStr := err.Error()
    return strings.Contains(errStr, "rate_limit") ||
           strings.Contains(errStr, "timeout") ||
           strings.Contains(errStr, "503")
}
```

### 2. Circuit Breaker

```go
type CircuitBreaker struct {
    failures    int32
    maxFailures int32
    timeout     time.Duration
    lastFailure time.Time
    mu          sync.Mutex
}

func (cb *CircuitBreaker) Call(fn func() error) error {
    cb.mu.Lock()
    failures := atomic.LoadInt32(&cb.failures)
    
    if failures >= cb.maxFailures {
        if time.Since(cb.lastFailure) < cb.timeout {
            cb.mu.Unlock()
            return fmt.Errorf("circuit breaker open")
        }
        atomic.StoreInt32(&cb.failures, 0)
    }
    cb.mu.Unlock()
    
    err := fn()
    if err != nil {
        atomic.AddInt32(&cb.failures, 1)
        cb.mu.Lock()
        cb.lastFailure = time.Now()
        cb.mu.Unlock()
    } else {
        atomic.StoreInt32(&cb.failures, 0)
    }
    
    return err
}
```

### 3. Rate Limiting

```go
import "golang.org/x/time/rate"

limiter := rate.NewLimiter(rate.Limit(10), 1)  // 10 req/sec

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
    if !limiter.Allow() {
        http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
        return
    }
    
    // Process request...
}
```

## What You've Learned

✅ Error handling strategies  
✅ Multi-provider fallback  
✅ Cost monitoring  
✅ Logging and metrics  
✅ Health checks  
✅ Graceful shutdown  
✅ Production API patterns  

## Next Steps

Continue to [Tutorial 6: Advanced Patterns](06-advanced-patterns) for optimization techniques.

## See Also

- [Handle Errors](../how-to/handle-errors) - Error strategies
- [Monitor Costs](../how-to/monitor-costs) - Cost tracking
