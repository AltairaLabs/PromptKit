---
title: Configure Media Storage
docType: how-to
order: 10
---

Learn how to configure media externalization to optimize memory usage and performance when working with images, audio, and video content.

## Overview

Media externalization automatically stores large media content (images, audio, video) to disk instead of keeping them in memory as base64 data. This significantly reduces memory usage and improves performance for media-heavy workflows.

**When to Use:**

- Working with images (generated images, uploaded photos)
- Processing audio or video content
- Long conversations with multiple media attachments
- Production applications handling media at scale

**Benefits:**

- Reduces memory footprint by 70-90% for media-heavy applications
- Automatic deduplication saves storage space
- Improves response times and throughput
- Configurable retention policies for cleanup

## Prerequisites

```bash
go get github.com/AltairaLabs/PromptKit/sdk
```

## Quick Start

### Enable Media Storage

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/storage/local"
)

// 1. Create file store
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir: "./media",  // Storage directory
})

// 2. Enable media storage
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(fileStore),  // Enables externalization
)
```

That's it! Media larger than 100KB is now automatically externalized.

## Configuration Options

### Storage Location

Choose where to store media files:

```go
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir: "./media",  // All media files stored here
})
```

**Recommended Locations:**

- Development: `./media` or `./tmp/media`
- Production: `/var/lib/myapp/media` or mounted volume
- Docker: `/app/media` with volume mount

### Organization Modes

Control how media files are organized:

```go
import "github.com/AltairaLabs/PromptKit/runtime/storage/local"

fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:      "./media",
    Organization: local.OrganizationBySession,  // Default
})
```

**Available Modes:**

**`OrganizationFlat`** - All files in one directory

```text
media/
  abc123_image.png
  def456_audio.mp3
  ghi789_video.mp4
```

- Use for: Simple applications, small media volumes
- Pros: Simple structure
- Cons: Hard to browse with many files

**`OrganizationByConversation`** - Organized by conversation ID

```text
media/
  conv-abc123/
    image1.png
    image2.png
  conv-def456/
    audio.mp3
```

- Use for: Per-conversation analysis or cleanup
- Pros: Easy to manage conversation-specific media
- Cons: Duplicates if same media in multiple conversations

**`OrganizationBySession`** - Organized by session ID (default)

```text
media/
  session-xyz789/
    conv-abc123/
      image1.png
    conv-def456/
      audio.mp3
```

- Use for: Multi-conversation sessions
- Pros: Groups related conversations together
- Cons: More complex structure

**`OrganizationByRun`** - Organized by run ID (Arena default)

```text
media/
  run-20241124-123456/
    session-xyz/
      conv-abc/
        image.png
```

- Use for: Test runs, batch processing
- Pros: Perfect for test isolation
- Cons: Most complex structure

### Size Threshold

Control when media is externalized:

```go
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(fileStore),
    sdk.WithConfig(sdk.ManagerConfig{
        MediaSizeThresholdKB: 50,  // Externalize media > 50KB
    }),
)
```

**Recommended Thresholds:**

- **100KB (default)**: Good balance for most applications
- **50KB**: More aggressive externalization, lower memory
- **500KB**: Keep smaller media in memory, less I/O
- **0**: Externalize all media (maximum memory savings)

### Deduplication

Enable content-based deduplication to save storage:

```go
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:            "./media",
    EnableDeduplication: true,  // Default: false
})
```

**How It Works:**

- Content hashed with SHA-256
- Identical content stored once
- Reference counting tracks usage
- Safe deletion when no references remain

**Benefits:**

- 30-70% storage savings in typical scenarios
- Automatic duplicate detection
- No configuration needed

**Trade-offs:**

- Slight overhead for hash computation
- Shared index file (`.dedup-index.json`)
- Best for applications with repeated media

### Retention Policy

Configure automatic cleanup:

```go
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(fileStore),
    sdk.WithConfig(sdk.ManagerConfig{
        MediaDefaultPolicy: "retain",  // or "delete"
    }),
)
```

**Policies:**

- **`retain`** (default): Keep media files indefinitely
- **`delete`**: Delete media when conversation ends

**Note:** Automatic cleanup not yet implemented. Manual cleanup recommended for production.

## Complete Examples

### Production Configuration

```go
package main

import (
    "log"
    "os"
    
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/storage/local"
)

