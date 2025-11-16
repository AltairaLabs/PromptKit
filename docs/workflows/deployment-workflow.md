---
layout: default
title: Deployment Workflow
parent: Workflows
nav_order: 3
---

# Deployment Workflow

Deploy an LLM application to production using PromptKit.

## Overview

This workflow covers packaging, configuration, deployment, monitoring, and rollback strategies.

**Time required**: 60 minutes

**What you'll deploy**: Customer support chatbot to production

## Prerequisites

- Completed [Development Workflow](development-workflow.md)
- Docker installed
- Kubernetes cluster (or similar)
- Redis instance

## Step 1: Production Configuration

Create `config/production.yaml`:

```yaml
app:
  name: support-bot
  version: 1.0.0
  environment: production

provider:
  type: openai
  model: gpt-4o
  api_key_env: OPENAI_API_KEY
  config:
    max_tokens: 500
    temperature: 0.7
    timeout: 30s

state:
  type: redis
  url: redis://redis:6379
  password_env: REDIS_PASSWORD
  db: 0
  pool_size: 10
  ttl: 24h
  max_messages: 20

middleware:
  state:
    enabled: true
    max_messages: 20
  
  template:
    enabled: true
    source: support.pack
    default: support
  
  validator:
    enabled: true
    banned_words:
      - hack
      - crack
      - pirate
    max_length: 2000
  
  provider:
    enabled: true
    rate_limit:
      requests_per_minute: 60
      burst: 10
    retry:
      max_attempts: 3
      backoff: exponential
    cost_tracking: true

monitoring:
  metrics:
    enabled: true
    port: 9090
    path: /metrics
  
  logging:
    level: info
    format: json
    output: stdout
  
  tracing:
    enabled: true
    endpoint: http://jaeger:14268/api/traces

alerts:
  error_rate_threshold: 0.05
  latency_p99_threshold: 5000ms
  cost_per_hour_threshold: 10.00
```

## Step 2: Containerization

Create `Dockerfile`:

```dockerfile
# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o support-bot .

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary and assets
COPY --from=builder /app/support-bot .
COPY support.pack .
COPY config/ ./config/

# Create non-root user
RUN adduser -D -u 1000 appuser
USER appuser

# Expose metrics port
EXPOSE 9090

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:9090/health || exit 1

CMD ["./support-bot"]
```

Build image:

```bash
docker build -t support-bot:1.0.0 .
```

Test locally:

```bash
docker run -p 8080:8080 -p 9090:9090 \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  -e REDIS_PASSWORD=$REDIS_PASSWORD \
  support-bot:1.0.0
```

## Step 3: Kubernetes Deployment

Create `k8s/deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: support-bot
  labels:
    app: support-bot
spec:
  replicas: 3
  selector:
    matchLabels:
      app: support-bot
  template:
    metadata:
      labels:
        app: support-bot
    spec:
      containers:
      - name: support-bot
        image: support-bot:1.0.0
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 9090
          name: metrics
        env:
        - name: OPENAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: support-bot-secrets
              key: openai-api-key
        - name: REDIS_PASSWORD
          valueFrom:
            secretKeyRef:
              name: support-bot-secrets
              key: redis-password
        - name: CONFIG_PATH
          value: /app/config/production.yaml
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 9090
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /ready
            port: 9090
          initialDelaySeconds: 5
          periodSeconds: 10
        volumeMounts:
        - name: config
          mountPath: /app/config
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: support-bot-config
---
apiVersion: v1
kind: Service
metadata:
  name: support-bot
spec:
  selector:
    app: support-bot
  ports:
  - name: http
    port: 80
    targetPort: 8080
  - name: metrics
    port: 9090
    targetPort: 9090
  type: LoadBalancer
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: support-bot-config
data:
  production.yaml: |
    # Include production.yaml content here
---
apiVersion: v1
kind: Secret
metadata:
  name: support-bot-secrets
type: Opaque
data:
  openai-api-key: <base64-encoded-key>
  redis-password: <base64-encoded-password>
```

Deploy:

```bash
kubectl apply -f k8s/deployment.yaml
```

## Step 4: Monitoring Setup

### Prometheus Metrics

Add to `main.go`:

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
    requestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "support_bot_requests_total",
            Help: "Total number of requests",
        },
        []string{"status"},
    )
    
    requestDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "support_bot_request_duration_seconds",
            Help:    "Request duration in seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"endpoint"},
    )
    
    llmCost = promauto.NewCounter(
        prometheus.CounterOpts{
            Name: "support_bot_llm_cost_usd",
            Help: "Total LLM cost in USD",
        },
    )
)

func main() {
    // ... setup code

    // Metrics endpoint
    http.Handle("/metrics", promhttp.Handler())
    go http.ListenAndServe(":9090", nil)
    
    // ... rest of code
}

