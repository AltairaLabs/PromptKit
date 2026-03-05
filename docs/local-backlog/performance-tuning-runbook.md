# PromptKit Performance Tuning Runbook

Practical guide to tuning PromptKit's runtime parameters for throughput, latency, and memory. All values reference code constants and configuration options introduced in the scalability work (March 2026).

---

## 1. EventBus Tuning

**Source**: `runtime/events/bus.go`

| Parameter | Default | Option | Effect |
|-----------|---------|--------|--------|
| Worker pool size | `DefaultWorkerPoolSize = 10` | `WithWorkerPoolSize(n)` | Number of goroutines dispatching events to subscribers |
| Event buffer | `DefaultEventBufferSize = 1000` | `WithEventBufferSize(n)` | Buffered channel capacity between `Publish()` and workers |
| Subscriber timeout | `DefaultSubscriberTimeout = 5s` | `WithSubscriberTimeout(d)` | Max time a listener may run before being skipped |

### When to adjust

- **Increase worker pool size** when `DroppedCount()` rises while subscriber callbacks are fast (workers cannot keep up with dispatch volume). Start with 20, measure, and go up to 50. Beyond that, contention on the subscriber list `RLock` becomes the bottleneck.
- **Increase event buffer** when `DroppedCount()` rises in short bursts but workers catch up during steady state. A buffer of 5000-10000 absorbs spikes without adding goroutines.
- **Decrease worker pool size** when running many conversations per process with a shared `EventBus` (via `sdk.WithEventBus()`). A shared bus with 10 workers serves all conversations; per-conversation buses waste 10 goroutines each.
- **Increase subscriber timeout** when listeners perform I/O (e.g., writing to an external telemetry sink). Set to 10-15s. Avoid going above 30s as a slow subscriber holds a worker.
- **Decrease subscriber timeout** when all listeners are in-process and fast (e.g., metrics counters). Set to 1-2s to detect stuck subscribers quickly.

### Symptoms of misconfiguration

| Symptom | Likely cause |
|---------|-------------|
| `DroppedCount()` increasing steadily | Buffer too small or workers too few for the event rate |
| Log: `"event dropped: buffer full"` (rate-limited, every 100th drop) | Same as above |
| Log: `"event subscriber timed out"` | Subscriber is slow or the timeout is too short |
| High goroutine count from event bus | Per-conversation buses instead of shared; or pool size too large |

### Lazy startup

Workers are started lazily on the first `Subscribe()`, `SubscribeAll()`, or `WithStore()` call. If no one subscribes, zero goroutines are spawned. Event store writes happen inside the worker goroutine (asynchronous to the publisher).

### Recommended production config

```go
bus := events.NewEventBus(
    events.WithWorkerPoolSize(20),
    events.WithEventBufferSize(5000),
    events.WithSubscriberTimeout(10 * time.Second),
)
// Share across all conversations:
conv, _ := sdk.Open(pack, sdk.WithEventBus(bus))
```

---

## 2. State Store Tuning

### MemoryStore

**Source**: `runtime/statestore/memory.go`

| Parameter | Default | Option | Effect |
|-----------|---------|--------|--------|
| TTL | `DefaultTTL = 1 hour` | `WithMemoryTTL(d)` / `WithNoTTL()` | Time since last access before entry is eligible for eviction |
| Max entries | `DefaultMaxEntries = 10,000` | `WithMemoryMaxEntries(n)` / `WithNoMaxEntries()` | Hard cap; LRU eviction when exceeded |
| Eviction interval | 0 (disabled) | `WithMemoryEvictionInterval(d)` | Background goroutine tick for TTL cleanup |

### When to adjust

- **Increase TTL** for long-lived conversations (e.g., support tickets that span hours). Set to 4-8h.
- **Decrease TTL** for high-throughput stateless workloads (e.g., one-shot API calls). Set to 5-15min to free memory quickly.
- **Increase max entries** if you expect more than 10K concurrent conversations on a single process. Each entry costs ~1-50 KB depending on message history length.
- **Enable eviction interval** in server deployments to proactively reclaim memory. Recommended: `WithMemoryEvictionInterval(1 * time.Minute)`. Without this, expired entries are only cleaned on access (lazy eviction).

