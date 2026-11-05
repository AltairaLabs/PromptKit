---
title: Monitor Costs
sidebar:
  order: 7
---
Track and optimize LLM API costs.

## Goal

Monitor token usage and costs for budget control.

## Quick Start

```go
// ask runs one user message through the pipeline and returns the result.
func ask(ctx context.Context, pipe *stage.StreamPipeline, text string) (*stage.ExecutionResult, error) {
    msg := types.Message{Role: "user"}
    msg.AddTextPart(text)
    return pipe.ExecuteSync(ctx, stage.NewMessageElement(&msg))
}

result, err := ask(ctx, pipe, "Your message")
if err != nil {
    log.Fatal(err)
}

// Check cost
log.Printf("Tokens: %d", result.CostInfo.InputTokens+result.CostInfo.OutputTokens)
log.Printf("Cost: $%.6f", result.CostInfo.TotalCost)
```

## Cost Tracking

### Basic Cost Info

```go
result, _ := ask(ctx, pipe, "Explain AI")

// Cost breakdown (token counts and costs are on CostInfo)
cost := result.CostInfo
log.Printf("Input tokens: %d", cost.InputTokens)
log.Printf("Output tokens: %d", cost.OutputTokens)
log.Printf("Total tokens: %d", cost.InputTokens+cost.OutputTokens)
log.Printf("Input cost: $%.6f", cost.InputCostUSD)
log.Printf("Output cost: $%.6f", cost.OutputCostUSD)
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

func (sc *SessionCosts) Record(sessionID string, result *stage.ExecutionResult) {
    sc.mu.Lock()
    defer sc.mu.Unlock()
    
    sc.costs[sessionID] += result.CostInfo.TotalCost
    sc.tokens[sessionID] += result.CostInfo.InputTokens+result.CostInfo.OutputTokens
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

result, _ := ask(ctx, pipe, "Hello")
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

func (ct *CostTracker) Record(result *stage.ExecutionResult) {
    costCents := int64(result.CostInfo.TotalCost * 100)
    atomic.AddInt64(&ct.totalCost, costCents)
    atomic.AddInt64(&ct.totalTokens, int64(result.CostInfo.InputTokens+result.CostInfo.OutputTokens))
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

result, _ := ask(ctx, pipe, "Your message")
if err := budget.Record(result.CostInfo.TotalCost); err != nil {
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
    cache map[string]*stage.ExecutionResult
    mu    sync.RWMutex
}

func (rc *ResponseCache) Key(prompt string) string {
    hash := sha256.Sum256([]byte(prompt))
    return fmt.Sprintf("%x", hash[:8])
}

func (rc *ResponseCache) Get(prompt string) (*stage.ExecutionResult, bool) {
    rc.mu.RLock()
    defer rc.mu.RUnlock()
    
    result, exists := rc.cache[rc.Key(prompt)]
    return result, exists
}

func (rc *ResponseCache) Set(prompt string, result *stage.ExecutionResult) {
    rc.mu.Lock()
    defer rc.mu.Unlock()
    
    rc.cache[rc.Key(prompt)] = result
}

// Usage
cache := &ResponseCache{cache: make(map[string]*stage.ExecutionResult)}

prompt := "What is AI?"
if cached, exists := cache.Get(prompt); exists {
    log.Println("Using cached response (no cost)")
    return cached, nil
}

result, _ := ask(ctx, pipe, prompt)
cache.Set(prompt, result)
log.Printf("Fresh response cost: $%.6f", result.CostInfo.TotalCost)
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
    
    "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
    "github.com/AltairaLabs/PromptKit/runtime/v2/providers"
    "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

type CostMonitor struct {
    totalCost   float64
    totalTokens int
    requests    int
    mu          sync.Mutex
}

func (cm *CostMonitor) Record(result *stage.ExecutionResult) {
    cm.mu.Lock()
    defer cm.mu.Unlock()
    
    cm.totalCost += result.CostInfo.TotalCost
    cm.totalTokens += result.CostInfo.InputTokens+result.CostInfo.OutputTokens
    cm.requests++
    
    log.Printf("Request cost: $%.6f (%d tokens)", 
        result.CostInfo.TotalCost, 
        result.CostInfo.InputTokens+result.CostInfo.OutputTokens)
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

func ask(ctx context.Context, pipe *stage.StreamPipeline, text string) (*stage.ExecutionResult, error) {
    msg := types.Message{Role: "user"}
    msg.AddTextPart(text)
    return pipe.ExecuteSync(ctx, stage.NewMessageElement(&msg))
}

func main() {
    monitor := &CostMonitor{}
    
    // Create provider
    provider := openai.NewProvider(
        "openai",
        "gpt-4o-mini",  // Cost-effective model
        "",
        providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
        false,
    )
    defer provider.Close()

    // Build pipeline
    pipe, err := stage.NewPipelineBuilder().
        Chain(stage.NewProviderStage(provider, nil, nil, &stage.ProviderConfig{})).
        Build()
    if err != nil {
        log.Fatal(err)
    }
    defer pipe.Shutdown(context.Background())
    
    ctx := context.Background()
    
    // Execute requests
    prompts := []string{
        "What is AI?",
        "Explain machine learning",
        "What is deep learning?",
    }
    
    for _, prompt := range prompts {
        result, err := ask(ctx, pipe, prompt)
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
   provider := openai.NewProvider("openai", "gpt-4o-mini", ...)
   ```

2. Limit max tokens:
   ```go
   // Set lower MaxTokens in ProviderDefaults
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
   total := result.CostInfo.InputCostUSD + result.CostInfo.OutputCostUSD
   // Some providers may have additional fees
   ```

3. Check for tool costs:
   ```go
   // Tool calls may incur additional token costs
   ```

## Best Practices

1. **Always monitor costs**:
   ```go
   log.Printf("Cost: $%.6f", result.CostInfo.TotalCost)
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
   budgets.Record(userID, result.CostInfo.TotalCost)
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

- [Setup Providers](/runtime/how-to/providers/setup-providers/) - Model selection
- [Configure Pipeline](/runtime/how-to/pipeline/configure-pipeline/) - Token limits
- [Handle Errors](/runtime/how-to/pipeline/handle-errors/) - Cost-aware retries

## See Also

- [Providers Reference](/runtime/reference/providers/) - Pricing tables
- [Pipeline Reference](/runtime/reference/pipeline/) - Cost structures
