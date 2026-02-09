---
title: 'Tutorial 6: Advanced Patterns'
sidebar:
  order: 6
---
Master advanced optimization and architectural patterns.

**Time**: 30 minutes  
**Level**: Advanced

## What You'll Learn

- Streaming optimization
- Response caching
- Custom middleware
- Performance tuning
- Advanced provider patterns

## Prerequisites

- Completed previous tutorials
- Understanding of Go concurrency

## Pattern 1: Response Caching

Cache common queries to reduce costs:

```go
package main

import (
    "crypto/sha256"
    "fmt"
    "sync"
    "time"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
)

type CacheEntry struct {
    result    *pipeline.PipelineResult
    timestamp time.Time
}

type ResponseCache struct {
    cache map[string]*CacheEntry
    ttl   time.Duration
    mu    sync.RWMutex
}

func NewResponseCache(ttl time.Duration) *ResponseCache {
    cache := &ResponseCache{
        cache: make(map[string]*CacheEntry),
        ttl:   ttl,
    }
    
    // Cleanup expired entries
    go cache.cleanup()
    
    return cache
}

func (c *ResponseCache) key(prompt string) string {
    hash := sha256.Sum256([]byte(prompt))
    return fmt.Sprintf("%x", hash[:16])
}

func (c *ResponseCache) Get(prompt string) (*pipeline.PipelineResult, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    entry, exists := c.cache[c.key(prompt)]
    if !exists {
        return nil, false
    }
    
    if time.Since(entry.timestamp) > c.ttl {
        return nil, false
    }
    
    return entry.result, true
}

func (c *ResponseCache) Set(prompt string, result *pipeline.PipelineResult) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    c.cache[c.key(prompt)] = &CacheEntry{
        result:    result,
        timestamp: time.Now(),
    }
}

func (c *ResponseCache) cleanup() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        c.mu.Lock()
        now := time.Now()
        for key, entry := range c.cache {
            if now.Sub(entry.timestamp) > c.ttl {
                delete(c.cache, key)
            }
        }
        c.mu.Unlock()
    }
}

// Usage
cache := NewResponseCache(10 * time.Minute)

prompt := "What is AI?"
if cached, exists := cache.Get(prompt); exists {
    fmt.Println("Cache hit!")
    return cached, nil
}

result, err := pipe.Execute(ctx, "user", prompt)
if err == nil {
    cache.Set(prompt, result)
}
```

## Pattern 2: Custom Middleware

Create domain-specific middleware:

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/types"
)

// LoggingWrapper wraps a provider to add logging
type LoggingWrapper struct {
    inner providers.Provider
}

func NewLoggingWrapper(inner providers.Provider) *LoggingWrapper {
    return &LoggingWrapper{inner: inner}
}

func (w *LoggingWrapper) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
    start := time.Now()

    content := ""
    if len(req.Messages) > 0 {
        content = req.Messages[len(req.Messages)-1].Content
        if len(content) > 50 {
            content = content[:50]
        }
    }
    fmt.Printf("[%s] Processing: %s...\n", time.Now().Format("15:04:05"), content)

    resp, err := w.inner.Predict(ctx, req)

    duration := time.Since(start)
    if err != nil {
        fmt.Printf("[%s] Error after %v: %v\n", time.Now().Format("15:04:05"), duration, err)
    } else if resp.CostInfo != nil {
        fmt.Printf("[%s] Success in %v (cost: $%.6f)\n",
            time.Now().Format("15:04:05"),
            duration,
            resp.CostInfo.TotalCost)
    }

    return resp, err
}

// BudgetWrapper wraps a provider to enforce budget limits
type BudgetWrapper struct {
    inner  providers.Provider
    budget float64
    spent  float64
    mu     sync.Mutex
}

func NewBudgetWrapper(inner providers.Provider, budget float64) *BudgetWrapper {
    return &BudgetWrapper{
        inner:  inner,
        budget: budget,
    }
}

func (w *BudgetWrapper) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
    w.mu.Lock()
    if w.spent >= w.budget {
        w.mu.Unlock()
        return providers.PredictionResponse{}, fmt.Errorf("budget exceeded: $%.2f / $%.2f", w.spent, w.budget)
    }
    w.mu.Unlock()

    resp, err := w.inner.Predict(ctx, req)
    if err == nil && resp.CostInfo != nil {
        w.mu.Lock()
        w.spent += resp.CostInfo.TotalCost
        w.mu.Unlock()
    }

    return resp, err
}
```

## Pattern 3: Streaming Optimization

Optimize streaming for better UX:

```go
package main