### LRU eviction performance

Eviction uses a `container/heap`-based min-heap (`accessHeap`), giving O(log N) eviction instead of the previous O(N) scan. The two-phase expiration approach (`collectExpiredKeys` under `RLock`, then `deleteExpiredKeys` under `Lock`) avoids blocking reads during background cleanup.

### RedisStore

**Source**: `runtime/statestore/redis.go`

| Parameter | Default | Option | Effect |
|-----------|---------|--------|--------|
| TTL | 24 hours | `WithTTL(d)` | Redis key expiration |
| Key prefix | `"promptkit"` | `WithPrefix(s)` | Namespace isolation |

Redis connection pooling is configured through the `go-redis` client options:

```go
client := redis.NewClient(&redis.Options{
    Addr:         "localhost:6379",
    PoolSize:     50,         // max connections (default: 10 * GOMAXPROCS)
    MinIdleConns: 10,         // keep warm connections ready
    PoolTimeout:  30 * time.Second,
    ReadTimeout:  5 * time.Second,
    WriteTimeout: 5 * time.Second,
})
store := statestore.NewRedisStore(client, statestore.WithTTL(2 * time.Hour))
```

### When to switch from MemoryStore to RedisStore

- Multi-pod Kubernetes deployments (state must survive pod restarts and be accessible from any pod)
- Memory pressure on the application pod (offload state to dedicated Redis)
- Conversations exceeding 10K concurrent on a single process
- Need for durable state across deploys

The RedisStore uses decomposed storage: metadata, messages, and summaries are stored as separate Redis keys/lists. `Save()` only serializes metadata fully; messages use Redis lists for incremental append. The store falls back to legacy monolithic format for backward compatibility.

---

## 3. Provider / Streaming Tuning

**Source**: `runtime/providers/streaming.go`, `runtime/providers/idle_timeout.go`, `runtime/providers/base_provider.go`, `runtime/providers/retry.go`

| Parameter | Default | Source | Effect |
|-----------|---------|--------|--------|
| Stream buffer size | `DefaultStreamBufferSize = 32` | `streaming.go` | Channel buffer for `StreamChunk` between provider goroutine and consumer |
| Stream idle timeout | `DefaultStreamIdleTimeout = 30s` | `idle_timeout.go` | Max time between stream data reads before closing the connection |
| Max payload size | `DefaultMaxPayloadSize = 100 MB` | `base_provider.go` | Request body size limit; returns `ErrPayloadTooLarge` when exceeded |
| Retry attempts | `DefaultMaxRetries = 3` | `retry.go` | Retries on 429/502/503/504 and network errors |
| Retry initial delay | `DefaultInitialDelayMs = 500` | `retry.go` | Exponential backoff base (with jitter) |
| Rate limiter | Not set by default | `BaseProvider.SetRateLimit(rps, burst)` | Token bucket rate limiter for outbound requests |

### Stream buffer size

The buffer of 32 prevents the streaming goroutine from blocking on every chunk send when the consumer is slow. For audio pipelines with high chunk rates (~50 chunks/sec), consider the inter-stage buffer sizes instead (see section below).

- **Increase to 64-128** if you see streaming stalls in logs and the consumer processes chunks in batches.
- **Decrease to 8-16** if memory is constrained and each `StreamChunk` carries large accumulated content strings.

### Stream idle timeout

The `IdleTimeoutReader` wraps the HTTP response body. The timer resets on each successful read. If no data arrives within 30s, the body is closed and `ErrStreamIdleTimeout` is returned.

- **Increase to 60-120s** for providers that have long "thinking" pauses (e.g., complex reasoning models).
- **Decrease to 10-15s** for latency-sensitive applications where a stalled stream should fail fast.

### Rate limiting

```go
provider.SetRateLimit(10.0, 5)  // 10 requests/sec, burst of 5
```

Set this based on your provider's rate limit tier. `WaitForRateLimit()` is called before every HTTP request in `MakeRawRequest()`.

### Audio pipeline buffer sizes

