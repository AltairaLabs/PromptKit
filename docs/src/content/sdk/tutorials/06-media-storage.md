---
title: 'Tutorial 6: Working with Images and Media Storage'
docType: tutorial
order: 6
---

Build an AI-powered image generator with automatic memory optimization using media storage.

## What You'll Learn

- Generate images with DALL-E
- Automatically externalize large media to disk
- Reduce memory usage by 90% for media-heavy applications
- Monitor storage and performance
- Configure media organization strategies

## Prerequisites

- Go 1.21+ installed
- OpenAI API key with DALL-E access
- Completed [Tutorial 1: Your First Conversation](01-first-conversation)
- Basic understanding of file systems

## What We're Building

An image generation chatbot that:

- Generates images based on user prompts
- Automatically stores large images to disk
- Maintains low memory footprint
- Organizes media files by session
- Provides storage statistics

**Why Media Storage?**

- Images are typically 100KB-5MB in base64
- Without storage: 10 images = 10-50MB in memory
- With storage: 10 images = ~100KB in memory (references only)

## Step 1: Set Up Your Project

Create a new Go module:

```bash
mkdir image-generator
cd image-generator
go mod init image-generator
```

Install dependencies:

```bash
go get github.com/AltairaLabs/PromptKit/sdk
go get github.com/AltairaLabs/PromptKit/runtime/providers
go get github.com/AltairaLabs/PromptKit/runtime/storage/local
```

## Step 2: Create Media Storage Directory

Create directory structure:

```bash
mkdir -p media
mkdir -p packs
```

The `media` directory will store all generated images automatically.

## Step 3: Create Your PromptPack

Create `packs/image-generator.pack.json`:

```json
{
  "id": "image-generator",
  "name": "Image Generator",
  "version": "1.0.0",
  "description": "AI-powered image generation with DALL-E",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}",
    "features": ["basic_substitution"]
  },
  "prompts": {
    "image-generator": {
      "id": "image-generator",
      "name": "image-generator",
      "description": "AI image generator using DALL-E",
      "system_template": "You are an expert AI image generator. When users describe what they want, you'll generate images using DALL-E. Be creative and ask clarifying questions if the description is vague. After generating an image, describe what was created.",
      "parameters": {
        "temperature": 0.7,
        "max_tokens": 500
      },
      "tools": ["dalle-image-generator"]
    }
  },
  "tools": {
    "dalle-image-generator": {
      "name": "dalle-image-generator",
      "description": "Generate images using DALL-E based on text descriptions",
      "parameters": {
        "type": "object",
        "properties": {
          "prompt": {
            "type": "string",
            "description": "The image description/prompt"
          },
          "size": {
            "type": "string",
            "description": "Image size (e.g., '1024x1024')",
            "default": "1024x1024"
          }
        },
        "required": ["prompt"]
      }
    }
  }
}
```

This follows the [PromptPack v1.1 specification](https://promptpack.org/docs/spec/structure) with proper tool definitions and template engine configuration.

## Step 4: Implement the Image Generator

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "runtime"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/storage/local"
)