// Track metrics in middleware
func trackMetrics(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        
        next.ServeHTTP(w, r)
        
        duration := time.Since(start).Seconds()
        requestDuration.WithLabelValues(r.URL.Path).Observe(duration)
        requestsTotal.WithLabelValues("success").Inc()
    })
}
```

### Grafana Dashboard

Create `monitoring/dashboard.json`:

```json
{
  "dashboard": {
    "title": "Support Bot",
    "panels": [
      {
        "title": "Request Rate",
        "targets": [
          {
            "expr": "rate(support_bot_requests_total[5m])"
          }
        ]
      },
      {
        "title": "Response Time P99",
        "targets": [
          {
            "expr": "histogram_quantile(0.99, support_bot_request_duration_seconds)"
          }
        ]
      },
      {
        "title": "LLM Cost",
        "targets": [
          {
            "expr": "rate(support_bot_llm_cost_usd[1h]) * 3600"
          }
        ]
      },
      {
        "title": "Error Rate",
        "targets": [
          {
            "expr": "rate(support_bot_requests_total{status=\"error\"}[5m])"
          }
        ]
      }
    ]
  }
}
```

## Step 5: Logging

### Structured Logging

```go
import (
    "go.uber.org/zap"
)

func setupLogger() *zap.Logger {
    config := zap.NewProductionConfig()
    config.OutputPaths = []string{"stdout"}
    config.ErrorOutputPaths = []string{"stderr"}
    
    logger, _ := config.Build()
    return logger
}

func handleRequest(logger *zap.Logger, pipe *pipeline.Pipeline) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        
        logger.Info("request received",
            zap.String("method", r.Method),
            zap.String("path", r.URL.Path),
            zap.String("user_id", getUserID(r)),
        )
        
        result, err := pipe.Execute(r.Context(), "user", getMessage(r))
        
        duration := time.Since(start)
        
        if err != nil {
            logger.Error("request failed",
                zap.Error(err),
                zap.Duration("duration", duration),
                zap.String("user_id", getUserID(r)),
            )
            http.Error(w, "Internal error", 500)
            return
        }
        
        logger.Info("request completed",
            zap.Duration("duration", duration),
            zap.Int("input_tokens", result.Response.Usage.InputTokens),
            zap.Int("output_tokens", result.Response.Usage.OutputTokens),
            zap.Float64("cost", result.Response.Cost),
            zap.String("user_id", getUserID(r)),
        )
        
        json.NewEncoder(w).Encode(result.Response)
    }
}
```

## Step 6: Health Checks

Implement health and readiness endpoints:

```go
func healthHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{
        "status": "healthy",
    })
}

func readinessHandler(store statestore.StateStore, provider types.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Check state store
        _, err := store.Load("health-check")
        if err != nil {
            w.WriteHeader(http.StatusServiceUnavailable)
            json.NewEncoder(w).Encode(map[string]string{
                "status": "not ready",
                "reason": "state store unavailable",
            })
            return
        }
        
        // Check provider (optional quick test)
        ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
        defer cancel()
        
        _, err = provider.Complete(ctx, []types.Message{
            {Role: "user", Content: "test"},
        }, &types.ProviderConfig{MaxTokens: 5})
        
        if err != nil {
            w.WriteHeader(http.StatusServiceUnavailable)
            json.NewEncoder(w).Encode(map[string]string{
                "status": "not ready",
                "reason": "provider unavailable",
            })
            return
        }
        
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{
            "status": "ready",
        })
    }
}
```

## Step 7: Graceful Shutdown

Handle shutdown signals properly:

```go
func main() {
    // ... setup code
    
    server := &http.Server{
        Addr:    ":8080",
        Handler: router,
    }
    
    // Start server
    go func() {
        if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Fatal("server error", zap.Error(err))
        }
    }()
    
    // Wait for interrupt signal
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    
    logger.Info("shutting down server...")
    
    // Graceful shutdown with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    if err := server.Shutdown(ctx); err != nil {
        logger.Fatal("server forced to shutdown", zap.Error(err))
    }
    
    // Cleanup resources
    provider.Close()
    logger.Info("server exited")
}
```

## Step 8: Deployment Strategy

### Blue-Green Deployment

```yaml
# Deploy new version (green)
kubectl apply -f k8s/deployment-v2.yaml

# Test green deployment
kubectl port-forward svc/support-bot-v2 8080:80

# Switch traffic
kubectl patch service support-bot -p '{"spec":{"selector":{"version":"v2"}}}'

# Monitor for issues
# If problems, rollback:
kubectl patch service support-bot -p '{"spec":{"selector":{"version":"v1"}}}'

# Clean up old version
kubectl delete deployment support-bot-v1
```

### Canary Deployment

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: support-bot
spec:
  hosts:
  - support-bot
  http:
  - match:
    - headers:
        user-type:
          exact: beta
    route:
    - destination:
        host: support-bot
        subset: v2
  - route:
    - destination:
        host: support-bot
        subset: v1
      weight: 90
    - destination:
        host: support-bot
        subset: v2
      weight: 10
```

