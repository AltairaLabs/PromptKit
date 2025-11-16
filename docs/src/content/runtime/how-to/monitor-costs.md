---
title: Monitor Costs
docType: how-to
order: 7
---
# How to Monitor Costs

Track and optimize LLM API costs.

## Goal

Monitor token usage and costs for budget control.

## Quick Start

```go
result, err := pipe.Execute(ctx, "user", "Your message")
if err != nil {
    log.Fatal(err)
}

// Check cost
log.Printf("Tokens: %d", result.Response.Usage.TotalTokens)
log.Printf("Cost: $%.6f", result.Cost.TotalCost)
```

## Cost Tracking

### Basic Cost Info

```go
result, _ := pipe.Execute(ctx, "user", "Explain AI")

// Token usage
usage := result.Response.Usage
log.Printf("Input tokens: %d", usage.PromptTokens)
log.Printf("Output tokens: %d", usage.CompletionTokens)
log.Printf("Total tokens: %d", usage.TotalTokens)

// Cost breakdown
cost := result.Cost
log.Printf("Input cost: $%.6f", cost.InputCost)
log.Printf("Output cost: $%.6f", cost.OutputCost)
log.Printf("Total cost: $%.6f", cost.TotalCost)
```

### Per-Session Tracking

```go
type SessionCosts struct {
    mu            sync.Mutex
    costs         map[string]float64
    tokens        map[string]int
    requestCounts map[string]int
}

func (sc *SessionCosts) Record(sessionID string, result *pipeline.PipelineResult) {
    sc.mu.Lock()
    defer sc.mu.Unlock()
    
    sc.costs[sessionID] += result.Cost.TotalCost
    sc.tokens[sessionID] += result.Response.Usage.TotalTokens
    sc.requestCounts[sessionID]++
}

func (sc *SessionCosts) GetStats(sessionID string) (float64, int, int) {
    sc.mu.Lock()
    defer sc.mu.Unlock()
    
    return sc.costs[sessionID], sc.tokens[sessionID], sc.requestCounts[sessionID]
}

// Usage
tracker := &SessionCosts{
    costs:         make(map[string]float64),
    tokens:        make(map[string]int),
    requestCounts: make(map[string]int),
}

result, _ := pipe.ExecuteWithContext(ctx, sessionID, "user", "Hello")
tracker.Record(sessionID, result)

cost, tokens, count := tracker.GetStats(sessionID)
log.Printf("Session %s: $%.6f, %d tokens, %d requests", sessionID, cost, tokens, count)
```

### Cumulative Cost Tracking

```go
import "sync/atomic"

type CostTracker struct {
    totalCost   int64  // Store as cents (multiply by 100)
    totalTokens int64
    totalReqs   int64
}

func (ct *CostTracker) Record(result *pipeline.PipelineResult) {
    costCents := int64(result.Cost.TotalCost * 100)
    atomic.AddInt64(&ct.totalCost, costCents)
    atomic.AddInt64(&ct.totalTokens, int64(result.Response.Usage.TotalTokens))
    atomic.AddInt64(&ct.totalReqs, 1)
}

func (ct *CostTracker) Report() {
    cost := float64(atomic.LoadInt64(&ct.totalCost)) / 100
    tokens := atomic.LoadInt64(&ct.totalTokens)
    reqs := atomic.LoadInt64(&ct.totalReqs)
    
    log.Printf("Total cost: $%.2f", cost)
    log.Printf("Total tokens: %d", tokens)
    log.Printf("Total requests: %d", reqs)
    log.Printf("Avg cost/request: $%.4f", cost/float64(reqs))
    log.Printf("Avg tokens/request: %.0f", float64(tokens)/float64(reqs))
}
```

## Budget Management

### Budget Limits

```go
type BudgetManager struct {
    limit       float64
    currentCost float64
    mu          sync.Mutex
}

func (bm *BudgetManager) CanExecute() bool {
    bm.mu.Lock()
    defer bm.mu.Unlock()
    return bm.currentCost < bm.limit
}

func (bm *BudgetManager) Record(cost float64) error {
    bm.mu.Lock()
    defer bm.mu.Unlock()
    
    bm.currentCost += cost
    if bm.currentCost >= bm.limit {
        return fmt.Errorf("budget exceeded: $%.2f / $%.2f", bm.currentCost, bm.limit)
    }
    return nil
}

// Usage
budget := &BudgetManager{limit: 10.0}  // $10 limit

if !budget.CanExecute() {
    log.Fatal("Budget exceeded")
}

result, _ := pipe.Execute(ctx, "user", "Your message")
if err := budget.Record(result.Cost.TotalCost); err != nil {
    log.Printf("Warning: %v", err)
}
```

### Per-User Budgets