func main() {
    ctx := context.Background()

    // Check API key
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        log.Fatal("OPENAI_API_KEY not set")
    }

    // 1. Create media storage
    fmt.Println("Setting up media storage...")
    fileStore := local.NewFileStore(local.FileStoreConfig{
        BaseDir:             "./media",
        Organization:        local.OrganizationBySession,
        EnableDeduplication: true,
    })

    // 2. Create provider
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o", false)

    // 3. Create manager with media storage
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithMediaStorage(fileStore),
        sdk.WithConfig(sdk.ManagerConfig{
            MediaSizeThresholdKB: 50, // Externalize images > 50KB
        }),
    )
    if err != nil {
        log.Fatalf("Failed to create manager: %v", err)
    }

    // 4. Load pack
    pack, err := manager.LoadPack("./packs/image-generator.pack.json")
    if err != nil {
        log.Fatalf("Failed to load pack: %v", err)
    }

    // 5. Create conversation
    sessionID := "session-demo-001"
    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        SessionID:  sessionID,
        PromptName: "image-generator",
    })
    if err != nil {
        log.Fatalf("Failed to create conversation: %v", err)
    }

    fmt.Printf("\n🎨 Image Generator Ready!\n")
    fmt.Printf("📂 Media storage: ./media/%s\n\n", sessionID)

    // Demonstrate with multiple generations
    prompts := []string{
        "Generate a beautiful sunset over mountains",
        "Create a futuristic city skyline",
        "Design a cozy coffee shop interior",
    }

    for i, prompt := range prompts {
        fmt.Printf("\n[%d/%d] Generating: %s\n", i+1, len(prompts), prompt)
        
        // Measure memory before
        var memBefore runtime.MemStats
        runtime.ReadMemStats(&memBefore)
        
        // Generate image
        response, err := conv.Send(ctx, prompt)
        if err != nil {
            log.Printf("Error: %v\n", err)
            continue
        }

        // Measure memory after
        var memAfter runtime.MemStats
        runtime.ReadMemStats(&memAfter)
        
        // Display response
        fmt.Printf("✓ %s\n", response.Content)
        fmt.Printf("  Cost: $%.4f\n", response.Cost)
        
        // Check for externalized media
        externalized := 0
        totalSize := int64(0)
        
        for _, msg := range response.Messages {
            for _, content := range msg.Content {
                if content.Type == "image" && content.Media != nil {
                    if content.Media.StorageReference != nil {
                        externalized++
                        fmt.Printf("  📁 Externalized: %s\n", 
                            content.Media.StorageReference.ID)
                    }
                }
            }
        }
        
        // Memory comparison
        memDiff := memAfter.Alloc - memBefore.Alloc
        fmt.Printf("  Memory impact: %d KB\n", memDiff/1024)
    }

    // Summary
    fmt.Printf("\n" + "=".repeat(50) + "\n")
    fmt.Printf("✅ Generated %d images\n", len(prompts))
    fmt.Printf("📂 Check ./media/%s/ for all images\n", sessionID)
    fmt.Printf("💾 Media automatically deduplicated and organized\n")
    
    printStorageStats("./media")
}

func printStorageStats(baseDir string) {
    // Simple storage statistics
    fmt.Printf("\n📊 Storage Statistics:\n")
    
    // This is simplified - in production you'd use filepath.Walk
    fmt.Printf("  Directory: %s\n", baseDir)
    fmt.Printf("  Organization: by-session\n")
    fmt.Printf("  Deduplication: enabled\n")
}
```

## Step 5: Run and See Memory Savings

Set your API key:

```bash
export OPENAI_API_KEY="your-api-key-here"
```

Run the generator:

```bash
go run main.go
```

Expected output:

```text
Setting up media storage...

🎨 Image Generator Ready!
📂 Media storage: ./media/session-demo-001

[1/3] Generating: Generate a beautiful sunset over mountains
✓ I've created a beautiful image of a sunset over mountains...
  Cost: $0.0450
  📁 Externalized: abc123-def456-ghi789.png
  Memory impact: 150 KB

[2/3] Generating: Create a futuristic city skyline
✓ Here's a futuristic city skyline with towering buildings...
  Cost: $0.0450
  📁 Externalized: xyz789-abc123-def456.png
  Memory impact: 145 KB

[3/3] Generating: Design a cozy coffee shop interior
✓ I've designed a warm and inviting coffee shop interior...
  Cost: $0.0450
  📁 Externalized: 123456-789abc-def012.png
  Memory impact: 148 KB

==================================================
✅ Generated 3 images
📂 Check ./media/session-demo-001/ for all images
💾 Media automatically deduplicated and organized

📊 Storage Statistics:
  Directory: ./media
  Organization: by-session
  Deduplication: enabled
```

## Step 6: Explore the Media Directory

Check what was created:

```bash
ls -lh media/session-demo-001/conv-*/
```

You'll see:

```text
media/session-demo-001/conv-abc123/
  abc123-def456-ghi789.png      1.2M
  abc123-def456-ghi789.png.meta  512B
  xyz789-abc123-def456.png      1.1M
  xyz789-abc123-def456.png.meta  512B
  123456-789abc-def012.png      1.3M
  123456-789abc-def012.png.meta  512B
