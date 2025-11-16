---
layout: default
title: "Tutorial 5: Custom Pipelines"
nav_order: 5
parent: SDK Tutorials
grand_parent: SDK
---

# Tutorial 5: Custom Pipelines

Learn how to build custom processing pipelines with middleware and validation.

## What You'll Learn

- Use PipelineBuilder for custom pipelines
- Add middleware components
- Implement custom validation
- Build advanced processing logic

## Why Custom Pipelines?

Custom pipelines give you full control:

- Add custom middleware
- Implement complex validation
- Integrate with existing systems
- Fine-tune processing behavior

## Prerequisites

Complete previous tutorials and understand SDK basics.

## Step 1: Basic Pipeline

Start with a simple custom pipeline:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
)

func main() {
    ctx := context.Background()

    // Create provider
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)

    // Build custom pipeline
    pipe := sdk.NewPipelineBuilder().
        WithProvider(provider).
        WithSystemPrompt("You are a helpful assistant.").
        Build()

    // Create request
    request := &pipeline.Request{
        UserMessage: "Hello, how are you?",
    }

    // Execute pipeline
    response, err := pipe.Execute(ctx, request)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Assistant:", response.Content)
}
```

## Step 2: Adding Middleware

Add custom middleware to the pipeline:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "strings"
    "time"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
)

// Logging middleware
func loggingMiddleware(next pipeline.Handler) pipeline.Handler {
    return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
        start := time.Now()
        
        log.Printf("Request: %s", truncate(req.UserMessage, 50))
        
        resp, err := next.Handle(ctx, req)
        
        duration := time.Since(start)
        if err != nil {
            log.Printf("Error after %v: %v", duration, err)
        } else {
            log.Printf("Response after %v: %s", duration, truncate(resp.Content, 50))
        }
        
        return resp, err
    })
}

// Profanity filter middleware
func profanityFilterMiddleware(next pipeline.Handler) pipeline.Handler {
    return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
        // Check input
        if containsProfanity(req.UserMessage) {
            return nil, fmt.Errorf("message contains inappropriate content")
        }
        
        resp, err := next.Handle(ctx, req)
        if err != nil {
            return nil, err
        }
        
        // Check output
        if containsProfanity(resp.Content) {
            resp.Content = "[Content filtered]"
        }
        
        return resp, nil
    })
}

// Rate limiting middleware
func rateLimitMiddleware(next pipeline.Handler) pipeline.Handler {
    limiter := rate.NewLimiter(rate.Limit(10), 10) // 10 req/sec
    
    return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
        if !limiter.Allow() {
            return nil, fmt.Errorf("rate limit exceeded")
        }
        
        return next.Handle(ctx, req)
    })
}

func main() {
    ctx := context.Background()

    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)

    // Build pipeline with middleware
    pipe := sdk.NewPipelineBuilder().
        WithProvider(provider).
        WithSystemPrompt("You are a helpful assistant.").
        Use(loggingMiddleware).
        Use(profanityFilterMiddleware).
        Use(rateLimitMiddleware).
        Build()

    request := &pipeline.Request{
        UserMessage: "Hello, how are you?",
    }

    response, err := pipe.Execute(ctx, request)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Assistant:", response.Content)
}

func containsProfanity(text string) bool {
    // Implement profanity check
    badWords := []string{"badword1", "badword2"}
    lower := strings.ToLower(text)
    
    for _, word := range badWords {
        if strings.Contains(lower, word) {
            return true
        }
    }
    
    return false
}

func truncate(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-3] + "..."
}
```

## Step 3: Custom Validators

Add validation to your pipeline:

```go
import "github.com/AltairaLabs/PromptKit/runtime/validators"

// Custom length validator
func lengthValidator(minLen, maxLen int) validators.Validator {
    return validators.ValidatorFunc(func(ctx context.Context, req *pipeline.Request) error {
        msgLen := len(req.UserMessage)
        
        if msgLen < minLen {
            return fmt.Errorf("message too short (min %d chars)", minLen)
        }
        if msgLen > maxLen {
            return fmt.Errorf("message too long (max %d chars)", maxLen)
        }
        
        return nil
    })
}

// Language validator
func languageValidator(allowedLangs []string) validators.Validator {
    return validators.ValidatorFunc(func(ctx context.Context, req *pipeline.Request) error {
        detected := detectLanguage(req.UserMessage)
        
        for _, lang := range allowedLangs {
            if detected == lang {
                return nil
            }
        }
        
        return fmt.Errorf("unsupported language: %s", detected)
    })
}

// Use validators in pipeline
pipe := sdk.NewPipelineBuilder().
    WithProvider(provider).
    WithValidator(lengthValidator(10, 1000)).
    WithValidator(languageValidator([]string{"en", "es", "fr"})).
    Build()
```

