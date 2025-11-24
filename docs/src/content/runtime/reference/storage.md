---
title: Storage Package
docType: reference
order: 8
---

The storage package provides interfaces and implementations for persistent storage of media content, enabling efficient memory management for media-heavy applications.

## Package Overview

```go
import "github.com/AltairaLabs/PromptKit/runtime/storage"
import "github.com/AltairaLabs/PromptKit/runtime/storage/local"
```

The storage system enables automatic externalization of large media content (images, audio, video) from memory to persistent storage. This significantly reduces memory footprint while maintaining transparent access to media.

## Core Interface

### MediaStorageService

The primary interface for media storage implementations.

```go
type MediaStorageService interface {
    // Store saves media content and returns a reference
    Store(ctx context.Context, media *types.MediaContent, metadata MediaMetadata) (*StorageReference, error)
    
    // Retrieve loads media content from storage
    Retrieve(ctx context.Context, ref *StorageReference) (*types.MediaContent, error)
    
    // Delete removes media content from storage
    Delete(ctx context.Context, ref *StorageReference) error
}
```

**Methods:**

#### Store

Stores media content and returns a reference for later retrieval.

```go
ref, err := storageService.Store(ctx, media, metadata)
```

**Parameters:**

- `ctx`: Context for cancellation and timeout
- `media`: MediaContent containing data to store
- `metadata`: Additional metadata (ConversationID, SessionID, etc.)

**Returns:**

- `*StorageReference`: Reference to stored media
- `error`: Storage errors (permission, disk space, etc.)

**Behavior:**

- Writes media to persistent storage
- Generates unique identifier
- Stores metadata for organization
- Returns reference for retrieval

#### Retrieve

Loads media content from storage using a reference.

```go
media, err := storageService.Retrieve(ctx, ref)
```

**Parameters:**

- `ctx`: Context for cancellation and timeout
- `ref`: StorageReference from previous Store call

**Returns:**

- `*types.MediaContent`: Loaded media with Data field populated
- `error`: Retrieval errors (not found, permission, corrupted)

**Behavior:**

- Locates media by reference ID
- Reads content from storage
- Populates MediaContent.Data field
- Preserves original MimeType and Type

#### Delete

Removes media content from storage.

```go
err := storageService.Delete(ctx, ref)
```

**Parameters:**

- `ctx`: Context for cancellation and timeout
- `ref`: StorageReference to delete

**Returns:**

- `error`: Deletion errors (not found, permission)

**Behavior:**

- Removes media file from storage
- Cleans up associated metadata
- Handles deduplication reference counting
- Idempotent (no error if already deleted)

## Types

### StorageReference

Reference to stored media content.

```go
type StorageReference struct {
    ID          string            // Unique identifier
    Backend     string            // Storage backend name ("file", "s3", etc.)
    Metadata    map[string]string // Backend-specific metadata
    CreatedAt   time.Time         // Creation timestamp
}
```

**Fields:**

- **ID**: Unique identifier for stored media (UUID, hash, or path)
- **Backend**: Storage backend type (implementation-specific)
- **Metadata**: Additional data for retrieval (path, bucket, region, etc.)
- **CreatedAt**: When media was stored

**Example:**

```go
ref := &storage.StorageReference{
    ID:       "abc123-def456-ghi789",
    Backend:  "file",
    Metadata: map[string]string{
        "path": "/var/lib/myapp/media/session-xyz/conv-123/abc123-def456-ghi789.png",
        "org":  "by-session",
    },
    CreatedAt: time.Now(),
}
```

### MediaMetadata

Contextual information for organizing stored media.

```go
type MediaMetadata struct {
    ConversationID string    // Conversation identifier
    SessionID      string    // Session identifier
    RunID          string    // Run identifier (Arena)
    UserID         string    // User identifier
    MessageIndex   int       // Position in conversation
    ContentIndex   int       // Position in message
    CreatedAt      time.Time // When media was created
}
```

**Fields:**

- **ConversationID**: Associates media with conversation
- **SessionID**: Groups media by session
- **RunID**: Groups media by test run (Arena)
- **UserID**: Associates media with user
- **MessageIndex**: Message position in conversation
- **ContentIndex**: Content position within message
- **CreatedAt**: Original creation time

**Usage:**