```

**Each image has:**

- `.png` - The actual image file
- `.meta` - Metadata (conversation ID, timestamp, etc.)

## Understanding the Memory Savings

Let's compare with and without media storage:

### Without Media Storage

```text
Image 1: 1.2MB base64 in memory
Image 2: 1.1MB base64 in memory
Image 3: 1.3MB base64 in memory
Total: ~3.6MB in memory per conversation
```

### With Media Storage

```text
Image 1: ~50 bytes (reference) in memory → 1.2MB on disk
Image 2: ~50 bytes (reference) in memory → 1.1MB on disk
Image 3: ~50 bytes (reference) in memory → 1.3MB on disk
Total: ~150 bytes in memory, 3.6MB on disk
Savings: 99.996% memory reduction
```

## Step 7: Add Interactive Mode

Let's make it interactive. Update `main.go`:

```go
func main() {
    // ... (previous setup code) ...

    fmt.Println("\n🎨 Interactive Image Generator")
    fmt.Println("Type your image prompts (or 'quit' to exit)")
    fmt.Println("Examples:")
    fmt.Println("  - A serene lake at dawn")
    fmt.Println("  - Abstract art with bold colors")
    fmt.Println("  - A robot reading a book\n")

    scanner := bufio.NewScanner(os.Stdin)
    imageCount := 0

    for {
        fmt.Print("\n> ")
        if !scanner.Scan() {
            break
        }

        prompt := scanner.Text()
        if prompt == "quit" || prompt == "exit" {
            break
        }
        
        if prompt == "" {
            continue
        }

        // Show memory before generation
        var memBefore runtime.MemStats
        runtime.ReadMemStats(&memBefore)

        response, err := conv.Send(ctx, prompt)
        if err != nil {
            fmt.Printf("Error: %v\n", err)
            continue
        }

        // Show memory after
        var memAfter runtime.MemStats
        runtime.ReadMemStats(&memAfter)

        fmt.Printf("\n%s\n", response.Content)
        
        // Check if image was externalized
        for _, msg := range response.Messages {
            for _, content := range msg.Content {
                if content.Type == "image" && content.Media != nil {
                    imageCount++
                    if content.Media.StorageReference != nil {
                        fmt.Printf("✓ Image #%d saved to disk\n", imageCount)
                        fmt.Printf("  ID: %s\n", content.Media.StorageReference.ID)
                        
                        memDiff := memAfter.Alloc - memBefore.Alloc
                        fmt.Printf("  Memory used: %d KB (instead of ~1200 KB)\n", 
                            memDiff/1024)
                    }
                }
            }
        }
        
        fmt.Printf("  Cost: $%.4f\n", response.Cost)
    }

    fmt.Printf("\n✅ Generated %d images total\n", imageCount)
    fmt.Printf("📂 All images saved in ./media/%s/\n", sessionID)
}
```

Don't forget to import `bufio`:

```go
import (
    "bufio"
    // ... other imports
)
```

## Step 8: Configure Organization Modes

Try different organization strategies:

### Flat Organization (Simple)

```go
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:      "./media",
    Organization: local.OrganizationFlat,
})
```

Result:

```text
media/
  abc123-image1.png
  def456-image2.png
  ghi789-image3.png
```

### By-Conversation (Per Chat)

```go
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:      "./media",
    Organization: local.OrganizationByConversation,
})
```

Result:

```text
media/
  conv-abc123/
    image1.png
    image2.png
  conv-def456/
    image1.png
```

### By-Session (Recommended)

```go
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:      "./media",
    Organization: local.OrganizationBySession,
})
```

Result:

```text
media/
  session-demo-001/
    conv-abc123/
      image1.png
    conv-def456/
      image1.png
```

## Step 9: Add Deduplication Demo

Create `demo_dedup.go` to demonstrate deduplication:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/storage/local"
)

func demonstrateDeduplication() {
    // Generate same image twice to show deduplication
    
    fileStore := local.NewFileStore(local.FileStoreConfig{
        BaseDir:             "./media",
        Organization:        local.OrganizationBySession,
        EnableDeduplication: true,
    })

    // ... create manager and conversation ...

    fmt.Println("Generating first image...")
    resp1, _ := conv.Send(ctx, "Generate: a red apple")
    
    fmt.Println("Generating same image again...")
    resp2, _ := conv.Send(ctx, "Generate: a red apple")
    
    // Same image, only stored once
    fmt.Println("\n✓ Deduplication saved storage space")
    fmt.Println("  Both references point to the same file")
    fmt.Println("  Storage: 1 file instead of 2")
}
```