import (
    "context"
    "fmt"
    "io"
    "strings"
    "time"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
)

type StreamBuffer struct {
    chunks    []string
    lastFlush time.Time
    minDelay  time.Duration
}

func NewStreamBuffer(minDelay time.Duration) *StreamBuffer {
    return &StreamBuffer{
        chunks:    make([]string, 0),
        lastFlush: time.Now(),
        minDelay:  minDelay,
    }
}

func (sb *StreamBuffer) Add(chunk string) string {
    sb.chunks = append(sb.chunks, chunk)
    
    if time.Since(sb.lastFlush) >= sb.minDelay {
        return sb.Flush()
    }
    
    return ""
}

func (sb *StreamBuffer) Flush() string {
    if len(sb.chunks) == 0 {
        return ""
    }
    
    result := strings.Join(sb.chunks, "")
    sb.chunks = sb.chunks[:0]
    sb.lastFlush = time.Now()
    return result
}

func streamWithBuffer(pipe *pipeline.Pipeline, ctx context.Context, role, content string) error {
    stream, err := pipe.ExecuteStream(ctx, role, content)
    if err != nil {
        return err
    }
    defer stream.Close()
    
    buffer := NewStreamBuffer(100 * time.Millisecond)
    
    for {
        chunk, err := stream.Next()
        if err == io.EOF {
            // Flush remaining
            if remaining := buffer.Flush(); remaining != "" {
                fmt.Print(remaining)
            }
            break
        }
        if err != nil {
            return err
        }
        
        if output := buffer.Add(chunk.Content); output != "" {
            fmt.Print(output)
        }
    }
    
    return nil
}
```

## Pattern 4: Provider Pool

Load balance across multiple providers:

```go
package main

import (
    "context"
    "sync"
    "sync/atomic"
    
    "github.com/AltairaLabs/PromptKit/runtime/types"
)

type ProviderPool struct {
    providers []providers.Provider
    current   int32
    health    map[string]*ProviderHealth
    mu        sync.RWMutex
}

type ProviderHealth struct {
    failures    int32
    lastFailure int64
    available   bool
}

func NewProviderPool(providerList ...providers.Provider) *ProviderPool {
    pool := &ProviderPool{
        providers: providerList,
        health:    make(map[string]*ProviderHealth),
    }

    for _, p := range providerList {
        pool.health[p.ID()] = &ProviderHealth{
            available: true,
        }
    }

    return pool
}

func (pp *ProviderPool) Execute(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
    startIdx := int(atomic.LoadInt32(&pp.current)) % len(pp.providers)

    for i := 0; i < len(pp.providers); i++ {
        idx := (startIdx + i) % len(pp.providers)
        provider := pp.providers[idx]

        // Check health
        pp.mu.RLock()
        health := pp.health[provider.ID()]
        pp.mu.RUnlock()

        if !health.available {
            continue
        }

        response, err := provider.Predict(ctx, req)
        if err == nil {
            atomic.StoreInt32(&pp.current, int32(idx))
            atomic.StoreInt32(&health.failures, 0)
            return response, nil
        }

        // Record failure
        failures := atomic.AddInt32(&health.failures, 1)
        if failures >= 3 {
            pp.mu.Lock()
            health.available = false
            pp.mu.Unlock()

            // Reset after 1 minute
            go func(h *ProviderHealth) {
                time.Sleep(time.Minute)
                pp.mu.Lock()
                h.available = true
                atomic.StoreInt32(&h.failures, 0)
                pp.mu.Unlock()
            }(health)
        }
    }

    return providers.PredictionResponse{}, fmt.Errorf("all providers unavailable")
}
```

## Pattern 5: Parallel Processing

Process multiple requests concurrently:

```go
package main

import (
    "context"
    "sync"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
)

type BatchRequest struct {
    SessionID string
    Role      string
    Content   string
}

type BatchResult struct {
    Request *BatchRequest
    Result  *pipeline.PipelineResult
    Error   error
}