## Step 4: Context Injection

Pass custom context through the pipeline:

```go
type contextKey string

const (
    userIDKey     contextKey = "user_id"
    requestIDKey  contextKey = "request_id"
    metadataKey   contextKey = "metadata"
)

// Context injection middleware
func contextInjectionMiddleware(userID, requestID string) pipeline.Middleware {
    return func(next pipeline.Handler) pipeline.Handler {
        return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
            // Inject values into context
            ctx = context.WithValue(ctx, userIDKey, userID)
            ctx = context.WithValue(ctx, requestIDKey, requestID)
            ctx = context.WithValue(ctx, metadataKey, map[string]interface{}{
                "timestamp": time.Now(),
                "source":    "api",
            })
            
            return next.Handle(ctx, req)
        })
    }
}

// Middleware that uses context
func auditMiddleware(next pipeline.Handler) pipeline.Handler {
    return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
        userID := ctx.Value(userIDKey).(string)
        requestID := ctx.Value(requestIDKey).(string)
        
        log.Printf("Audit: user=%s request=%s message=%s",
            userID, requestID, truncate(req.UserMessage, 30))
        
        return next.Handle(ctx, req)
    })
}

// Use in pipeline
userID := "user123"
requestID := generateRequestID()

pipe := sdk.NewPipelineBuilder().
    WithProvider(provider).
    Use(contextInjectionMiddleware(userID, requestID)).
    Use(auditMiddleware).
    Build()
```

## Step 5: Error Handling

Implement robust error handling:

```go
// Error recovery middleware
func errorRecoveryMiddleware(next pipeline.Handler) pipeline.Handler {
    return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("Recovered from panic: %v", r)
            }
        }()
        
        resp, err := next.Handle(ctx, req)
        if err != nil {
            // Log error details
            log.Printf("Pipeline error: %v", err)
            
            // Check if retryable
            if isRetryable(err) {
                log.Println("Error is retryable")
            }
            
            // Transform error for user
            err = transformError(err)
        }
        
        return resp, err
    })
}

// Retry middleware
func retryMiddleware(maxRetries int) pipeline.Middleware {
    return func(next pipeline.Handler) pipeline.Handler {
        return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
            var lastErr error
            
            for attempt := 0; attempt <= maxRetries; attempt++ {
                if attempt > 0 {
                    // Exponential backoff
                    backoff := time.Duration(attempt) * time.Second
                    time.Sleep(backoff)
                    
                    log.Printf("Retry attempt %d/%d", attempt, maxRetries)
                }
                
                resp, err := next.Handle(ctx, req)
                if err == nil {
                    return resp, nil
                }
                
                lastErr = err
                
                // Don't retry non-retryable errors
                if !isRetryable(err) {
                    break
                }
            }
            
            return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
        })
    }
}

func isRetryable(err error) bool {
    // Check if error is retryable
    if strings.Contains(err.Error(), "rate limit") {
        return true
    }
    if strings.Contains(err.Error(), "timeout") {
        return true
    }
    if strings.Contains(err.Error(), "503") {
        return true
    }
    return false
}

func transformError(err error) error {
    // User-friendly error messages
    switch {
    case strings.Contains(err.Error(), "rate limit"):
        return fmt.Errorf("service is busy, please try again")
    case strings.Contains(err.Error(), "context length"):
        return fmt.Errorf("message is too long")
    case strings.Contains(err.Error(), "authentication"):
        return fmt.Errorf("authentication failed")
    default:
        return fmt.Errorf("request failed")
    }
}
```

## Complete Example: Production Pipeline

Here's a production-ready pipeline with all features:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/validators"
)

type PipelineService struct {
    pipeline *pipeline.Pipeline
}

func NewPipelineService() (*PipelineService, error) {
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)

    // Build production pipeline
    pipe := sdk.NewPipelineBuilder().
        WithProvider(provider).
        WithSystemPrompt("You are a helpful, safe, and accurate assistant.").
        // Validation
        WithValidator(lengthValidator(1, 2000)).
        WithValidator(validators.ProfanityValidator()).
        // Middleware stack
        Use(requestIDMiddleware).
        Use(loggingMiddleware).
        Use(rateLimitMiddleware).
        Use(errorRecoveryMiddleware).
        Use(retryMiddleware(3)).
        Use(metricsMiddleware).
        Use(auditMiddleware).
        Build()

    return &PipelineService{pipeline: pipe}, nil
}

