// Package main demonstrates sending media to a vision model by durable
// storage reference with the PromptKit SDK.
//
// Instead of inlining image bytes on every request, the caller stores the
// image once in a MediaStorageService and passes a durable reference. At
// model-call time the provider resolves that reference to a model-fetchable
// URL (or bytes) via the store. With a cloud store the model fetches the
// presigned URL directly, so no image bytes need to transit the app.
//
// This example shows:
//   - Creating a local disk-backed MediaStorageService
//   - Storing an image once and getting back a durable reference
//   - Wiring the store into the conversation with WithMediaStorage
//   - Sending the image by reference with WithImageStorageRef
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
//
// Alternatively set GEMINI_API_KEY or ANTHROPIC_API_KEY and the SDK will
// select the matching vision-capable provider from the environment.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	ctx := context.Background()

	// 1. Create a local disk-backed media store under ./data. In production
	//    this would be an S3/GCS-backed store whose GetURL returns a presigned
	//    URL the model can fetch directly.
	dir := filepath.Join(".", "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("Failed to create data dir: %v", err)
	}
	store, err := local.NewFileStore(local.FileStoreConfig{BaseDir: dir})
	if err != nil {
		log.Fatalf("Failed to create file store: %v", err)
	}

	// 2. Store an image once. Here we generate a tiny PNG in code; a real app
	//    would store an uploaded or generated image. StoreMedia returns a
	//    durable reference we can re-send cheaply on any later request.
	ref, err := store.StoreMedia(ctx,
		&types.MediaContent{Data: pngDataURLPayload(), MIMEType: "image/png"},
		&storage.MediaMetadata{SessionID: "example", MIMEType: "image/png"},
	)
	if err != nil {
		log.Fatalf("Failed to store image: %v", err)
	}
	fmt.Printf("Stored image as durable reference: %s\n\n", ref)

	// 3. Open a conversation, wiring the store in so providers can resolve
	//    references at model-call time.
	conv, err := sdk.Open("./media-storage-ref.pack.json", "vision-analyst",
		sdk.WithMediaStorage(store),
	)
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// 4. Send the image BY REFERENCE — no bytes are attached to the request.
	fmt.Println("=== Vision Analysis (image sent by reference) ===")
	fmt.Println()

	for chunk := range conv.Stream(ctx, "Describe this image.",
		sdk.WithImageStorageRef(string(ref), "image/png"),
	) {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}
		if chunk.Type == sdk.ChunkDone {
			fmt.Println("\n\n[Analysis Complete]")
			break
		}
		fmt.Print(chunk.Text)
	}
}

// pngDataURLPayload builds a minimal 2x2 PNG and returns its base64 payload.
// MediaContent.Data holds base64-encoded bytes.
func pngDataURLPayload() *string {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 0xFF, A: 0xFF})
	img.Set(1, 0, color.RGBA{G: 0xFF, A: 0xFF})
	img.Set(0, 1, color.RGBA{B: 0xFF, A: 0xFF})
	img.Set(1, 1, color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatalf("Failed to encode PNG: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	return &encoded
}