```go
type UserBudgets struct {
    limits map[string]float64
    costs  map[string]float64
    mu     sync.RWMutex
}

func (ub *UserBudgets) SetLimit(userID string, limit float64) {
    ub.mu.Lock()
    defer ub.mu.Unlock()
    ub.limits[userID] = limit
}

func (ub *UserBudgets) CanExecute(userID string) bool {
    ub.mu.RLock()
    defer ub.mu.RUnlock()
    
    limit, hasLimit := ub.limits[userID]
    if !hasLimit {
        return true  // No limit set
    }
    
    cost := ub.costs[userID]
    return cost < limit
}

func (ub *UserBudgets) Record(userID string, cost float64) error {
    ub.mu.Lock()
    defer ub.mu.Unlock()
    
    ub.costs[userID] += cost
    
    if limit, hasLimit := ub.limits[userID]; hasLimit {
        if ub.costs[userID] >= limit {
            return fmt.Errorf("user %s exceeded budget: $%.2f / $%.2f", 
                userID, ub.costs[userID], limit)
        }
    }
    
    return nil
}
```

## Cost Optimization

### Model Selection by Cost

```go
func selectCostEffectiveModel(complexity string) (string, string) {
    switch complexity {
    case "simple":
        // Cheapest: GPT-4o-mini
        return "gpt-4o-mini", "openai"
    case "medium":
        // Balanced: Claude Haiku
        return "claude-3-5-haiku-20241022", "claude"
    case "complex":
        // Best quality: GPT-4o
        return "gpt-4o", "openai"
    default:
        return "gpt-4o-mini", "openai"
    }
}

// Usage
model, providerType := selectCostEffectiveModel("simple")
provider := createProvider(providerType, model)
```

### Token Limit Optimization

```go
func estimateInputTokens(messages []types.Message) int {
    // Rough estimate: 4 chars per token
    total := 0
    for _, msg := range messages {
        total += len(msg.Content) / 4
    }
    return total
}

func optimizeMaxTokens(messages []types.Message, budget float64, pricePerToken float64) int {
    inputTokens := estimateInputTokens(messages)
    inputCost := float64(inputTokens) * pricePerToken
    
    // Remaining budget for output
    remainingBudget := budget - inputCost
    if remainingBudget <= 0 {
        return 100  // Minimum
    }
    
    // Calculate max output tokens
    maxTokens := int(remainingBudget / (pricePerToken * 3))  // Output costs ~3x more
    
    // Cap at reasonable limits
    if maxTokens > 2000 {
        maxTokens = 2000
    }
    if maxTokens < 100 {
        maxTokens = 100
    }
    
    return maxTokens
}
```

### Caching Responses

```go
import "crypto/sha256"

type ResponseCache struct {
    cache map[string]*pipeline.PipelineResult
    mu    sync.RWMutex
}

func (rc *ResponseCache) Key(prompt string) string {
    hash := sha256.Sum256([]byte(prompt))
    return fmt.Sprintf("%x", hash[:8])
}

func (rc *ResponseCache) Get(prompt string) (*pipeline.PipelineResult, bool) {
    rc.mu.RLock()
    defer rc.mu.RUnlock()
    
    result, exists := rc.cache[rc.Key(prompt)]
    return result, exists
}

func (rc *ResponseCache) Set(prompt string, result *pipeline.PipelineResult) {
    rc.mu.Lock()
    defer rc.mu.Unlock()
    
    rc.cache[rc.Key(prompt)] = result
}

// Usage
cache := &ResponseCache{cache: make(map[string]*pipeline.PipelineResult)}

prompt := "What is AI?"
if cached, exists := cache.Get(prompt); exists {
    log.Println("Using cached response (no cost)")
    return cached, nil
}

result, _ := pipe.Execute(ctx, "user", prompt)
cache.Set(prompt, result)
log.Printf("Fresh response cost: $%.6f", result.Cost.TotalCost)
```

## Reporting

### Daily Cost Summary

```go
type DailyCosts struct {
    costs map[string]float64  // date -> cost
    mu    sync.Mutex
}

func (dc *DailyCosts) Record(cost float64) {
    dc.mu.Lock()
    defer dc.mu.Unlock()
    
    date := time.Now().Format("2006-01-02")
    dc.costs[date] += cost
}

func (dc *DailyCosts) Report() {
    dc.mu.Lock()
    defer dc.mu.Unlock()
    
    var dates []string
    for date := range dc.costs {
        dates = append(dates, date)
    }
    sort.Strings(dates)
    
    log.Println("Daily Costs:")
    total := 0.0
    for _, date := range dates {
        cost := dc.costs[date]
        total += cost
        log.Printf("  %s: $%.2f", date, cost)
    }
    log.Printf("Total: $%.2f", total)
}
```

### Cost Alerts