**Source**: `runtime/pipeline/stage/config.go`, `runtime/audio/silence.go`

| Parameter | Default | Effect |
|-----------|---------|--------|
| Inter-stage channel buffer | `DefaultChannelBufferSize = 32` | Buffer between pipeline stages |
| Audio channel buffer | `DefaultAudioChannelBufferSize = 64` | ~1.3s of buffering at 50 chunks/sec |
| Audio buffer cap | `DefaultMaxAudioBufferSize = 10 MB` | Max size of `SilenceDetector.audioBuffer`; truncated when exceeded |

For duplex audio at scale, use `DefaultAudioChannelBufferSize` for audio pipeline stages to avoid backpressure stalls. The audio buffer cap prevents unbounded memory growth during long utterances.

---

## 4. MCP Tuning

**Source**: `runtime/mcp/registry.go`, `runtime/mcp/client.go`

| Parameter | Default | Option/Field | Effect |
|-----------|---------|-------------|--------|
| Max processes | `DefaultMaxProcesses = 0` (unlimited) | `RegistryOptions.MaxProcesses` | Semaphore-based limit on concurrent child processes |
| Request timeout | 30s | `ClientOptions.RequestTimeout` | Per-RPC request timeout |
| Init timeout | 10s | `ClientOptions.InitTimeout` | Initialization handshake timeout |
| Max retries | 3 | `ClientOptions.MaxRetries` | RPC request retries with exponential backoff |
| Retry delay | 100ms | `ClientOptions.RetryDelay` | Initial backoff delay |
| Max reconnect attempts | 3 | `ClientOptions.MaxReconnectAttempts` | Auto-reconnection attempts on process death |

### When to adjust

- **Set MaxProcesses** in production to prevent OS resource exhaustion. Each MCP server is a child process with 2+ background goroutines (`readLoop`, `logStderr`). Recommended: set to 2-3x the number of distinct MCP servers you expect to run concurrently.

```go
registry := mcp.NewRegistryWithOptions(mcp.RegistryOptions{
    MaxProcesses: 20,
})
```

- **Increase request timeout** for MCP tools that perform heavy computation (e.g., code execution sandboxes). Set to 60-120s.
- **Decrease reconnect attempts** to 0 if you want fail-fast behavior (the caller handles restart logic).
- **Increase reconnect attempts** to 5 for flaky MCP servers that crash intermittently.

### Symptoms of misconfiguration

| Symptom | Likely cause |
|---------|-------------|
| `ErrMaxProcessesReached` errors | `MaxProcesses` too low for workload; increase or share registry across conversations |
| Goroutine count growing with `readLoop`/`logStderr` stacks | Dead MCP processes not cleaned up; check `MaxReconnectAttempts` |
| Tool execution timeouts | `RequestTimeout` too short for the tool's workload |

### Sharing MCP registry

In server deployments, create one registry and share it across conversations to avoid O(N) child processes:

```go
registry, _ := mcp.NewRegistryWithServers(serverConfigs)
// Pass to each conversation via SDK options
```

---

## 5. Storage Tuning

### FileStore

**Source**: `runtime/storage/local/filestore.go`

| Parameter | Default | Option | Effect |
|-----------|---------|--------|--------|
| Max file size | `DefaultMaxFileSize = 50 MB` | `WithMaxFileSize(n)` | Per-file size limit; returns `ErrFileTooLarge` |
| Dedup enabled | Configurable | `FileStoreConfig.EnableDeduplication` | SHA-256 content-based deduplication |

### Dedup dirty flag optimization

The `dedupDirty` flag ensures the dedup index is only written to disk when actually modified. Dedup cache hits skip writes entirely. Call `Flush()` explicitly to persist the index before a graceful shutdown.

- For high-throughput media storage, enable dedup to reduce disk usage for duplicate content (e.g., repeated audio chunks).
- For workloads with unique content, disable dedup to avoid the SHA-256 hashing overhead.

### Policy enforcement

**Source**: `runtime/storage/policy/time_based.go`