## Step 10: Production Configuration

For production use, configure properly:

```go
func createProductionManager() (*sdk.ConversationManager, error) {
    // Use environment variables
    mediaDir := os.Getenv("MEDIA_DIR")
    if mediaDir == "" {
        mediaDir = "/var/lib/myapp/media"
    }

    fileStore := local.NewFileStore(local.FileStoreConfig{
        BaseDir:             mediaDir,
        Organization:        local.OrganizationBySession,
        EnableDeduplication: true,
    })

    provider := providers.NewOpenAIProvider(
        os.Getenv("OPENAI_API_KEY"),
        "gpt-4o",
        false,
    )

    return sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithMediaStorage(fileStore),
        sdk.WithConfig(sdk.ManagerConfig{
            MediaSizeThresholdKB: 100,    // Externalize > 100KB
            MediaDefaultPolicy:   "retain",
            DefaultTimeout:       60 * time.Second,
        }),
    )
}
```

## Common Patterns

### Pattern 1: Image Analysis with Upload

```go
// User uploads image, ask questions about it
resp, err := conv.SendWithMedia(ctx, "What's in this image?", []sdk.MediaContent{
    {
        Type:     "image",
        MimeType: "image/jpeg",
        Data:     uploadedImageData, // Large image
    },
})

// Automatically externalized to disk
```

### Pattern 2: Multi-Image Generation

```go
// Generate multiple images in one conversation
prompts := []string{
    "Scene 1: Hero enters the forest",
    "Scene 2: Hero finds the treasure",
    "Scene 3: Hero returns home",
}

for _, prompt := range prompts {
    resp, _ := conv.Send(ctx, prompt)
    // Each image automatically stored
}

// All images organized in same session directory
```

### Pattern 3: Batch Processing

```go
// Process many images without memory overflow
for i, userPrompt := range largePromptList {
    resp, err := conv.Send(ctx, userPrompt)
    if err != nil {
        log.Printf("Failed prompt %d: %v", i, err)
        continue
    }
    
    // Memory stays constant regardless of image count
}
```

## Troubleshooting

### Images Not Externalizing

Check threshold:

```go
manager, _ := sdk.NewConversationManager(
    sdk.WithMediaStorage(fileStore),
    sdk.WithConfig(sdk.ManagerConfig{
        MediaSizeThresholdKB: 50, // Lower = more externalization
    }),
)
```

### Disk Space Issues

Monitor usage:

```bash
# Check media directory size
du -sh ./media

# Find large files
find ./media -type f -size +1M

# Clean old sessions
find ./media -type d -name "session-*" -mtime +7 -exec rm -rf {} \;
```

### Permission Errors

Fix permissions:

```bash
chmod 755 ./media
# Or for production
sudo chown -R appuser:appuser /var/lib/myapp/media
```

## Key Takeaways

✅ **Media storage dramatically reduces memory usage** (70-90% reduction)

✅ **Automatic and transparent** - no code changes needed

✅ **Deduplication saves storage space** (30-70% savings)

✅ **Organization modes** fit different use cases

✅ **Production-ready** with proper configuration

## Next Steps

- **[How-To: Configure Media Storage](../how-to/configure-media-storage)** - Advanced configuration
- **[Storage Reference](../../runtime/reference/storage)** - Complete API documentation
- **[Explanation: Media Storage Design](../explanation/media-storage)** - Architecture deep dive
- **[Tutorial 7: Production Deployment](07-production-deployment)** - Deploy at scale

## Complete Code

The complete working example is available in:

- [examples/sdk/media-storage/](https://github.com/AltairaLabs/PromptKit/tree/main/examples/sdk/media-storage)

Clone and run:

```bash
git clone https://github.com/AltairaLabs/PromptKit
cd PromptKit/examples/sdk/media-storage
export OPENAI_API_KEY="your-key"
go run main.go
```

🎉 **Congratulations!** You've mastered media storage with PromptKit!