func processBatch(pipe *pipeline.Pipeline, ctx context.Context, requests []BatchRequest, maxConcurrent int) []BatchResult {
    results := make([]BatchResult, len(requests))
    sem := make(chan struct{}, maxConcurrent)
    var wg sync.WaitGroup
    
    for i, req := range requests {
        wg.Add(1)
        go func(idx int, r BatchRequest) {
            defer wg.Done()
            
            sem <- struct{}{}
            defer func() { <-sem }()
            
            result, err := pipe.ExecuteWithContext(ctx, r.SessionID, r.Role, r.Content)
            results[idx] = BatchResult{
                Request: &r,
                Result:  result,
                Error:   err,
            }
        }(i, req)
    }
    
    wg.Wait()
    return results
}

// Usage
requests := []BatchRequest{
    {SessionID: "user-1", Role: "user", Content: "Hello"},
    {SessionID: "user-2", Role: "user", Content: "Hi there"},
    {SessionID: "user-3", Role: "user", Content: "Good morning"},
}

results := processBatch(pipe, ctx, requests, 3)  // Process 3 at a time
for _, r := range results {
    if r.Error != nil {
        fmt.Printf("Error for %s: %v\n", r.Request.SessionID, r.Error)
    } else {
        fmt.Printf("%s: %s\n", r.Request.SessionID, r.Result.Response.Content)
    }
}
```

## Performance Tips

### 1. Connection Pooling

Reuse providers across requests:

```go
// Create once, reuse many times
provider := openai.NewProvider(...)
defer provider.Close()

// Use in multiple pipelines or requests
```

### 2. Context Timeouts

Set appropriate timeouts:

```go
// Short timeout for simple queries
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

// Longer timeout for complex tasks
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
```

### 3. Token Optimization

Reduce token usage:

```go
// Trim conversation history
if len(messages) > 10 {
    messages = messages[len(messages)-10:]
}

// Limit output
config := &middleware.ProviderMiddlewareConfig{
    MaxTokens: 300,  // Shorter responses
}
```

### 4. Model Selection

Use appropriate models:

```go
// Simple tasks: cheap, fast
"gpt-4o-mini"

// Complex reasoning: expensive, better
"gpt-4o"

// Long context: specialized
"claude-3-5-sonnet-20241022"
```

## Complete Advanced Example

Combining all patterns:

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/types"
)

func main() {
    // Response cache
    cache := NewResponseCache(10 * time.Minute)

    // Provider with connection pooling
    provider := openai.NewProvider(
        "openai",
        "gpt-4o-mini",
        "", // uses OPENAI_API_KEY env var
        providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
        false,
    )
    defer provider.Close()

    // Wrap with logging and budget
    budgetProvider := NewBudgetWrapper(provider, 1.0) // $1 budget

    ctx := context.Background()

    // Process with caching
    prompts := []string{
        "What is AI?",
        "What is machine learning?",
        "What is AI?",  // Cache hit!
    }

    for _, prompt := range prompts {
        if cached, exists := cache.Get(prompt); exists {
            fmt.Printf("\n[CACHE] %s\n", prompt)
            fmt.Printf("Response: %s\n", cached.Content)
            continue
        }

        resp, err := budgetProvider.Predict(ctx, providers.PredictionRequest{
            Messages:    []types.Message{{Role: "user", Content: prompt}},
            Temperature: 0.7,
            MaxTokens:   500,
        })
        if err != nil {
            fmt.Printf("Error: %v\n", err)
            continue
        }

        fmt.Printf("\nResponse: %s\n", resp.Content)
    }
}
```

## What You've Learned

✅ Response caching  
✅ Custom middleware  
✅ Streaming optimization  
✅ Provider pooling  
✅ Parallel processing  
✅ Performance tuning  
✅ Production patterns  

## Congratulations!

You've completed the Runtime tutorial series! You now know how to build production-ready LLM applications.

## Next Steps

- Explore [Runtime Reference](../reference/index) for complete API documentation
- Read [Runtime Explanation](../explanation/index) for architectural concepts
- Check [Runtime How-To](../how-to/index) for specific tasks

## See Also

- [Pipeline Reference](../reference/pipeline) - Complete API
- [Handle Errors](../how-to/handle-errors) - Error strategies
- [Monitor Costs](../how-to/monitor-costs) - Cost optimization
