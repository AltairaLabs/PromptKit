---
title: Media Storage and Memory Management
docType: explanation
order: 5
---

Understanding the design, architecture, and trade-offs of PromptKit's media externalization system.

## The Problem

Modern LLM applications increasingly work with multimodal content—images, audio, and video. However, media content presents significant challenges:

### Memory Pressure

**Base64-encoded media is enormous:**

- Small image (800×600 JPEG): ~100-200 KB
- High-res image (1920×1080 PNG): 1-5 MB
- Audio clip (30 seconds MP3): 500 KB - 1 MB
- Video segment (10 seconds MP4): 5-20 MB

**In a typical conversation:**

```text
User uploads image (2 MB)
→ Stored in message history (2 MB in memory)

Assistant generates image (3 MB)
→ Added to history (5 MB total in memory)

User uploads another image (1.5 MB)
→ History grows (6.5 MB in memory)

Assistant analyzes both images
→ Both images in context (6.5 MB in memory)

After 10 turns with media: 20-50 MB per conversation in memory
```

**At scale:**

- 100 concurrent users: 2-5 GB just for conversation state
- 1,000 concurrent users: 20-50 GB just for conversation state
- Most of this is redundant base64 data sitting in memory

### Performance Degradation

Large conversation states cause:

1. **Serialization overhead**: Encoding/decoding gigabytes of base64
2. **Network overhead**: Transferring state to/from stores (Redis, Postgres)
3. **GC pressure**: Garbage collector struggles with large objects
4. **Memory fragmentation**: Large allocations fragment heap
5. **Context switching**: More time in system calls, less in application logic

### Cost Implications

**Memory costs money:**

- Cloud VMs: ~$10/month per GB of RAM
- 50 GB for media state: $500/month just for RAM
- Kubernetes pods: Memory limits force expensive pod sizes
- Serverless: Higher memory = higher cost per invocation

**Network costs:**

- Redis network egress: $0.05-0.10 per GB
- Transferring 50 GB/day in conversation state: $75-150/month
- Cross-AZ transfers: Even more expensive

## The Solution: Media Externalization

PromptKit's media storage system solves these problems through **automatic externalization**—storing large media content on disk (or object storage) instead of keeping it inline in conversation state.

### Core Concept

Instead of storing media data inline:

```go
// Without externalization
message := Message{
    Content: []Content{
        {
            Type: "image",
            Media: &MediaContent{
                Data: "iVBORw0KGgoAAAANSUhEUg..." // 2 MB base64 string
            },
        },
    },
}
// Total message size: ~2 MB
```

Store a reference:

```go
// With externalization
message := Message{
    Content: []Content{
        {
            Type: "image",
            Media: &MediaContent{
                StorageReference: &StorageReference{
                    ID:      "abc123-def456-ghi789",
                    Backend: "file",
                    // ... metadata
                },
                Data: "", // Empty - data is on disk
            },
        },
    },
}
// Total message size: ~200 bytes
```

Memory savings: 99.99%

## Architecture

### Component Overview

```text
┌─────────────────────────────────────────────────┐
│           Conversation Manager                  │
│  (SDK high-level API)                          │
└────────────────┬────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────┐
│              Pipeline                           │
│  ┌──────────────────────────────────────────┐  │
│  │  1. Context Builder                      │  │
│  │  2. Provider Middleware (LLM call)       │  │
│  │  3. MediaExternalizer ◄─────────────────┐│  │
│  │  4. State Persistence                    ││  │
│  └──────────────────────────────────────────┘│  │
└───────────────────────────────────┬────────┬───┘
                                    │        │
                        ┌───────────┘        └───────────┐
                        ▼                                ▼
           ┌────────────────────────┐      ┌─────────────────────┐
           │  MediaStorageService   │      │    StateStore       │
           │  (e.g., FileStore)     │      │  (e.g., Redis)      │
           └────────────────────────┘      └─────────────────────┘
                        │                               │
                        ▼                               ▼
           ┌────────────────────────┐      ┌─────────────────────┐
           │    Disk Storage        │      │   Conversation      │
           │  /var/lib/media/...    │      │   State (small)     │
           │  ├── abc123.png (2 MB) │      │   └── messages:     │
           │  └── def456.mp3 (1 MB) │      │       └── ref: abc  │
           └────────────────────────┘      └─────────────────────┘
```

### Key Components

#### 1. MediaExternalizer Middleware

Runs **after** the LLM provider returns a response:

```go
func (m *MediaExternalizer) Process(ctx context.Context, state *types.State) error {
    for _, message := range state.Response.Messages {
        for _, content := range message.Content {
            if content.Media == nil {
                continue
            }
            
            // Check size threshold
            size := len(content.Media.Data) * 3 / 4 // base64 → bytes
            if size < m.threshold {
                continue // Keep small media in memory
            }
            
            // Store to external storage
            ref, err := m.storage.Store(ctx, content.Media, metadata)
            if err != nil {
                return err
            }
            
            // Replace data with reference
            content.Media.StorageReference = ref
            content.Media.Data = "" // Clear inline data (memory saved!)
        }
    }
    return nil
}
```

