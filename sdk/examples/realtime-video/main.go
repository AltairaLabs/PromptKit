// Package main demonstrates realtime video/image streaming with the PromptKit SDK.
//
// This example shows:
//   - Opening a duplex session with video streaming enabled
//   - Sending image frames using SendFrame()
//   - Frame rate limiting with WithStreamingVideo
//   - Receiving streaming responses from vision models
//
// This simulates a webcam or screen capture scenario where frames are
// continuously sent to an LLM for real-time analysis.
//
// Run with:
//
//	export GEMINI_API_KEY=your-key
//	go run .
//
// Note: This example simulates frame capture. In a real application,
// you would capture frames from a webcam, screen, or video file.
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/session"
)

func main() {
	fmt.Println("🎥 Realtime Video Streaming Example")
	fmt.Println("====================================")
	fmt.Println()

	// Check for API key
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY environment variable is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Open a duplex session with video streaming configuration
	fmt.Println("Opening duplex session with video streaming...")
	fmt.Println()

	conv, err := sdk.OpenDuplex(
		"./realtime-video.pack.json",
		"vision-stream",
		sdk.WithAPIKey(apiKey),
		sdk.WithModel("gemini-2.0-flash-exp"),
		// Configure video streaming: 1 FPS, auto-resize to 1024x1024
		sdk.WithStreamingVideo(&sdk.VideoStreamConfig{
			TargetFPS:    1.0,   // 1 frame per second for LLM processing
			MaxWidth:     1024,
			MaxHeight:    1024,
			Quality:      85,
			EnableResize: true,
		}),
	)
	if err != nil {
		log.Fatalf("Failed to open duplex session: %v", err)
	}
	defer conv.Close()

	// Get the response channel
	responseChan, err := conv.Response()
	if err != nil {
		log.Fatalf("Failed to get response channel: %v", err)
	}

	// Start receiving responses in a goroutine
	go func() {
		fmt.Println("Waiting for vision analysis responses...")
		fmt.Println()
		for chunk := range responseChan {
			if chunk.Error != nil {
				fmt.Printf("\n[Error: %v]\n", chunk.Error)
				continue
			}
			if chunk.Content != "" {
				fmt.Print(chunk.Content)
			}
			if chunk.FinishReason != nil && *chunk.FinishReason != "" {
				fmt.Printf("\n[Response complete: %s]\n", *chunk.FinishReason)
			}
		}
	}()

	// Simulate sending frames (in a real app, these would come from a webcam)
	fmt.Println("Simulating frame capture and sending...")
	fmt.Println()

	// Send a few simulated frames
	for i := 1; i <= 3; i++ {
		// Generate a simulated frame (in practice, capture from webcam)
		frameData, err := generateSimulatedFrame(i)
		if err != nil {
			log.Printf("Failed to generate frame %d: %v", i, err)
			continue
		}

		// Create an ImageFrame and send it
		frame := &session.ImageFrame{
			Data:      frameData,
			MIMEType:  "image/jpeg",
			Width:     320,
			Height:    240,
			FrameNum:  int64(i),
			Timestamp: time.Now(),
		}

		fmt.Printf("Sending frame %d (%d bytes)...\n", i, len(frameData))

		err = conv.SendFrame(ctx, frame)
		if err != nil {
			log.Printf("Failed to send frame %d: %v", i, err)
			continue
		}

		// Wait between frames (simulating real-time capture)
		time.Sleep(time.Second)
	}

	// Send a text prompt to trigger analysis
	fmt.Println("\nSending analysis request...")
	err = conv.SendText(ctx, "What did you observe in the frames I just sent?")
	if err != nil {
		log.Printf("Failed to send text: %v", err)
	}

	// Wait for responses
	fmt.Println("\nWaiting for analysis...")
	doneChan, err := conv.Done()
	if err != nil {
		log.Printf("Failed to get done channel: %v", err)
	} else {
		select {
		case <-doneChan:
			fmt.Println("\n[Session complete]")
		case <-time.After(15 * time.Second):
			fmt.Println("\n[Timeout waiting for response]")
		}
	}

	fmt.Println("\n=== Example Complete ===")
	fmt.Println()
	fmt.Println("In a real application, you would:")
	fmt.Println("  1. Capture frames from webcam using a library like gocv")
	fmt.Println("  2. Encode frames as JPEG")
	fmt.Println("  3. Send via conv.SendFrame() at your desired rate")
	fmt.Println("  4. The SDK handles frame rate limiting automatically")
}

// generateSimulatedFrame creates a simple test image with frame number overlay.
// In a real application, this would be replaced with actual webcam capture.
func generateSimulatedFrame(frameNum int) ([]byte, error) {
	// Create a simple colored image that changes each frame
	width, height := 320, 240
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill with a color that varies by frame number
	var bgColor color.RGBA
	switch frameNum % 3 {
	case 0:
		bgColor = color.RGBA{100, 150, 200, 255} // Blue-ish
	case 1:
		bgColor = color.RGBA{150, 200, 100, 255} // Green-ish
	case 2:
		bgColor = color.RGBA{200, 100, 150, 255} // Pink-ish
	}

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, bgColor)
		}
	}

	// Draw a simple moving element (a white rectangle that moves)
	rectX := (frameNum * 50) % (width - 50)
	for y := height/2 - 25; y < height/2+25; y++ {
		for x := rectX; x < rectX+50; x++ {
			img.Set(x, y, color.White)
		}
	}

	// Encode as JPEG
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