| Parameter | Default | Effect |
|-----------|---------|--------|
| Enforcement interval | Configurable at construction | Tick interval for background cleanup |
| Auto-start | `false` | `WithAutoStart(true)` starts enforcement on construction |
| Base dir | Required for auto-start | `WithBaseDir(dir)` sets the scan directory |

The expiry index (`expiryIndex` map) is built once at startup via `BuildExpiryIndex()` and maintained incrementally via `TrackFile()`/`UntrackFile()`. Subsequent enforcement ticks only check files whose expiry has passed (O(expired) instead of O(total)).

### Recommended config

```go
handler := policy.NewTimeBasedPolicyHandler(
    5 * time.Minute,               // enforcement interval
    policy.WithAutoStart(true),
    policy.WithBaseDir("/data/media"),
)
```

- **Decrease enforcement interval** (to 1 min) for storage-constrained environments where expired files should be cleaned quickly.
- **Increase enforcement interval** (to 15-30 min) for low-churn environments to reduce disk I/O.

---

## 6. Monitoring Checklist

### Key metrics to expose

| Metric | Source | What it tells you |
|--------|--------|-------------------|
| Event bus dropped count | `EventBus.DroppedCount()` | Events lost due to full buffer — indicates undersized buffer or workers |
| Subscriber timeout count | Log: `"event subscriber timed out"` | Slow subscribers blocking workers |
| Memory store entry count | `MemoryStore.Len()` | Current state store size; watch for approaching `MaxEntries` |
| MCP active processes | `RegistryImpl.ActiveProcessCount()` | Current child process count; watch for approaching `MaxProcesses` |
| Goroutine count | `runtime.NumGoroutine()` | Overall goroutine pressure; alert at 200K+ |
| Provider retry count | Log: retry attempts from `DoWithRetry()` | Rate limit pressure or provider instability |
| Stream idle timeouts | Log: `ErrStreamIdleTimeout` occurrences | Provider stalls or network issues |
| Tool execution timeouts | Log: `ErrToolTimeout` occurrences | Tools exceeding their deadline |
| Pending tool store size | `PendingStore` count | Unresolved async tool calls; leaks if growing unbounded |
| Policy expiry index size | `TimeBasedPolicyHandler.ExpiryIndexLen()` | Files tracked for cleanup |

### Prometheus integration pattern

PromptKit does not ship a built-in Prometheus exporter, but metrics can be exposed by subscribing to the event bus and maintaining counters:

```go
bus := events.NewEventBus()

// Track event throughput
var eventCount prometheus.Counter
bus.SubscribeAll(func(e *events.Event) {
    eventCount.Inc()
})

// Periodic metric collection
go func() {
    ticker := time.NewTicker(15 * time.Second)
    for range ticker.C {
        droppedGauge.Set(float64(bus.DroppedCount()))
        storeEntriesGauge.Set(float64(memStore.Len()))
        mcpProcessGauge.Set(float64(mcpRegistry.ActiveProcessCount()))
        goroutineGauge.Set(float64(runtime.NumGoroutine()))
    }
}()
```

---

