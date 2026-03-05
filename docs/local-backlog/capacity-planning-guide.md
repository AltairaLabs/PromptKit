# PromptKit Capacity Planning Guide

Practical guidance for estimating resource requirements when deploying PromptKit at scale.

---

## 1. Per-Conversation Memory Footprint

Each active conversation allocates several runtime objects. The table below summarizes the major components.

| Component | Memory Estimate | Notes |
|-----------|----------------|-------|
| **ConversationState** (statestore) | 2-50 KB | Scales with message count. Each `types.Message` is ~200-500 bytes for text. At the 200-message default cap (`DefaultMaxMessages`), expect ~40-100 KB. |
| **Conversation struct** (sdk) | ~2 KB | Fixed overhead: config pointers, handler maps, mutexes, mode union. |
| **EventBus** (per-conversation) | ~80 KB | 10 worker goroutines (8 KB stack each) + 1000-element buffered channel. **Strongly recommend sharing** a single bus via `WithEventBus()`. |
| **EventBus** (shared) | ~0 KB per conversation | Amortized across all conversations. Single bus: 10 workers total. |
| **Provider HTTP client** (per-conversation) | ~50-100 KB | Connection pool (`MaxIdleConns=1000`, `MaxConnsPerHost=100`), TLS state. **Recommend sharing** via `WithProvider()`. |
| **Provider HTTP client** (shared) | ~0 KB per conversation | Pool shared across all conversations to the same endpoint. |
| **Tool registry** | ~1-5 KB | Map of tool descriptors and executors. |
| **Streaming channel buffer** | ~6 KB (active only) | `DefaultStreamBufferSize = 32` elements, allocated only during active streaming. |
| **MCP client** (per-server) | ~500 KB-1 MB | Child process + 2 goroutines (readLoop, logStderr) + pipe buffers. See section on MCP below. |
| **Eval middleware** | ~1 KB | Semaphore channel (`DefaultMaxConcurrentEvals = 10`), WaitGroup. |

### Summary by Configuration

| Configuration | Memory per Conversation |
|---------------|------------------------|
| Shared EventBus + shared provider (recommended) | **5-60 KB** |
| Per-conversation EventBus, shared provider | **85-160 KB** |
| Per-conversation EventBus + per-conversation provider | **135-260 KB** |
| With 3 MCP servers (per-conversation registry) | **add 1.5-3 MB** |

---

## 2. Workload Profiles

### Chat-Only (text-based, no tools)

Typical use case: customer support bots, FAQ systems, simple Q&A.

| Resource | Per Conversation | At 1,000 | At 10,000 |
|----------|-----------------|----------|-----------|
| Memory (shared bus/provider) | 5-60 KB | 50-600 MB | 500 MB-6 GB |
| Goroutines | 2-5 | 2,000-5,000 | 20,000-50,000 |
| Provider API calls | 1 per turn | — | — |
| State store entries | 1 | 1,000 | 10,000 |
| CPU (steady state) | negligible | 0.5-1 core | 2-5 cores |

**Key constraint**: Provider rate limits (see section 5). At 10K concurrent, you need provider capacity for burst request rates during peak activity.

### Tool-Heavy (MCP tools, A2A agents)

Typical use case: agentic workflows, multi-step reasoning with external tools.

| Resource | Per Conversation | At 1,000 | At 10,000 |
|----------|-----------------|----------|-----------|
| Memory (shared bus/provider) | 50-200 KB | 500 MB-2 GB | 5-20 GB |
| Memory (with 3 MCP servers, shared registry) | 50-200 KB + 1.5 MB fixed | 500 MB-2 GB + 1.5 MB | 5-20 GB + 1.5 MB |
| Memory (with 3 MCP servers, per-conversation) | 1.5-3.2 MB | 15-32 GB | **150-320 GB** |
| Goroutines (shared MCP registry) | 5-15 | 5,000-15,000 | 50,000-150,000 |
| Tool calls per turn | 1-10 (parallel, max 10) | — | — |
| A2A client cache | 30 min TTL, max 100 clients | — | — |
| CPU per turn | moderate (tool execution) | 2-5 cores | 10-30 cores |

**Critical**: Share the MCP registry across conversations. Per-conversation MCP processes will exhaust OS process and file descriptor limits. At 10K conversations with 3 MCP servers each, that would be 30,000 child processes.

### Audio/Video (real-time streaming with media)

Typical use case: voice assistants, real-time audio agents.