```go
metadata := storage.MediaMetadata{
    ConversationID: conv.ID,
    SessionID:      conv.SessionID,
    UserID:         conv.UserID,
    MessageIndex:   len(conv.Messages),
    ContentIndex:   i,
    CreatedAt:      time.Now(),
}
```

## FileStore Implementation

### Overview

Local filesystem implementation of MediaStorageService with support for deduplication, multiple organization modes, and atomic operations.

```go
import "github.com/AltairaLabs/PromptKit/runtime/storage/local"

fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:             "./media",
    Organization:        local.OrganizationBySession,
    EnableDeduplication: true,
})
```

### Configuration

#### FileStoreConfig

Configuration for FileStore instances.

```go
type FileStoreConfig struct {
    BaseDir             string         // Root directory for media storage
    Organization        OrganizationMode // How to organize files
    EnableDeduplication bool           // Enable content deduplication
}
```

**Fields:**

**BaseDir** - Root storage directory

- Absolute or relative path
- Created automatically if doesn't exist
- Requires write permissions
- Example: `./media`, `/var/lib/myapp/media`

**Organization** - File organization strategy

- Controls directory structure
- See [Organization Modes](#organization-modes)
- Default: `OrganizationBySession`

**EnableDeduplication** - Content-based deduplication

- Uses SHA-256 hash for identity
- Reference counting for safety
- Saves 30-70% storage typically
- Default: `false`

#### Organization Modes

Constants defining file organization strategies.

```go
const (
    OrganizationFlat            OrganizationMode = "flat"
    OrganizationByConversation  OrganizationMode = "by-conversation"
    OrganizationBySession       OrganizationMode = "by-session"
    OrganizationByRun           OrganizationMode = "by-run"
)
```

**OrganizationFlat** - All files in base directory

```text
media/
  abc123_image.png
  def456_audio.mp3
```

- Pros: Simple, fast lookups
- Cons: Hard to browse with many files
- Use: Small applications, temporary storage

**OrganizationByConversation** - Group by conversation

```text
media/
  conv-abc123/
    image1.png
    image2.png
  conv-def456/
    audio.mp3
```

- Pros: Easy per-conversation cleanup
- Cons: Duplicates if same media across conversations
- Use: Conversation-focused analysis

**OrganizationBySession** - Group by session (default)

```text
media/
  session-xyz789/
    conv-abc123/
      image1.png
    conv-def456/
      audio.mp3
```

- Pros: Groups related conversations, balanced structure
- Cons: More complex than flat
- Use: Multi-conversation sessions, production apps

**OrganizationByRun** - Group by run ID

```text
media/
  run-20241124-123456/
    session-xyz/
      conv-abc/
        image.png
```

- Pros: Perfect for test isolation
- Cons: Most complex structure
- Use: Arena tests, batch processing

### Methods

#### NewFileStore

Creates a new FileStore instance.

```go
func NewFileStore(config FileStoreConfig) *FileStore
```

**Parameters:**

- `config`: FileStoreConfig with BaseDir and options

**Returns:**

- `*FileStore`: Ready-to-use storage service

**Example:**

```go
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:             "./media",
    Organization:        local.OrganizationBySession,
    EnableDeduplication: true,
})
```

**Behavior:**

- Creates BaseDir if doesn't exist
- Initializes deduplication index if enabled
- Validates configuration
- Returns immediately (lazy initialization)

### Deduplication

FileStore supports content-based deduplication using SHA-256 hashing.

#### How It Works

1. **Store**: Hash content with SHA-256
2. **Check**: Look up hash in deduplication index
3. **Reuse**: If exists, increment reference count
4. **Save**: If new, store content and create index entry
5. **Delete**: Decrement reference count, delete when zero

#### Index Format

Stored in `.dedup-index.json` in BaseDir:

```json
{
  "sha256:abc123...": {
    "hash": "abc123...",
    "path": "session-xyz/conv-123/abc123.png",
    "size": 102400,
    "mime_type": "image/png",
    "ref_count": 3,
    "created_at": "2024-11-24T10:30:00Z",
    "last_accessed": "2024-11-24T12:45:00Z"
  }
}
```

#### Benefits

- **Storage Savings**: 30-70% reduction typically
- **Automatic**: No manual intervention needed
- **Safe**: Reference counting prevents premature deletion
- **Fast**: Hash-based lookups

#### Trade-offs

- **Hash Overhead**: SHA-256 computation cost
- **Index File**: Shared state, requires locking
- **Best For**: Repeated media (profile images, templates, similar generated images)

### File Naming

FileStore generates unique filenames using:

```text
{hash}-{uuid}.{extension}
```

Examples:

- `abc123-def456-ghi789.png`
- `xyz789-abc123-def456.mp4`
- `123456-789abc-def012.wav`

**Components:**

- **hash**: First 6 chars of content hash (dedup enabled) or random
- **uuid**: Shortened UUID for uniqueness
- **extension**: Derived from MIME type

### Atomic Operations

FileStore uses atomic writes to prevent corruption:

1. Write to temporary file (`.tmp` suffix)
2. Write metadata to separate file (`.meta` suffix)
3. Rename to final name (atomic operation)
4. Update deduplication index if enabled

This ensures:

- No partial writes visible to readers
- Crash safety (temp files cleaned up)
- Consistent state across restarts

### Error Handling

FileStore returns specific errors:

```go
var (
    ErrNotFound       = errors.New("media not found")
    ErrPermission     = errors.New("permission denied")
    ErrDiskSpace      = errors.New("insufficient disk space")
    ErrCorrupted      = errors.New("media file corrupted")
    ErrInvalidRef     = errors.New("invalid storage reference")
)
```

**Error Types:**

- **ErrNotFound**: Referenced media doesn't exist
- **ErrPermission**: Insufficient file system permissions
- **ErrDiskSpace**: Disk full or quota exceeded
- **ErrCorrupted**: Metadata mismatch or read error
- **ErrInvalidRef**: Malformed or missing reference

## Usage Examples

### Basic Usage

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/storage/local"
    "github.com/AltairaLabs/PromptKit/runtime/types"
)