## Step 9: Rollback Procedure

Create rollback script `scripts/rollback.sh`:

```bash
#!/bin/bash

set -e

VERSION=$1

if [ -z "$VERSION" ]; then
    echo "Usage: ./rollback.sh <version>"
    exit 1
fi

echo "Rolling back to version $VERSION..."

# Update deployment
kubectl set image deployment/support-bot \
    support-bot=support-bot:$VERSION

# Wait for rollout
kubectl rollout status deployment/support-bot

# Verify health
kubectl run test-health --rm -i --restart=Never --image=curlimages/curl -- \
    curl -f http://support-bot/health

echo "Rollback complete!"
```

## Step 10: Monitoring Alerts

Create `monitoring/alerts.yaml`:

```yaml
groups:
- name: support-bot
  interval: 30s
  rules:
  - alert: HighErrorRate
    expr: rate(support_bot_requests_total{status="error"}[5m]) > 0.05
    for: 5m
    annotations:
      summary: "High error rate detected"
      description: "Error rate is {{ $value }} (threshold: 0.05)"
  
  - alert: HighLatency
    expr: histogram_quantile(0.99, support_bot_request_duration_seconds) > 5
    for: 5m
    annotations:
      summary: "High latency detected"
      description: "P99 latency is {{ $value }}s (threshold: 5s)"
  
  - alert: HighCost
    expr: rate(support_bot_llm_cost_usd[1h]) * 3600 > 10
    for: 10m
    annotations:
      summary: "High LLM cost detected"
      description: "Hourly cost is ${{ $value }} (threshold: $10)"
  
  - alert: LowAvailability
    expr: up{job="support-bot"} < 1
    for: 1m
    annotations:
      summary: "Service unavailable"
      description: "Support bot is down"
```

## Deployment Checklist

### Pre-Deployment

- [ ] All tests passing (unit, integration, evaluation)
- [ ] Performance benchmarks meet targets
- [ ] Cost estimates within budget
- [ ] Security scan completed
- [ ] Configuration reviewed
- [ ] Rollback plan documented
- [ ] Monitoring dashboards ready
- [ ] Alerts configured

### During Deployment

- [ ] Deploy to staging first
- [ ] Run smoke tests
- [ ] Check metrics and logs
- [ ] Gradually increase traffic
- [ ] Monitor error rates
- [ ] Watch costs

### Post-Deployment

- [ ] Verify all endpoints healthy
- [ ] Check dashboard metrics
- [ ] Review initial logs
- [ ] Test critical paths
- [ ] Monitor for 24 hours
- [ ] Document any issues

## Best Practices

### Configuration

✅ Use environment variables for secrets  
✅ Separate config per environment  
✅ Version configuration with code  
✅ Validate configuration on startup  

### Monitoring

✅ Track key metrics (latency, errors, cost)  
✅ Set up alerts for anomalies  
✅ Log structured data  
✅ Use distributed tracing  

### Reliability

✅ Implement health checks  
✅ Handle graceful shutdown  
✅ Add retry logic  
✅ Use circuit breakers  
✅ Set resource limits  

### Security

✅ Don't log sensitive data  
✅ Use secrets management  
✅ Rotate API keys regularly  
✅ Scan for vulnerabilities  
✅ Use least privilege access  

## Troubleshooting

### High Error Rate

```bash
# Check logs
kubectl logs -l app=support-bot --tail=100

# Check provider status
curl https://status.openai.com

# Check Redis
kubectl exec -it redis-0 -- redis-cli ping

# Rollback if needed
./scripts/rollback.sh 0.9.0
```

### High Latency

```bash
# Check resource usage
kubectl top pods -l app=support-bot

# Check provider latency
# Review metrics dashboard

# Scale if needed
kubectl scale deployment support-bot --replicas=5
```

### High Cost

```bash
# Check cost metrics
curl http://support-bot:9090/metrics | grep cost

# Review recent prompts
kubectl logs -l app=support-bot | grep input_tokens

# Adjust max_tokens if needed
kubectl edit configmap support-bot-config
kubectl rollout restart deployment support-bot
```

## Summary

Production deployment workflow:

1. **Configure** - Production settings
2. **Containerize** - Docker image
3. **Deploy** - Kubernetes
4. **Monitor** - Metrics and logs
5. **Alert** - Anomaly detection
6. **Test** - Verify in production
7. **Rollback** - If issues arise

## Next Steps

- **Build full-stack app**: [Full-Stack Example](full-stack-example.md)
- **Optimize costs**: [Monitor Costs](../runtime/how-to/monitor-costs.md)
- **Add observability**: [Runtime Monitoring](../runtime/tutorials/05-production-deployment.md)

## Related Documentation

- [Production Deployment Tutorial](../runtime/tutorials/05-production-deployment.md)
- [Error Handling Guide](../runtime/how-to/handle-errors.md)
- [Provider Configuration](../runtime/reference/providers.md)