| Resource | Per Connection (ASM) | Per Connection (VAD) | At 1,000 (ASM) | At 10,000 (ASM) |
|----------|---------------------|---------------------|-----------------|------------------|
| Memory | 600 KB-1 MB | 1-3 MB | 6-10 GB | 60-100 GB |
| Goroutines (shared bus) | 12-14 | 15-18 | 12,000-14,000 | 120,000-140,000 |
| Network bandwidth | 32 KB/s per direction | 32 KB/s per direction | 64 MB/s bidir | 640 MB/s bidir |
| WebSocket connections | 1 to provider | 1 to provider | 1,000 | 10,000 |
| Audio buffer (VAD max) | 10 MB cap | 10 MB cap | — | — |
| CPU (VAD + resample) | — | ~0.3 ms/chunk | — | 2-6 cores |

**Key constraints**: Provider WebSocket connection limits (typically 100-1000 per account), network bandwidth (multi-gigabit links needed at 10K), and base64 encoding overhead (33% increase for OpenAI).

Channel buffer sizes for audio pipelines: `DefaultAudioChannelBufferSize = 64` elements (~1.3s of buffering at 50 chunks/sec).

---

## 3. Scaling Formulas

### Basic Instance Sizing

```
conversations_per_instance = available_memory / memory_per_conversation
instances = ceil(target_conversations / conversations_per_instance)
```

**Example (chat-only, shared bus/provider):**
- Pod memory: 4 GB available (after Go runtime, OS overhead)
- Memory per conversation: ~60 KB (upper bound for 200-message conversations)
- conversations_per_instance = 4 GB / 60 KB = ~68,000
- For 10,000 target conversations: 1 instance

**Example (tool-heavy, shared MCP):**
- Pod memory: 8 GB available
- Memory per conversation: ~200 KB
- conversations_per_instance = 8 GB / 200 KB = ~40,000
- For 10,000 target conversations: 1 instance

**Example (audio/ASM):**
- Pod memory: 16 GB available
- Memory per conversation: ~1 MB
- conversations_per_instance = 16 GB / 1 MB = ~16,000
- For 10,000 target connections: 1 instance (but see constraints below)

### Goroutine Budget

Go handles goroutines efficiently, but each has a minimum 8 KB stack that can grow. Plan for:

```
goroutine_memory = goroutine_count * avg_stack_size
```

Conservative estimate: 16 KB average stack per goroutine.

| Goroutine Count | Estimated Stack Memory |
|----------------|----------------------|
| 50,000 | 800 MB |
| 150,000 | 2.4 GB |
| 250,000 | 4 GB |

Alert threshold: 200,000+ goroutines. Expose `runtime.NumGoroutine()` as a Prometheus metric.

### CPU Sizing

For text workloads, CPU is rarely the bottleneck (most time is spent waiting on provider API responses). Key CPU consumers:

- **Token counting** (heuristic): negligible
- **State deep-copy**: ~0.1 ms per operation for typical conversations
- **JSON serialization** (Redis state store): ~0.5-2 ms per save for 200 messages
- **VAD processing**: ~0.3 ms per 20ms audio chunk
- **Audio resampling**: ~0.2 ms per chunk (pooled buffers)

Rule of thumb:
```
cpu_cores = base_cores + (audio_connections * 0.0005) + (tool_calls_per_second * 0.001)
```

Where `base_cores = 1-2` for the Go runtime, GC, and HTTP handling.

---

## 4. Storage Planning

### MemoryStore (In-Process)

Default limits:
- `DefaultMaxEntries = 10,000` conversations
- `DefaultTTL = 1 hour` (entries expire after last access)
- LRU eviction when max entries reached (O(log N) via heap)

Memory consumption: number of entries * average state size. For 10,000 conversations at 50 KB each = ~500 MB.

**Not suitable for production multi-pod deployments** -- state is process-local.

### Redis State Store

Decomposed storage (3 keys per conversation after scalability fixes):
- `{prefix}:{id}:meta` -- small JSON (~200-500 bytes): ID, UserID, SystemPrompt, TokenCount, timestamps
- `{prefix}:{id}:messages` -- Redis list, one entry per message (~200-500 bytes each)
- `{prefix}:{id}:summaries` -- Redis list, one entry per summary (~200-1000 bytes each)

Default TTL: 24 hours.

| Conversations | Avg Messages | Redis Memory Estimate |
|--------------|-------------|----------------------|
| 1,000 | 50 | ~25-50 MB |
| 10,000 | 50 | ~250-500 MB |
| 10,000 | 200 | ~1-2 GB |
| 100,000 | 50 | ~2.5-5 GB |

Growth rate depends on conversation creation rate and TTL. With 24-hour TTL, Redis memory is bounded by:

```
peak_redis_memory = max_daily_conversations * avg_messages * 500 bytes
```

**Recommendations:**
- Set Redis `maxmemory` with `allkeys-lru` eviction policy as a safety net.
- Monitor Redis memory usage and key count.
- Tune TTL based on conversation patterns (`WithTTL()` option on Redis store).

### FileStore (Local Disk)

