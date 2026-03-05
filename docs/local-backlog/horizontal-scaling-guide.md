# Horizontal Scaling and Resource Planning Guide

This guide covers how to deploy PromptKit across multiple instances, extract state to shared stores, and size resources for different workloads. It references the scalability work completed in the `feat/scalability-*` branches (see `docs/local-backlog/05-mar-scalability-review.md` for the full audit).

---

## 1. Extracting State to External Stores

### The Problem

By default, `sdk.Open()` creates a per-conversation `MemoryStore` (`runtime/statestore/memory.go`). This store is process-local: if a user's next request lands on a different pod, the conversation state is gone and `Resume()` returns `ErrConversationNotFound`.

### Solution: Wire a RedisStore

PromptKit ships a production-ready Redis-backed state store at `runtime/statestore/redis.go`. It implements the full set of interfaces:

- `Store` (Load/Save/Fork)
- `MessageReader` (LoadRecentMessages/MessageCount)
- `MessageAppender` (AppendMessages)
- `MetadataAccessor` (LoadMetadata)
- `SummaryAccessor` (LoadSummaries/SaveSummary)

These interfaces are defined in `runtime/statestore/interface.go`.

#### Basic Setup

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
    "github.com/AltairaLabs/PromptKit/sdk"
)

redisClient := redis.NewClient(&redis.Options{
    Addr:     "redis:6379",
    Password: os.Getenv("REDIS_PASSWORD"),
    DB:       0,
})

store := statestore.NewRedisStore(
    redisClient,
    statestore.WithTTL(24 * time.Hour),  // Default is 24h
    statestore.WithPrefix("myapp"),       // Default is "promptkit"
)

conv, err := sdk.Open("./chat.pack.json", "assistant",
    sdk.WithStateStore(store),
)
```

#### How It Works

The `RedisStore` uses a decomposed key layout for efficient partial reads:

| Key Pattern | Content | Purpose |
|---|---|---|
| `{prefix}:conversation:{id}:meta` | Small JSON (ID, UserID, SystemPrompt, TokenCount, LastAccessedAt, Metadata) | Fast metadata reads without deserializing messages |
| `{prefix}:conversation:{id}:messages` | Redis list, one JSON entry per message | Supports `LRANGE` for partial reads, `RPUSH` for appends |
| `{prefix}:conversation:{id}:summaries` | Redis list, one JSON entry per summary | Append-only summary storage |
| `{prefix}:user:{userId}:conversations` | Redis set of conversation IDs | O(1) user-scoped lookups |

`Save()` uses Redis pipelining to write meta, messages, and summaries in a single round-trip. `AppendMessages()` uses `RPUSH` to add messages incrementally without rewriting the full list. `LoadRecentMessages()` uses `LRANGE` with negative indices to fetch only the last N messages.

The store automatically migrates data from the legacy monolithic format (a single JSON blob per conversation) to the decomposed format on first access. All TTLs are applied to every key in the pipeline.

#### Redis Configuration Recommendations

For production deployments:

```
# redis.conf recommendations
maxmemory-policy allkeys-lru    # Evict oldest keys when memory is full
tcp-keepalive 60                # Detect dead connections
timeout 300                     # Close idle connections after 5 min

# For Redis Cluster (10K+ conversations)
cluster-enabled yes
cluster-node-timeout 5000
```

Use Redis Sentinel or Redis Cluster for high availability. A single Redis instance can typically handle 10K-50K concurrent conversations depending on message volume and average conversation size.

### MemoryStore Defaults (Single-Instance)

If you intentionally run a single instance, the `MemoryStore` now has safe defaults:

- `DefaultTTL = 1 hour` -- entries expire after 1 hour of inactivity
- `DefaultMaxEntries = 10,000` -- LRU eviction when limit is reached (O(log N) via min-heap)
- Background eviction configurable via `WithMemoryEvictionInterval()`
- Opt out explicitly with `WithNoTTL()` or `WithNoMaxEntries()`

---

## 2. Event Bus Considerations

### Current Architecture

The `EventBus` (`runtime/events/bus.go`) is an in-process pub/sub system:

- Fixed worker pool (default `DefaultWorkerPoolSize = 10` goroutines)
- Buffered event channel (default `DefaultEventBufferSize = 1000`)
- Subscriber timeout protection (default `DefaultSubscriberTimeout = 5s`)
- Lazy worker startup -- goroutines only spawn on first `Subscribe()`/`SubscribeAll()` call
- Dropped event counter (`DroppedCount()`) for monitoring

### Multi-Instance Gap

The EventBus is purely in-process. Subscribers on pod A cannot receive events published on pod B. This affects:

- **Observability**: OTel/metrics listeners only see events from their local pod
- **Eval results**: Evaluation middleware results are local to the pod that handled the request
- **Side effects**: Any event-driven side effects (notifications, logging) are incomplete

### Recommended: Share a Single Bus Per Pod

Even within a single pod, create one `EventBus` and share it across all conversations:

```go
bus := events.NewEventBus(
    events.WithWorkerPoolSize(20),       // Scale with expected concurrency
    events.WithEventBufferSize(5000),    // Larger buffer for bursty workloads
    events.WithSubscriberTimeout(10*time.Second),
)
defer bus.Close()