**Key behaviors:**

- Only externalizes media above threshold (default: 100 KB)
- Preserves small media inline (thumbnails, icons)
- Runs after provider to avoid affecting LLM calls
- Atomic: Either all media externalized or none

#### 2. MediaStorageService Interface

Abstracts storage backends:

```go
type MediaStorageService interface {
    Store(ctx context.Context, media *MediaContent, metadata MediaMetadata) (*StorageReference, error)
    Retrieve(ctx context.Context, ref *StorageReference) (*MediaContent, error)
    Delete(ctx context.Context, ref *StorageReference) error
}
```

**Design principles:**

- **Interface-based**: Swap backends without code changes
- **Context-aware**: Supports cancellation and timeouts
- **Metadata-rich**: Carries conversation/session/run context
- **Error-typed**: Specific errors for not-found, permission, etc.

#### 3. FileStore Implementation

Local filesystem storage with advanced features:

**Features:**

- **Organization modes**: Flat, by-conversation, by-session, by-run
- **Deduplication**: Content-based (SHA-256) with reference counting
- **Atomic writes**: Temp file + rename for crash safety
- **Metadata files**: `.meta` sidecar files for context
- **Index persistence**: Deduplication index survives restarts

**File layout (by-session mode):**

```text
media/
  session-abc123/
    conv-def456/
      abc123-ghi789-jkl012.png      ← Actual image
      abc123-ghi789-jkl012.png.meta ← Metadata (JSON)
    conv-mno345/
      def456-pqr789-stu012.mp3
      def456-pqr789-stu012.mp3.meta
  .dedup-index.json                  ← Deduplication index
```

#### 4. MediaLoader

Unified media access across all sources:

```go
type MediaLoader struct {
    storageService MediaStorageService
    httpClient     *http.Client
}

func (l *MediaLoader) GetBase64Data(ctx context.Context, media *MediaContent) (string, error) {
    // Priority order
    if media.Data != "" {
        return media.Data, nil // Already inline
    }
    if media.StorageReference != nil {
        return l.loadFromStorage(ctx, media.StorageReference)
    }
    if media.FilePath != "" {
        return l.loadFromFile(media.FilePath)
    }
    if media.URL != "" {
        return l.loadFromURL(ctx, media.URL)
    }
    return "", ErrNoMediaSource
}
```

**Priority order matters:**

1. **Data**: If present, use it (already in memory)
2. **StorageReference**: Load from configured storage backend
3. **FilePath**: Load from local file system
4. **URL**: Fetch from HTTP/HTTPS

This allows providers to transparently access media regardless of source.

## When Media is Externalized

### Size Threshold

Default: 100 KB

```go
// Configured in manager
manager, _ := sdk.NewConversationManager(
    sdk.WithMediaStorage(fileStore),
    sdk.WithConfig(sdk.ManagerConfig{
        MediaSizeThresholdKB: 100, // Only externalize if > 100 KB
    }),
)
```

**Why 100 KB?**

- **Small media stays fast**: Thumbnails, icons, small images stay inline
- **Large media externalizes**: Photos, generated images, audio/video
- **Reduces I/O**: Don't externalize tiny media (overhead not worth it)
- **Balances memory**: 100 KB × 10 messages = 1 MB (acceptable in memory)

**Tuning threshold:**

- **Lower (50 KB)**: More aggressive, lower memory, more I/O
- **Higher (500 KB)**: Less aggressive, higher memory, less I/O
- **Zero (0 KB)**: Externalize everything (maximum memory savings)

### Automatic Detection

Externalization happens automatically:

1. **Provider returns response** with media (e.g., DALL-E image)
2. **MediaExternalizer middleware** runs after provider
3. **Checks each media** in response messages
4. **Calculates size** (base64 length × 3/4)
5. **If above threshold**: Store + replace with reference
6. **If below threshold**: Keep inline
7. **State persisted** with references instead of data

**No application code changes needed.**

## Storage Reference Lifecycle

### Creation

```go
// 1. Provider returns image
response := provider.SendMessage(ctx, "Generate image of sunset")
// response.Messages[0].Content[0].Media.Data = "iVBORw0..." (2 MB)

// 2. MediaExternalizer processes
ref, _ := storage.Store(ctx, media, metadata)
// Writes: /media/session-xyz/conv-abc/abc123-def456.png

// 3. Reference replaces data
media.StorageReference = ref
media.Data = "" // Cleared - memory saved!

// 4. State persisted
stateStore.Save(ctx, conversationID, state)
// Only reference saved, not 2 MB of data
```

