package multimodalbasics
package main

import (
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func main() {
	fmt.Println("=== PromptKit Multimodal Message Examples ===\n")

	// Example 1: Legacy text-only message (backward compatibility)
	legacyMsg := types.Message{
		Role:    "user",
		Content: "What is the capital of France?",
	}
	fmt.Println("1. Legacy Text Message:")
	printMessage(legacyMsg)

	// Example 2: Simple multimodal message with text
	textMsg := types.Message{Role: "user"}
	textMsg.AddTextPart("Hello from the new multimodal API!")
	fmt.Println("\n2. Multimodal Text Message:")
	printMessage(textMsg)

	// Example 3: Text + Image from URL
	imageMsg := types.Message{Role: "user"}
	imageMsg.AddTextPart("What's in this image?")
	imageMsg.AddImagePartFromURL("https://example.com/photo.jpg", nil)
	fmt.Println("\n3. Text + Image (URL):")
	printMessage(imageMsg)

	// Example 4: Text + Image with detail level
	detailMsg := types.Message{Role: "user"}
	detailMsg.AddTextPart("Analyze this chart in detail:")
	detail := "high"
	detailMsg.AddImagePartFromURL("https://example.com/chart.png", &detail)
	fmt.Println("\n4. Text + Image (High Detail):")
	printMessage(detailMsg)

	// Example 5: Multiple content types - text, image, audio
	multiMsg := types.Message{Role: "user"}
	multiMsg.AddTextPart("Please analyze these files:")
	multiMsg.AddImagePartFromURL("https://example.com/diagram.png", nil)
	multiMsg.AddTextPart("And listen to this audio:")
	
	// Using base64-encoded audio data
	audioData := types.NewAudioPartFromData("YmFzZTY0IGF1ZGlvIGRhdGE=", types.MIMETypeAudioMP3)
	multiMsg.AddPart(audioData)
	
	fmt.Println("\n5. Multi-content Message (Text + Image + Audio):")
	printMessage(multiMsg)

	// Example 6: Direct construction with ContentPart slice
	directMsg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("Compare these two images:"),
			types.NewImagePartFromURL("https://example.com/before.jpg", nil),
			types.NewTextPart("vs"),
			types.NewImagePartFromURL("https://example.com/after.jpg", nil),
		},
	}
	fmt.Println("\n6. Direct Construction with Multiple Images:")
	printMessage(directMsg)

	// Example 7: Video content
	videoMsg := types.Message{Role: "user"}
	videoMsg.AddTextPart("What happens in this video?")
	videoMsg.AddPart(types.NewVideoPartFromData("YmFzZTY0IHZpZGVvIGRhdGE=", types.MIMETypeVideoMP4))
	fmt.Println("\n7. Text + Video:")
	printMessage(videoMsg)

	// Example 8: Using helper methods to check content type
	fmt.Println("\n8. Message Type Checks:")
	fmt.Printf("   Legacy message is multimodal: %v\n", legacyMsg.IsMultimodal())
	fmt.Printf("   Image message is multimodal: %v\n", imageMsg.IsMultimodal())
	fmt.Printf("   Image message has media: %v\n", imageMsg.HasMediaContent())
	fmt.Printf("   Text message has media: %v\n", textMsg.HasMediaContent())
	fmt.Printf("   Legacy message content: %q\n", legacyMsg.GetContent())
	fmt.Printf("   Multi message text content: %q\n", multiMsg.GetContent())
}

func printMessage(msg types.Message) {
	// Pretty print the message as JSON
	jsonData, err := json.MarshalIndent(msg, "   ", "  ")
	if err != nil {
		fmt.Printf("   Error: %v\n", err)
		return
	}
	fmt.Printf("   %s\n", string(jsonData))
}