// All conversations share the same bus
conv1, _ := sdk.Open("./chat.pack.json", "assistant",
    sdk.WithEventBus(bus),
    sdk.WithStateStore(store),
)
conv2, _ := sdk.Open("./chat.pack.json", "assistant",
    sdk.WithEventBus(bus),
    sdk.WithStateStore(store),
)
```

This reduces goroutine overhead from 10 workers per conversation to 10-20 workers total per pod.

### Future: Cross-Instance Event Distribution

For cross-pod event visibility, the EventBus would need an external transport layer. Candidate technologies:

| Technology | Pros | Cons | Best For |
|---|---|---|---|
| **NATS** | Lightweight, low latency, built-in clustering | Less durable than Kafka | Real-time event fanout, metrics |
| **Redis Pub/Sub** | Already deployed if using RedisStore, simple | No persistence, at-most-once delivery | Lightweight notifications |
| **Kafka** | Durable, exactly-once semantics, replay capability | Higher operational complexity, higher latency | Audit logs, eval result aggregation |

A future implementation would wrap the `EventBus` with a bridge that publishes events to the external transport and subscribes to events from other pods. The existing `EventStore` interface (`runtime/events/store.go`) provides a natural hook for persistent event storage.

**This is not yet implemented.** For now, accept that event subscribers are pod-local, and use external observability (e.g., OpenTelemetry exported to a collector) for cross-pod visibility.

---

## 3. Session Affinity

### When You Need Sticky Sessions

If you use the in-memory `MemoryStore` (the default), you **must** configure session affinity (sticky sessions) in your load balancer so that all requests for a given conversation route to the same pod. Without it, the second request for a conversation will hit a different pod and fail with `ErrConversationNotFound`.

Common approaches:

- **Kubernetes**: Use `sessionAffinity: ClientIP` on the Service, or use an Ingress controller with cookie-based affinity
- **NGINX**: `ip_hash` directive or `sticky cookie`
- **AWS ALB**: Stickiness enabled on the target group

### How to Avoid Needing Sticky Sessions

Use `RedisStore` (Section 1). When conversation state lives in Redis, any pod can serve any request. This is the recommended approach for production:

```yaml
# Kubernetes Service -- no session affinity needed with Redis
apiVersion: v1
kind: Service
metadata:
  name: promptkit
spec:
  type: ClusterIP
  # No sessionAffinity needed
  ports:
    - port: 8080