### Usage

When media is needed (e.g., sending to LLM):

```go
// Provider needs media data
mediaLoader := providers.NewMediaLoader(providers.MediaLoaderConfig{
    StorageService: fileStore,
})

// Load from storage
data, _ := mediaLoader.GetBase64Data(ctx, media)
// Reads from disk, returns base64

// Send to LLM
provider.SendMessage(ctx, "Analyze this image", data)
```

**Key insight**: Media loaded on-demand, not kept in memory permanently.

### Cleanup

**Current behavior:**

- Media retained indefinitely (default policy: "retain")
- Manual cleanup required for production

**Future behavior:**

- Policy-based cleanup (e.g., "delete after 30 days")
- Reference counting for safe deletion
- Automatic cleanup when conversation deleted

## Deduplication

### How It Works

**Content-based hashing:**

```text
User uploads logo.png (100 KB)
→ SHA-256: abc123...def456
→ Store: media/abc123...def456.png
→ Index: { "abc123...": { refCount: 1, path: "..." } }

Different user uploads same logo.png
→ SHA-256: abc123...def456 (same hash!)
→ Found in index: Increment refCount to 2
→ No new file written
→ Return reference to existing file

User 1 deletes conversation
→ Decrement refCount to 1
→ Keep file (still referenced)

User 2 deletes conversation
→ Decrement refCount to 0
→ Delete file (no more references)
```

**Benefits:**

- **Storage savings**: 30-70% typical, up to 90% for repeated media
- **Automatic**: No configuration needed
- **Safe**: Reference counting prevents premature deletion
- **Fast**: Hash-based lookups O(1)

**Trade-offs:**

- **Hash overhead**: SHA-256 computation (~10-20ms for 1 MB)
- **Index file**: Shared state requires locking
- **Best for**: Repeated media (profile images, templates, logos)

### When to Enable

**Enable if you have:**

- User profile images (same image across many conversations)
- Template images or logos
- Generated images with similar prompts (AI often generates similar content)
- Media uploaded multiple times

**Disable if you have:**

- Unique media every time
- Very high throughput (avoid hash overhead)
- Simple, short-lived applications

## Trade-offs and Design Decisions

### Memory vs. I/O

**Without Externalization:**

- ✅ Fast: No disk I/O needed
- ✅ Simple: Everything in memory
- ❌ Memory: 20-50 MB per conversation with media
- ❌ Scale: Limited by RAM

**With Externalization:**

- ✅ Memory: 99% reduction (references only)
- ✅ Scale: Thousands of concurrent conversations
- ❌ I/O: Disk reads when media needed
- ❌ Complexity: Storage backend configuration

**Decision:** Memory is more scarce and expensive than disk I/O. Most applications benefit from externalization.

### Inline vs. Reference

**Small media (< 100 KB) stays inline:**

- Thumbnails, icons, emoji images
- Small audio clips
- Low-res images

**Why?**

- I/O overhead not worth it
- Small enough to keep in memory
- Simplifies provider code (no loading needed)

**Large media (> 100 KB) externalizes:**

- Photos, high-res images
- Generated images
- Audio/video files

**Why?**

- Memory savings worth the I/O
- Prevents memory pressure
- Enables scaling

### Eager vs. Lazy Loading

#### Current: Lazy Loading

Media loaded only when needed:

```go
// State restored from Redis
state := stateStore.Get(ctx, conversationID)
// Media.Data is empty, only references

// Later: Provider needs media
data, _ := mediaLoader.GetBase64Data(ctx, media)
// Now loaded from disk
```

**Benefits:**

- Minimal memory at rest
- Fast state restoration
- Only pay I/O cost when needed

#### Alternative: Eager Loading (Not Implemented)

Load all media when restoring state:

```go
state := stateStore.Get(ctx, conversationID)
// Immediately load all media from storage
for _, msg := range state.Messages {
    for _, content := range msg.Content {
        if content.Media.StorageReference != nil {
            content.Media.Data = loadFromStorage(...)
        }
    }
}
```

**Trade-offs:**

- ✅ No loading needed during conversation
- ❌ Higher memory usage
- ❌ Slower state restoration
- ❌ Loading media that may never be used

**Decision:** Lazy loading is better for most cases. Providers can pre-load if needed.

### Organization Strategies

**Flat:**

```text
media/abc123.png
```

- ✅ Simplest structure
- ✅ Fastest lookups
- ❌ Hard to browse/manage at scale

**By-Conversation:**

```text
media/conv-abc123/image.png
```

- ✅ Easy per-conversation cleanup
- ✅ Clear ownership
- ❌ Duplicates if same media in multiple conversations

**By-Session:**

```text
media/session-xyz/conv-abc/image.png
```

- ✅ Groups related conversations
- ✅ Good for multi-turn sessions
- ✅ Recommended default