Used for media files (images, audio, documents).

| Setting | Default | Notes |
|---------|---------|-------|
| Max file size | 50 MB (`DefaultMaxFileSize`) | Per-file limit |
| Max aggregate media | 100 MB (`MaxAggregateMediaSize`) | Per-loader instance |
| Max media items | 20 (`MaxMediaItems`) | Per-loader instance |
| Dedup index | In-memory map | Written to disk only when dirty |

Disk growth rate:
```
daily_disk_growth = new_media_files_per_day * avg_file_size
```

**Recommendations:**
- Enable retention policy enforcement (`WithAutoStart(true)`) to automatically clean expired media.
- For production, replace `FileStore` with a cloud storage backend (S3/GCS).
- Set `TimeBasedPolicy` with appropriate retention (e.g., 24-48 hours for transient media).

---

## 5. Provider Rate Limits

### Typical Provider Limits

| Provider | RPM (Requests/Min) | TPM (Tokens/Min) | Concurrent Connections |
|----------|--------------------|--------------------|----------------------|
| OpenAI GPT-4 | 500-10,000 | 30K-800K | No hard limit (HTTP) |
| OpenAI Realtime | — | — | 100-1,000 WebSocket |
| Anthropic Claude | 1,000-4,000 | 80K-400K | No hard limit (HTTP) |
| Google Gemini | 360-1,000 | 120K-4M | No hard limit (HTTP) |

*Limits vary by tier and account. Check your provider dashboard for exact numbers.*

### How Rate Limits Affect Capacity

The effective conversation throughput is bounded by:

```
max_turns_per_minute = provider_RPM
max_concurrent_active = provider_RPM * avg_response_time_seconds / 60
```

**Example:** OpenAI GPT-4 at 500 RPM with 3-second average response time:
- max_concurrent_active = 500 * 3 / 60 = 25 conversations actively generating at once
- The other conversations are idle (waiting for user input)

For a chat application where users send a message every 30 seconds on average:
```
sustainable_conversations = provider_RPM * avg_user_think_time / 60
                          = 500 * 30 / 60 = 250 concurrent conversations
```

### Built-In Rate Limiting

PromptKit has built-in rate limiting on `BaseProvider`:
- Configurable via `SetRateLimit(rps float64, burst int)`
- Uses `golang.org/x/time/rate` token bucket algorithm
- Set this below your provider's limit to avoid 429 errors

### Connection Pooling

Default HTTP transport settings (per provider instance):
- `MaxIdleConns = 1000`
- `MaxIdleConnsPerHost = 100`
- `MaxConnsPerHost = 100`
- `IdleConnTimeout = 90s`
- HTTP/2 enabled (multiplexing multiple requests over fewer TCP connections)

**Recommendation:** Share provider instances across conversations via `WithProvider()`. This shares the connection pool and rate limiter, preventing per-conversation connection overhead and ensuring rate limits are enforced globally.

### Retry Behavior

Built-in retry with exponential backoff:
- Default: 3 retries, 500ms initial delay
- Retries on 429, 502, 503, 504, and network errors
- Respects `Retry-After` headers from providers
- Budget for retry overhead: ~2x provider calls during degraded conditions

---

## Quick Reference: Deployment Sizing

| Deployment Size | Conversations | Recommended Pod Spec | Pods | Key Settings |
|----------------|--------------|---------------------|------|--------------|
| Small | Up to 500 | 2 CPU, 4 GB RAM | 1-2 | MemoryStore OK for single-pod |
| Medium | 500-5,000 | 4 CPU, 8 GB RAM | 2-4 | Redis state store, shared EventBus + provider |
| Large | 5,000-50,000 | 8 CPU, 16 GB RAM | 4-10 | Redis state store, shared everything, cloud storage |
| Audio (ASM) | Up to 5,000 | 8 CPU, 32 GB RAM | 4-8 | Multi-gigabit network, shared EventBus |
| Audio (VAD) | Up to 2,000 | 8 CPU, 32 GB RAM | 4-10 | Higher CPU for VAD/resample, shared EventBus |

### OS Tuning for Large Deployments

```bash
ulimit -n 65536            # file descriptors
sysctl net.core.somaxconn=32768
sysctl net.ipv4.tcp_tw_reuse=1
```

### Key SDK Options for Production

```go
// Share resources across conversations
sharedBus := events.NewBus()
sharedProvider := openai.New(apiKey)
sharedStateStore := statestore.NewRedisStore(redisClient)
shutdownMgr := sdk.NewShutdownManager()

conv, _ := sdk.Open(pack,
    sdk.WithEventBus(sharedBus),
    sdk.WithProvider(sharedProvider),
    sdk.WithStateStore(sharedStateStore),
    sdk.WithShutdownManager(shutdownMgr),
)
```