func initManager() (*sdk.ConversationManager, error) {
    // 1. Provider
    provider := providers.NewOpenAIProvider(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4o",
        false,
    )
    
    // 2. Media storage with deduplication
    fileStore := local.NewFileStore(local.FileStoreConfig{
        BaseDir:             "/var/lib/myapp/media",
        Organization:        local.OrganizationBySession,
        EnableDeduplication: true,
    })
    
    // 3. Manager with storage configuration
    return sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithMediaStorage(fileStore),
        sdk.WithConfig(sdk.ManagerConfig{
            MediaSizeThresholdKB: 100,
            MediaDefaultPolicy:   "retain",
        }),
    )
}
```

### Image Generation Example

```go
func generateImage(manager *sdk.ConversationManager, userID, prompt string) error {
    ctx := context.Background()
    
    // Create conversation
    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     userID,
        SessionID:  "session-" + userID,
        PromptName: "image-generator",
    })
    if err != nil {
        return err
    }
    
    // Request image generation
    resp, err := conv.Send(ctx, prompt)
    if err != nil {
        return err
    }
    
    // Media automatically externalized if > 100KB
    for _, msg := range resp.Messages {
        for _, content := range msg.Content {
            if content.Type == "image" && content.Media != nil {
                if content.Media.StorageReference != nil {
                    log.Printf("Image externalized to: %s", 
                        content.Media.StorageReference.ID)
                }
            }
        }
    }
    
    return nil
}
```

### Multimodal Chatbot

```go
func handleImageUpload(manager *sdk.ConversationManager, userID, imageData, question string) (string, error) {
    ctx := context.Background()
    
    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     userID,
        PromptName: "vision-assistant",
    })
    if err != nil {
        return "", err
    }
    
    // Send image with question
    resp, err := conv.SendWithMedia(ctx, question, []sdk.MediaContent{
        {
            Type:     "image",
            MimeType: "image/jpeg",
            Data:     imageData,  // Base64 encoded
        },
    })
    if err != nil {
        return "", err
    }
    
    // Large image automatically externalized
    return resp.Content, nil
}
```

## Docker Integration

### Volume Mount

```dockerfile
FROM golang:1.23-alpine

WORKDIR /app

# Create media directory
RUN mkdir -p /app/media

COPY . .
RUN go build -o myapp

# Volume for persistent media storage
VOLUME /app/media

CMD ["./myapp"]
```

### Docker Compose

```yaml
version: '3.8'

services:
  app:
    build: .
    volumes:
      - media-storage:/app/media
    environment:
      - OPENAI_API_KEY=${OPENAI_API_KEY}

volumes:
  media-storage:
```

### Configuration

```go
// Use environment variable for BaseDir
mediaDir := os.Getenv("MEDIA_DIR")
if mediaDir == "" {
    mediaDir = "./media"  // Default for development
}

fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir: mediaDir,
})
```

## Monitoring and Troubleshooting

### Check Storage Usage

```bash
# Total storage used
du -sh ./media

# Files per conversation
du -sh ./media/session-*/conv-*

# Largest files
find ./media -type f -exec du -h {} + | sort -rh | head -20
```

### Verify Externalization

```go
// Check if media was externalized
for _, msg := range resp.Messages {
    for _, content := range msg.Content {
        if content.Media != nil {
            if content.Media.StorageReference != nil {
                log.Printf("✓ Externalized: %s", content.Media.StorageReference.ID)
            } else if content.Media.Data != "" {
                log.Printf("✗ In-memory: %d bytes", len(content.Media.Data))
            }
        }
    }
}
```

### Debug Deduplication

```bash
# View deduplication index
cat ./media/.dedup-index.json | jq

# Check for duplicates
find ./media -type f -name "*.png" -exec sha256sum {} \; | \
    awk '{print $1}' | sort | uniq -c | sort -rn
```

### Common Issues

**Media Not Externalizing:**

```go
// Check threshold
config := sdk.ManagerConfig{
    MediaSizeThresholdKB: 100,  // Ensure threshold is reasonable
}

// Verify storage service is set
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(fileStore),  // Required!
)
```

**Permission Errors:**

```bash
# Ensure write permissions
chmod 755 ./media

# Check ownership (Docker)
chown -R 1000:1000 ./media
```

**Disk Space:**

```bash
# Monitor disk usage
df -h

# Clean old sessions (manual)
find ./media -type d -name "session-*" -mtime +30 -exec rm -rf {} \;
```

## Performance Optimization

### Choose Optimal Threshold

Test different thresholds for your use case:

```go
// Test configuration
thresholds := []int64{0, 50, 100, 200, 500}