```go
type CostAlerts struct {
    threshold float64
    current   float64
    lastAlert time.Time
    mu        sync.Mutex
}

func (ca *CostAlerts) Record(cost float64) {
    ca.mu.Lock()
    defer ca.mu.Unlock()
    
    ca.current += cost
    
    // Alert every hour if over threshold
    if ca.current >= ca.threshold && time.Since(ca.lastAlert) >= time.Hour {
        ca.sendAlert()
        ca.lastAlert = time.Now()
    }
}

func (ca *CostAlerts) sendAlert() {
    log.Printf("ALERT: Cost threshold exceeded: $%.2f / $%.2f", ca.current, ca.threshold)
    // Send email, Slack message, etc.
}
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    "sync"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)

type CostMonitor struct {
    totalCost   float64
    totalTokens int
    requests    int
    mu          sync.Mutex
}

func (cm *CostMonitor) Record(result *pipeline.PipelineResult) {
    cm.mu.Lock()
    defer cm.mu.Unlock()
    
    cm.totalCost += result.Cost.TotalCost
    cm.totalTokens += result.Response.Usage.TotalTokens
    cm.requests++
    
    log.Printf("Request cost: $%.6f (%d tokens)", 
        result.Cost.TotalCost, 
        result.Response.Usage.TotalTokens)
}

func (cm *CostMonitor) Report() {
    cm.mu.Lock()
    defer cm.mu.Unlock()
    
    log.Printf("\n=== Cost Summary ===")
    log.Printf("Total requests: %d", cm.requests)
    log.Printf("Total tokens: %d", cm.totalTokens)
    log.Printf("Total cost: $%.4f", cm.totalCost)
    log.Printf("Avg cost/request: $%.6f", cm.totalCost/float64(cm.requests))
    log.Printf("Avg tokens/request: %.1f", float64(cm.totalTokens)/float64(cm.requests))
}

func main() {
    monitor := &CostMonitor{}
    
    // Create provider
    provider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",  // Cost-effective model
        "",
        openai.DefaultProviderDefaults(),
        false,
    )
    defer provider.Close()
    
    // Build pipeline
    pipe := pipeline.NewPipeline(
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   500,  // Limit output tokens
            Temperature: 0.7,
        }),
    )
    defer pipe.Shutdown(context.Background())
    
    ctx := context.Background()
    
    // Execute requests
    prompts := []string{
        "What is AI?",
        "Explain machine learning",
        "What is deep learning?",
    }
    
    for _, prompt := range prompts {
        result, err := pipe.Execute(ctx, "user", prompt)
        if err != nil {
            log.Printf("Error: %v", err)
            continue
        }
        
        monitor.Record(result)
    }
    
    // Print summary
    monitor.Report()
}
```

## Troubleshooting

### Issue: Higher Costs Than Expected

**Problem**: Costs exceeding budget.

**Solutions**:

1. Check model pricing:
   ```go
   // GPT-4o is expensive, use gpt-4o-mini
   provider := openai.NewOpenAIProvider("openai", "gpt-4o-mini", ...)
   ```

2. Limit max tokens:
   ```go
   config := &middleware.ProviderMiddlewareConfig{
       MaxTokens: 500,  // Reduce from default
   }
   ```

3. Trim conversation history:
   ```go
   if len(messages) > 10 {
       messages = messages[len(messages)-10:]
   }
   ```

### Issue: Cost Tracking Inaccurate

**Problem**: Reported costs don't match bills.

**Solutions**:

1. Verify pricing is current:
   ```go
   // Check provider's pricing page
   // Update if prices changed
   ```

2. Include all cost components:
   ```go
   total := result.Cost.InputCost + result.Cost.OutputCost
   // Some providers may have additional fees
   ```

3. Check for tool costs:
   ```go
   // Tool calls may incur additional token costs
   ```

## Best Practices

1. **Always monitor costs**:
   ```go
   log.Printf("Cost: $%.6f", result.Cost.TotalCost)
   ```

2. **Set budget limits**:
   ```go
   budget := &BudgetManager{limit: 10.0}
   ```

3. **Use cost-effective models**:
   ```go
   // Simple tasks: gpt-4o-mini
   // Complex tasks: gpt-4o only when needed
   ```

4. **Limit max tokens**:
   ```go
   config.MaxTokens = 500  // Reasonable default
   ```

5. **Cache repeated requests**:
   ```go
   if cached, exists := cache.Get(prompt); exists {
       return cached, nil
   }
   ```

6. **Track per-user costs**:
   ```go
   budgets.Record(userID, result.Cost.TotalCost)
   ```

7. **Set up alerts**:
   ```go
   if totalCost > threshold {
       sendAlert()
   }
   ```

8. **Generate regular reports**:
   ```go
   go func() {
       ticker := time.NewTicker(24 * time.Hour)
       for range ticker.C {
           monitor.Report()
       }
   }()
   ```

## Next Steps

- [Setup Providers](setup-providers) - Model selection
- [Configure Pipeline](configure-pipeline) - Token limits
- [Handle Errors](handle-errors) - Cost-aware retries

## See Also

- [Providers Reference](../reference/providers) - Pricing tables
- [Pipeline Reference](../reference/pipeline) - Cost structures