```

Additional considerations:

- **MCP processes** are pod-local (child processes). If a conversation uses MCP tools, switching pods means re-spawning MCP servers. This adds latency but not correctness issues, since MCP servers are stateless tool executors.
- **Provider instances** are pod-local (HTTP connection pools). Switching pods means establishing new connections, but `BaseProvider` configures aggressive connection pooling (`MaxIdleConns=1000`, `MaxConnsPerHost=100`) that makes this fast.
- **EventBus** is pod-local. Event subscribers on the original pod will not fire for requests that land on a different pod. Use external observability for cross-pod visibility.

---

## 4. Resource Limit Recommendations

### Key Constants in the Codebase

| Constant | Location | Default | Purpose |
|---|---|---|---|
| `DefaultStreamBufferSize` | `runtime/providers/streaming.go` | 32 | Buffered channel for stream chunks |
| `DefaultStreamIdleTimeout` | `runtime/providers/streaming.go` | 30s | Max wait between stream chunks |
| `DefaultChannelBufferSize` | `runtime/pipeline/stage/config.go` | 32 | Inter-stage pipeline channel buffer |
| `DefaultAudioChannelBufferSize` | `runtime/pipeline/stage/config.go` | 64 | Audio pipeline channel buffer (~1.3s at 50 chunks/sec) |
| `DefaultMaxAudioBufferSize` | `runtime/audio/silence.go` | 10 MB | Max VAD audio buffer per connection |
| `DefaultToolTimeout` | `runtime/tools/registry.go` | 30,000 ms | Per-tool execution timeout |
| `DefaultMaxToolResultSize` | `runtime/tools/registry.go` | 1 MB | Max tool result payload |
| `defaultMaxParallelToolCalls` | `runtime/pipeline/stage/stages_provider.go` | 10 | Concurrent tool executions per turn |
| `DefaultMaxProcesses` | `runtime/mcp/registry.go` | 0 (unlimited) | Max concurrent MCP child processes |
| `defaultMaxReconnectAttempts` | `runtime/mcp/client.go` | 3 | MCP process reconnection attempts |
| `DefaultMaxClients` | `runtime/a2a/executor.go` | 100 | Max cached A2A clients |
| `DefaultClientTTL` | `runtime/a2a/executor.go` | 30 min | A2A client cache TTL |
| `DefaultTTL` (MemoryStore) | `runtime/statestore/memory.go` | 1 hour | In-memory state expiration |
| `DefaultMaxEntries` | `runtime/statestore/memory.go` | 10,000 | In-memory state max entries |
| `DefaultWorkerPoolSize` | `runtime/events/bus.go` | 10 | EventBus worker goroutines |
| `DefaultEventBufferSize` | `runtime/events/bus.go` | 1,000 | EventBus channel buffer |
| `DefaultMaxIdleConns` | `runtime/providers/base_provider.go` | 1,000 | HTTP transport idle connections |
| `DefaultMaxConnsPerHost` | `runtime/providers/base_provider.go` | 100 | HTTP transport max per host |

### Per-Connection Resource Footprint

| Workload | Goroutines (shared EventBus) | Memory per Connection | Notes |
|---|---|---|---|
| **Chat-only (text)** | ~12-15 | 200-500 KB | Pipeline stages + state |
| **Chat with tools** | ~15-20 | 300 KB - 1 MB | Add tool execution goroutines (up to `defaultMaxParallelToolCalls`) |
| **Audio ASM** (provider-side VAD) | ~20-22 | 600 KB - 1 MB | WebSocket + pipeline + streaming |
| **Audio VAD** (local VAD + STT/TTS) | ~23-26 | 1-3 MB | Depends on utterance length; `DefaultMaxAudioBufferSize = 10MB` caps worst case |
| **With MCP tools** | +2 per MCP server | +~50 KB per server | `readLoop` + `logStderr` goroutines per child process |

### Sizing Guidance

#### Chat-Only Workloads (e.g., customer support bot)

```yaml
resources:
  requests:
    cpu: "500m"
    memory: "512Mi"
  limits:
    cpu: "2"
    memory: "2Gi"
```

- Supports ~1,000-2,000 concurrent conversations per pod
- CPU is dominated by JSON serialization and HTTP I/O
- Set `MaxProcesses` on MCP registry if MCP tools are used

#### Tool-Heavy Workloads (e.g., agent with database/API tools)

```yaml
resources:
  requests:
    cpu: "1"
    memory: "1Gi"
  limits:
    cpu: "4"
    memory: "4Gi"
```

- Each tool call fan-out creates up to 10 goroutines (`defaultMaxParallelToolCalls`)
- Tool result size capped at `DefaultMaxToolResultSize = 1MB`
- Set `RegistryOptions{MaxProcesses: 20}` on MCP registry to bound child processes:

```go
mcpRegistry := mcp.NewRegistryWithOptions(mcp.RegistryOptions{
    MaxProcesses: 20,
})
```

#### Audio/Video Workloads (duplex streaming)

```yaml
resources:
  requests:
    cpu: "2"
    memory: "4Gi"
  limits:
    cpu: "8"
    memory: "16Gi"