for _, threshold := range thresholds {
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithMediaStorage(fileStore),
        sdk.WithConfig(sdk.ManagerConfig{
            MediaSizeThresholdKB: threshold,
        }),
    )
    
    // Run benchmark
    runBenchmark(manager)
}
```

**Guidelines:**

- Lower threshold = More I/O, less memory
- Higher threshold = Less I/O, more memory
- Monitor: Response time, memory usage, disk I/O

### Enable Deduplication

Enable if you have:

- Repeated media across conversations
- User profile images
- Standard templates or logos
- Generated images with similar prompts

Disable if you have:

- Unique media every time
- Very high throughput (avoid hash overhead)
- Simple, short-lived applications

### Optimize Organization Mode

**For throughput:**

- Use `OrganizationFlat` - Fastest lookups
- Disable deduplication - No hash overhead

**For manageability:**

- Use `OrganizationBySession` - Balance of performance and organization
- Enable deduplication - Storage savings

**For analysis:**

- Use `OrganizationByConversation` - Easy per-conversation access
- Keep metadata for analysis

## Migration from In-Memory

### Step 1: Add Storage Service

```go
// Before
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
)

// After
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir: "./media",
})

manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(fileStore),  // Add this
)
```

### Step 2: Test with Existing Code

Your existing code works unchanged! Media is automatically externalized:

```go
// This code doesn't need to change
resp, err := conv.Send(ctx, "Generate an image of a sunset")
// Image automatically externalized if > 100KB
```

### Step 3: Gradual Rollout

```go
// Feature flag for gradual rollout
var fileStore storage.MediaStorageService
if os.Getenv("ENABLE_MEDIA_EXTERNALIZATION") == "true" {
    fileStore = local.NewFileStore(local.FileStoreConfig{
        BaseDir: "./media",
    })
}

manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(fileStore),  // nil = disabled
)
```

## Cleanup Strategies

### Manual Cleanup

```bash
# Delete sessions older than 30 days
find ./media -type d -name "session-*" -mtime +30 -exec rm -rf {} \;

# Delete specific session
rm -rf ./media/session-xyz789
```

### Scheduled Cleanup (cron)

```bash
# Add to crontab
0 2 * * * find /var/lib/myapp/media -type d -name "session-*" -mtime +30 -delete
```

### Application-Level Cleanup

```go
func cleanupOldMedia(baseDir string, maxAgeDays int) error {
    cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
    
    sessions, err := filepath.Glob(filepath.Join(baseDir, "session-*"))
    if err != nil {
        return err
    }
    
    for _, session := range sessions {
        info, err := os.Stat(session)
        if err != nil {
            continue
        }
        
        if info.ModTime().Before(cutoff) {
            log.Printf("Removing old session: %s", session)
            os.RemoveAll(session)
        }
    }
    
    return nil
}

// Run periodically
go func() {
    ticker := time.NewTicker(24 * time.Hour)
    for range ticker.C {
        cleanupOldMedia("./media", 30)
    }
}()
```

## Best Practices

### ✅ Do

- **Enable in production** for media-heavy applications
- **Use deduplication** if you have repeated media
- **Monitor disk usage** with alerts
- **Mount volumes** in Docker/Kubernetes
- **Set reasonable thresholds** (100KB is good default)
- **Use by-session organization** for most applications
- **Plan cleanup strategy** before going to production

### ❌ Don't

- **Don't externalize tiny media** (< 10KB)
- **Don't forget permissions** in production
- **Don't ignore disk space** monitoring
- **Don't delete media** while conversations are active
- **Don't share media directories** between environments
- **Don't hardcode paths** - use environment variables

## Advanced Topics

### Custom Storage Backend

Implement `storage.MediaStorageService` interface:

```go
type MediaStorageService interface {
    Store(ctx context.Context, media *types.MediaContent, metadata MediaMetadata) (*StorageReference, error)
    Retrieve(ctx context.Context, ref *StorageReference) (*types.MediaContent, error)
    Delete(ctx context.Context, ref *StorageReference) error
}
```

Examples: S3, Google Cloud Storage, Azure Blob Storage

See: [Custom Storage Backends Guide](custom-storage-backends) (coming soon)

### Accessing Externalized Media

Use `MediaLoader` for unified access:

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/storage/local"
)

// Create loader
loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
    StorageService: fileStore,
    HTTPTimeout:    30 * time.Second,
})

// Load media from any source (Data, StorageReference, FilePath, URL)
data, err := loader.GetBase64Data(ctx, media)
```

See: [Runtime Providers Reference](../../runtime/reference/providers)

## Next Steps

- **[Tutorial: Working with Images](../tutorials/06-media-storage)** - Complete walkthrough
- **[Storage Reference](../../runtime/reference/storage)** - API documentation
- **[MediaLoader Reference](../../runtime/reference/providers#medialoader)** - Unified media access
- **[Types Reference](../../runtime/reference/types#mediacontent)** - MediaContent structure

## See Also

- [SDK Reference: ConversationManager](../reference/conversation-manager)
- [Explanation: Media Storage Design](../explanation/media-storage)
- [Arena Media Documentation](../../arena/reference/cli-commands#media-storage)
- [Example: media-storage](https://github.com/AltairaLabs/PromptKit/tree/main/examples/sdk/media-storage)