## 7. Common Issues and Fixes

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| Events silently disappearing | Event buffer full, events dropped | Increase `WithEventBufferSize()` to 5000+ or add more workers |
| `"event subscriber timed out"` warnings | Subscriber doing I/O in callback | Increase `WithSubscriberTimeout()` or make the subscriber async |
| Memory growing unbounded in long-running process | MemoryStore with no TTL or no eviction interval | Enable `WithMemoryEvictionInterval(1*time.Minute)` and verify TTL is set |
| `ErrNotFound` on `Resume()` across pods | Using MemoryStore in multi-pod deployment | Switch to `RedisStore` with `sdk.WithStateStore()` |
| `ErrMaxProcessesReached` on tool calls | MCP process limit too low | Increase `MaxProcesses` or share registry across conversations |
| Streaming responses cut off mid-way | HTTP client timeout (60s) too short for long responses | Increase provider HTTP client timeout |
| `ErrStreamIdleTimeout` during long pauses | Provider thinking time exceeds 30s idle timeout | Increase `DefaultStreamIdleTimeout` (requires code change) or use a provider-specific timeout |
| `ErrPayloadTooLarge` on send | Request body exceeds 100 MB | Reduce message history or media; or increase via `SetMaxPayloadSize()` |
| `ErrFileTooLarge` on media store | File exceeds 50 MB limit | Increase via `WithMaxFileSize()` or compress before storing |
| High GC pressure with audio workloads | Frequent `[]int16` allocations in resampling | Already mitigated with `sync.Pool`; verify pool is being used |
| 429 errors from provider | Missing rate limiter | Configure `provider.SetRateLimit(rps, burst)` based on provider tier |
| Tool calls running sequentially | `MaxParallelToolCalls` set to 1 | Increase `ToolPolicy.MaxParallelToolCalls` (default 10) |
| Eval goroutines piling up | More evals dispatched than semaphore allows | Increase `WithMaxConcurrentEvals(n)` (default 10); evals exceeding the limit are skipped with a warning |
| Conversation history growing without bound | No token budget or context window configured | Set `ContextAssemblyConfig.MaxMessages` (default 200) or configure `TokenBudgetStage` |
| Disk filling up with expired media | Policy enforcement not running | Use `WithAutoStart(true)` and `WithBaseDir(dir)` on `TimeBasedPolicyHandler` |
| Full directory walk on every enforcement tick | Expiry index not built | Ensure `BuildExpiryIndex()` is called at startup (automatic with `StartEnforcement()`) |

---

## Quick Reference: All Defaults

| Component | Parameter | Default Value | Code Location |
|-----------|-----------|---------------|---------------|
| EventBus | Worker pool size | 10 | `runtime/events/bus.go:15` |
| EventBus | Event buffer size | 1000 | `runtime/events/bus.go:16` |
| EventBus | Subscriber timeout | 5s | `runtime/events/bus.go:17` |
| MemoryStore | TTL | 1 hour | `runtime/statestore/memory.go:54` |
| MemoryStore | Max entries | 10,000 | `runtime/statestore/memory.go:59` |
| RedisStore | TTL | 24 hours | `runtime/statestore/redis.go:58` |
| Streaming | Buffer size | 32 | `runtime/providers/streaming.go:13` |
| Streaming | Idle timeout | 30s | `runtime/providers/streaming.go:18` |
| Provider | Max payload | 100 MB | `runtime/providers/base_provider.go:37` |
| Provider | Max retries | 3 | `runtime/providers/retry.go:20` |
| Provider | Initial retry delay | 500ms | `runtime/providers/retry.go:21` |
| MCP | Max processes | 0 (unlimited) | `runtime/mcp/registry.go:14` |
| MCP | Request timeout | 30s | `runtime/mcp/client.go:38` |
| MCP | Init timeout | 10s | `runtime/mcp/client.go:39` |
| MCP | Max reconnect attempts | 3 | `runtime/mcp/client.go:49` |
| Storage | Max file size | 50 MB | `runtime/storage/local/filestore.go:36` |
| Tools | Default timeout | 30s (30000ms) | `runtime/tools/registry.go:22` |
| Tools | Max result size | 1 MB | `runtime/tools/registry.go:26` |
| Tools | Max parallel tool calls | 10 | `runtime/pipeline/stage/stages_provider.go:24` |
| Pipeline | Max messages | 200 | `runtime/pipeline/stage/stages_context.go:18` |
| Pipeline | Warning threshold | 100 messages | `runtime/pipeline/stage/stages_context.go:22` |
| Pipeline | Channel buffer | 32 | `runtime/pipeline/stage/config.go:11` |
| Pipeline | Audio channel buffer | 64 | `runtime/pipeline/stage/config.go:14` |
| Audio | Max audio buffer | 10 MB | `runtime/audio/silence.go:12` |
| Eval | Max concurrent evals | 10 | `sdk/eval_middleware.go:13` |
| Pending tools | TTL | 5 min | `sdk/tools/pending.go:15` |
| Pending tools | Max pending | 1000 | `sdk/tools/pending.go:19` |
| A2A | Client cache TTL | 30 min | `runtime/a2a/executor.go:29` |
| A2A | Max cached clients | 100 | `runtime/a2a/executor.go:33` |