// Create storage
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir: "./media",
})

// Store media
media := &types.MediaContent{
    Type:     "image",
    MimeType: "image/png",
    Data:     base64ImageData,
}

metadata := storage.MediaMetadata{
    ConversationID: "conv-123",
    SessionID:      "session-xyz",
}

ref, err := fileStore.Store(ctx, media, metadata)
if err != nil {
    log.Fatal(err)
}

// Retrieve media
loaded, err := fileStore.Retrieve(ctx, ref)
if err != nil {
    log.Fatal(err)
}

// Delete media
err = fileStore.Delete(ctx, ref)
if err != nil {
    log.Fatal(err)
}
```

### With SDK

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/storage/local"
)

// Create storage
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:             "./media",
    Organization:        local.OrganizationBySession,
    EnableDeduplication: true,
})

// Enable in SDK
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(fileStore),
)

// Media automatically externalized
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:     "user123",
    SessionID:  "session-xyz",
    PromptName: "assistant",
})

resp, _ := conv.Send(ctx, "Generate an image")
// Large images automatically stored via fileStore
```

### Custom Organization

```go
// Flat organization for simple apps
flatStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:      "./temp-media",
    Organization: local.OrganizationFlat,
})

// By-run for tests
testStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:      "./test-output/media",
    Organization: local.OrganizationByRun,
})

// By-conversation for analysis
analysisStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:      "./analysis/media",
    Organization: local.OrganizationByConversation,
})
```

### With Deduplication

```go
// Enable deduplication
dedupStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:             "./media",
    Organization:        local.OrganizationBySession,
    EnableDeduplication: true,
})

// Store identical media
ref1, _ := dedupStore.Store(ctx, media1, metadata1)
ref2, _ := dedupStore.Store(ctx, media2, metadata2) // Same content

// Only one copy stored, both refs work
loaded1, _ := dedupStore.Retrieve(ctx, ref1)
loaded2, _ := dedupStore.Retrieve(ctx, ref2)

// Delete requires both refs deleted
dedupStore.Delete(ctx, ref1) // Decrements ref count
dedupStore.Delete(ctx, ref2) // Actually deletes file
```

### Production Configuration

```go
// Production setup
productionStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:             "/var/lib/myapp/media",
    Organization:        local.OrganizationBySession,
    EnableDeduplication: true,
})

// With SDK
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(productionStore),
    sdk.WithConfig(sdk.ManagerConfig{
        MediaSizeThresholdKB: 100,  // Externalize > 100KB
        MediaDefaultPolicy:   "retain",
    }),
)
```

## Custom Storage Backends

### Implementing MediaStorageService

Create custom backends for S3, GCS, Azure Blob Storage, etc:

```go
type S3Store struct {
    bucket string
    client *s3.Client
}

func (s *S3Store) Store(ctx context.Context, media *types.MediaContent, metadata storage.MediaMetadata) (*storage.StorageReference, error) {
    // Generate unique key
    key := fmt.Sprintf("%s/%s/%s", metadata.SessionID, metadata.ConversationID, uuid.New())
    
    // Upload to S3
    _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
        Bucket: aws.String(s.bucket),
        Key:    aws.String(key),
        Body:   bytes.NewReader([]byte(media.Data)),
    })
    if err != nil {
        return nil, err
    }
    
    // Return reference
    return &storage.StorageReference{
        ID:      key,
        Backend: "s3",
        Metadata: map[string]string{
            "bucket": s.bucket,
            "region": s.client.Options().Region,
        },
        CreatedAt: time.Now(),
    }, nil
}

func (s *S3Store) Retrieve(ctx context.Context, ref *storage.StorageReference) (*types.MediaContent, error) {
    // Download from S3
    result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
        Bucket: aws.String(ref.Metadata["bucket"]),
        Key:    aws.String(ref.ID),
    })
    if err != nil {
        return nil, err
    }
    defer result.Body.Close()
    
    // Read data
    data, err := io.ReadAll(result.Body)
    if err != nil {
        return nil, err
    }
    
    // Return media
    return &types.MediaContent{
        Data:     base64.StdEncoding.EncodeToString(data),
        MimeType: aws.ToString(result.ContentType),
    }, nil
}

func (s *S3Store) Delete(ctx context.Context, ref *storage.StorageReference) error {
    _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
        Bucket: aws.String(ref.Metadata["bucket"]),
        Key:    aws.String(ref.ID),
    })
    return err
}
```

### Usage with Custom Backend

```go
// Create custom store
s3Store := &S3Store{
    bucket: "myapp-media",
    client: s3Client,
}

// Use with SDK
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(s3Store),
)
```

## Best Practices

### Do

- **Create storage once** at application startup
- **Use by-session organization** for most applications
- **Enable deduplication** if you have repeated media
- **Monitor disk space** with alerts
- **Use absolute paths** for BaseDir in production
- **Handle errors** gracefully (disk full, permissions)
- **Clean up old media** with scheduled jobs

### Don't

- **Don't create storage per request** (expensive)
- **Don't share directories** between environments
- **Don't forget permissions** (write access required)
- **Don't ignore errors** (disk full is common)
- **Don't hardcode paths** (use environment variables)
- **Don't delete while in use** (check reference counts)

## Performance Considerations

### FileStore Performance

- **Write**: ~1-5ms for typical images (100KB-1MB)
- **Read**: ~1-3ms from local disk
- **Dedup Hash**: ~10-20ms for 1MB image
- **Organization**: Minimal impact on performance

### Optimization Tips

1. **Lower threshold** for more externalization (less memory)
2. **Disable dedup** for unique media (avoid hash overhead)
3. **Use SSD** for media storage (faster I/O)
4. **Mount volumes** in containers (persistence)
5. **Monitor metrics** (disk I/O, space usage)

## Troubleshooting

### Common Issues

**Permission Denied:**

```bash
# Fix permissions
chmod 755 /var/lib/myapp/media
chown -R appuser:appuser /var/lib/myapp/media
```

**Disk Space Full:**

```bash
# Check usage
du -sh ./media

# Clean old sessions
find ./media -type d -name "session-*" -mtime +30 -delete
```

**Media Not Found:**

```go
// Check reference validity
if ref == nil || ref.ID == "" {
    log.Printf("Invalid reference")
}

// Verify file exists
path := filepath.Join(baseDir, ref.Metadata["path"])
if _, err := os.Stat(path); os.IsNotExist(err) {
    log.Printf("File missing: %s", path)
}
```

**Deduplication Index Corrupted:**

```bash
# Rebuild index
rm ./media/.dedup-index.json
# Restart application (rebuilds from metadata files)
```

## See Also

- **[How-To: Configure Media Storage](../../sdk/how-to/configure-media-storage)** - Configuration guide
- **[Types Reference](types#mediacontent)** - MediaContent structure
- **[Providers Reference](providers#medialoader)** - MediaLoader for unified access
- **[SDK Reference](../../sdk/reference/conversation-manager)** - WithMediaStorage option
- **[Explanation: Media Storage](../../sdk/explanation/media-storage)** - Design and concepts