**By-Run:**

```text
media/run-20241124/session-xyz/conv-abc/image.png
```

- ✅ Perfect for test isolation (Arena)
- ✅ Clear time-based organization
- ❌ Most complex structure

**Decision:** By-session is best default. By-run for testing. User choice for specific needs.

## Integration with Pipeline

### Middleware Order Matters

```go
pipeline := []Middleware{
    ContextBuilder,      // 1. Build context from history
    ProviderMiddleware,  // 2. Call LLM (may return media)
    MediaExternalizer,   // 3. Externalize media in response ← Must be after provider!
    StatePersistence,    // 4. Save state (with references)
}
```

**Why after provider?**

- Provider returns media inline (from LLM)
- Externalizer processes response before persisting
- State saved with references, not inline data

**Why before state?**

- Must externalize before saving to state store
- Otherwise, large media written to Redis/Postgres
- Defeats purpose of externalization

### Arena Integration

Arena automatically uses media storage:

```yaml
# .promptarena.yaml
output_dir: ./out
```

**Behavior:**

- Media stored in `./out/media/`
- Organization: by-run (each test isolated)
- Deduplication: enabled
- Statistics: included in results (future)

**Benefits:**

- Test artifacts preserved
- Easy comparison across runs
- Disk usage tracked
- Reproducible test results

## Performance Characteristics

### FileStore Benchmarks

**Write performance:**

- 100 KB image: ~1-2 ms
- 1 MB image: ~5-10 ms
- 5 MB video: ~20-30 ms
- Dedup hash (1 MB): ~10-15 ms

**Read performance:**

- 100 KB image: ~1 ms
- 1 MB image: ~3-5 ms
- 5 MB video: ~15-20 ms

**Storage overhead:**

- Metadata: ~500 bytes per file
- Index: ~200 bytes per unique file
- Directory: ~4 KB per directory

### Scaling Characteristics

**Memory usage:**

- Without externalization: O(n × m) where n = conversations, m = avg media size
- With externalization: O(n × r) where r = reference size (~200 bytes)
- Example: 1000 conversations with 5 images each (1 MB avg)
  - Without: 1000 × 5 × 1 MB = 5 GB
  - With: 1000 × 5 × 200 bytes = 1 MB (5000× reduction)

**Disk usage:**

- Without dedup: Total size of all media
- With dedup: Total size of unique media
- Example: 1000 users with same profile image (100 KB)
  - Without: 1000 × 100 KB = 100 MB
  - With: 1 × 100 KB = 100 KB (1000× reduction)

## Best Practices

### When to Use Media Storage

✅ **Always use in production** for multimodal applications

✅ **Use for development** to match production behavior

✅ **Use in tests** (Arena does this automatically)

❌ **Don't use** for text-only applications (no benefit)

❌ **Don't use** for media you need in-memory (rare)

### Configuration Guidelines

**Threshold:**

- Start with 100 KB (default)
- Monitor memory usage
- Lower if memory-constrained
- Raise if I/O-bound

**Organization:**

- Use by-session for most applications
- Use by-run for batch processing
- Use by-conversation for per-chat cleanup

**Deduplication:**

- Enable for production (storage savings)
- Enable if you have repeated media
- Disable for testing (simpler debugging)

### Monitoring

**Track these metrics:**

- Disk usage (df -h)
- File count (find | wc -l)
- Externalization rate (media externalized / total media)
- Dedup savings (unique files / total files)
- Memory usage (RSS, heap size)

## Future Enhancements

### Planned Features

#### 1. Cloud Storage Backends

- S3, GCS, Azure Blob Storage
- Better scalability
- Geographic distribution

#### 2. Automatic Cleanup

- Policy-based retention
- Time-based expiration
- Size-based pruning

#### 3. Media Compression

- Automatic image optimization
- Format conversion
- Quality adjustment

#### 4. Caching Layer

- In-memory LRU cache
- Hot media stays cached
- Reduces disk I/O

#### 5. Async Externalization

- Background processing
- Non-blocking pipeline
- Faster response times

## Conclusion

Media storage is a critical feature for production multimodal applications. By automatically externalizing large media content, PromptKit enables:

- **95-99% memory reduction** for media-heavy applications
- **Horizontal scaling** without memory constraints
- **30-70% storage savings** with deduplication
- **Production-ready** performance and reliability

The design balances performance, simplicity, and flexibility—making it easy to use while providing power-user customization.

## See Also

- **[How-To: Configure Media Storage](../how-to/configure-media-storage)** - Configuration guide
- **[Tutorial: Working with Images](../tutorials/06-media-storage)** - Hands-on walkthrough
- **[Storage Reference](../../runtime/reference/storage)** - Complete API reference
- **[Types Reference](../../runtime/reference/types#mediacontent)** - MediaContent structure