func (s *PipelineService) Process(ctx context.Context, userID, message string) (string, error) {
    // Add timeout
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    // Inject user context
    ctx = context.WithValue(ctx, userIDKey, userID)
    ctx = context.WithValue(ctx, requestIDKey, generateRequestID())

    // Create request
    request := &pipeline.Request{
        UserMessage: message,
        Metadata: map[string]interface{}{
            "timestamp": time.Now(),
            "source":    "api",
        },
    }

    // Execute pipeline
    response, err := s.pipeline.Execute(ctx, request)
    if err != nil {
        return "", err
    }

    return response.Content, nil
}

// Middleware implementations

func requestIDMiddleware(next pipeline.Handler) pipeline.Handler {
    return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
        if ctx.Value(requestIDKey) == nil {
            ctx = context.WithValue(ctx, requestIDKey, generateRequestID())
        }
        return next.Handle(ctx, req)
    })
}

func loggingMiddleware(next pipeline.Handler) pipeline.Handler {
    return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
        requestID := ctx.Value(requestIDKey).(string)
        start := time.Now()

        log.Printf("[%s] Request: %s", requestID, truncate(req.UserMessage, 50))

        resp, err := next.Handle(ctx, req)

        duration := time.Since(start)
        if err != nil {
            log.Printf("[%s] Error after %v: %v", requestID, duration, err)
        } else {
            log.Printf("[%s] Success after %v", requestID, duration)
        }

        return resp, err
    })
}

func metricsMiddleware(next pipeline.Handler) pipeline.Handler {
    return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
        start := time.Now()

        resp, err := next.Handle(ctx, req)

        duration := time.Since(start)

        // Record metrics
        recordMetric("pipeline.duration", duration)
        if err != nil {
            recordMetric("pipeline.errors", 1)
        } else {
            recordMetric("pipeline.success", 1)
            if resp != nil {
                recordMetric("pipeline.tokens", resp.TokensUsed)
                recordMetric("pipeline.cost", resp.Cost)
            }
        }

        return resp, err
    })
}

func auditMiddleware(next pipeline.Handler) pipeline.Handler {
    return pipeline.HandlerFunc(func(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
        userID := ctx.Value(userIDKey).(string)
        requestID := ctx.Value(requestIDKey).(string)

        // Audit log
        auditLog := map[string]interface{}{
            "request_id": requestID,
            "user_id":    userID,
            "timestamp":  time.Now(),
            "message":    truncate(req.UserMessage, 100),
        }

        resp, err := next.Handle(ctx, req)

        if err != nil {
            auditLog["error"] = err.Error()
        } else if resp != nil {
            auditLog["tokens"] = resp.TokensUsed
            auditLog["cost"] = resp.Cost
        }

        // Write to audit log (database, file, etc.)
        writeAuditLog(auditLog)

        return resp, err
    })
}

// Helper functions

func generateRequestID() string {
    return fmt.Sprintf("req-%d", time.Now().UnixNano())
}

func recordMetric(name string, value interface{}) {
    // Send to metrics system (Prometheus, Datadog, etc.)
    log.Printf("Metric: %s = %v", name, value)
}

func writeAuditLog(log map[string]interface{}) {
    // Write to audit system
    // In production: database, S3, etc.
}

func truncate(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-3] + "..."
}

func lengthValidator(min, max int) validators.Validator {
    return validators.ValidatorFunc(func(ctx context.Context, req *pipeline.Request) error {
        length := len(req.UserMessage)
        if length < min || length > max {
            return fmt.Errorf("message length must be between %d and %d", min, max)
        }
        return nil
    })
}

func main() {
    service, err := NewPipelineService()
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // Process messages
    messages := []string{
        "Hello, how are you?",
        "Tell me about the weather",
        "What's 2+2?",
    }

    for _, msg := range messages {
        fmt.Printf("\nUser: %s\n", msg)

        response, err := service.Process(ctx, "user123", msg)
        if err != nil {
            fmt.Printf("Error: %v\n", err)
            continue
        }

        fmt.Printf("Assistant: %s\n", response)
    }
}
```

## What You've Learned

✅ Build custom pipelines with PipelineBuilder  
✅ Add middleware for logging, filtering, rate limiting  
✅ Implement custom validators  
✅ Inject and use context  
✅ Handle errors and retries  
✅ Build production-ready processing pipelines  

## Congratulations!

You've completed all SDK tutorials! You now know how to:

1. ✅ Create conversations with the SDK
2. ✅ Implement streaming responses
3. ✅ Add tool integration
4. ✅ Manage persistent state
5. ✅ Build custom pipelines

## Next Steps

- Explore [SDK Examples](../../../examples/)
- Read [SDK Reference Documentation](../reference/)
- Check [SDK Explanation Docs](../explanation/)
- Build your own LLM application!

## Further Reading

- [PipelineBuilder Reference](../reference/pipeline-builder.md)
- [Middleware Guide](../explanation/middleware-architecture.md)
- [Validators Reference](../reference/validators.md)