```

- Memory dominated by audio buffers: each connection uses up to 10MB for VAD buffer
- Network bandwidth is the primary bottleneck: ~32 KB/s per direction per connection (PCM 16kHz/16-bit/mono)
- At 1,000 connections: ~64 MB/s bidirectional PCM bandwidth
- Provider WebSocket limits may be the real ceiling (typically 100-1000 per API key)

#### OS-Level Tuning for K8s Nodes

For any workload above 1,000 concurrent connections:

```bash
# /etc/security/limits.conf or K8s securityContext
ulimit -n 65536                    # File descriptors

# sysctl (node-level)
net.core.somaxconn = 32768
net.ipv4.tcp_tw_reuse = 1
net.ipv4.ip_local_port_range = 1024 65535
```

---

## 5. Auto-Scaling Policy Guidance

### Metrics to Monitor

Expose these via Prometheus and use them for HPA scaling decisions:

| Metric | Source | Why |
|---|---|---|
| **Active conversations** | Application counter (increment on `Open()`, decrement on `Close()`) | Primary demand signal |
| **Goroutine count** | `runtime.NumGoroutine()` | Proxy for overall load; alert at 200K+ |
| **Event bus dropped count** | `bus.DroppedCount()` | Indicates backpressure; scale out if consistently > 0 |
| **MCP active processes** | `registry.ActiveProcessCount()` | OS resource pressure |
| **Response latency (p95/p99)** | OTel or application middleware | User-facing quality signal |
| **Redis operation latency** | Redis client metrics | State store becoming a bottleneck |
| **Provider rate limit 429s** | Provider retry metrics | May need to spread load across API keys |

### Kubernetes HPA Configuration

#### Chat Workloads

Scale on a custom metric for active conversations per pod:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: promptkit-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: promptkit
  minReplicas: 2
  maxReplicas: 20
  metrics:
    # Primary: active conversations per pod
    - type: Pods
      pods:
        metric:
          name: promptkit_active_conversations
        target:
          type: AverageValue
          averageValue: "500"    # Target 500 active conversations per pod
    # Secondary: CPU as a safety net
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 30
      policies:
        - type: Pods
          value: 2
          periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 300   # 5 min cooldown before scaling down
      policies:
        - type: Pods
          value: 1
          periodSeconds: 120
```

#### Audio Workloads

Scale on memory (audio buffers) and connection count:

```yaml
metrics:
  - type: Pods
    pods:
      metric:
        name: promptkit_active_duplex_sessions
      target:
        type: AverageValue
        averageValue: "200"     # Conservative: 200 audio sessions per pod
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 60  # Audio buffers consume memory rapidly
```

### Graceful Shutdown

PromptKit includes a `ShutdownManager` (`sdk/shutdown.go`) for coordinated shutdown:

```go
mgr := sdk.NewShutdownManager()

// Conversations auto-register when WithShutdownManager is used
conv, _ := sdk.Open("./chat.pack.json", "assistant",
    sdk.WithStateStore(store),
    sdk.WithShutdownManager(mgr),
)

// In your main() or signal handler:
// Listens for SIGTERM/SIGINT, closes all tracked conversations
sdk.GracefulShutdown(mgr, 30*time.Second)
```

For Kubernetes, set `terminationGracePeriodSeconds` in your pod spec to match or exceed your shutdown timeout:

```yaml
spec:
  terminationGracePeriodSeconds: 45   # > GracefulShutdown timeout (30s)
  containers:
    - name: promptkit
      # ...
```

The A2A server also exposes health endpoints:

- `GET /healthz` -- liveness probe (always 200 when server is running)
- `GET /readyz` -- readiness probe (200 when ready to accept traffic)

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
```

### Scaling Checklist

| Item | Single Instance | Multi-Instance |
|---|---|---|
| State store | `MemoryStore` (default) | `RedisStore` via `WithStateStore()` |
| Event bus | One per conversation (default) | Shared via `WithEventBus()` |
| Session affinity | Not needed | Not needed with Redis; required with MemoryStore |
| Provider instances | One per conversation (default) | Share via `WithProvider()` to reduce connection pools |
| MCP registry | Per-conversation (default) | Share across conversations; set `MaxProcesses` |
| Graceful shutdown | `Conversation.Close()` | `ShutdownManager` + `GracefulShutdown()` |
| Health probes | N/A | `/healthz` + `/readyz` on A2A server |
| Goroutine monitoring | Optional | Expose `runtime.NumGoroutine()` as Prometheus metric |
