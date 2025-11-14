//go:build ignore
// +build ignore

package main

import (
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

func main() {
	// Create a test message with video
	duration := 30
	width := 1920
	height := 1080
	message := types.Message{
		Role: "assistant",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: stringPtr("Here's a video"),
			},
			{
				Type: types.ContentTypeVideo,
				Media: &types.MediaContent{
					URL:      stringPtr("mock://video.mp4"),
					MIMEType: "video/mp4",
					Duration: &duration,
					Width:    &width,
					Height:   &height,
				},
			},
		},
	}

	// Test VideoDurationValidator
	fmt.Println("=== VideoDurationValidator ===")
	durationValidator := assertions.NewVideoDurationValidator(map[string]interface{}{
		"min_seconds": 10.0,
		"max_seconds": 60.0,
	})
	durationResult := durationValidator.Validate("", map[string]interface{}{
		"_assistant_message": message,
	})
	printResult(durationResult)

	// Test VideoResolutionValidator
	fmt.Println("\n=== VideoResolutionValidator ===")
	resolutionValidator := assertions.NewVideoResolutionValidator(map[string]interface{}{
		"presets":    []string{"1080p", "4k"},
		"min_width":  1280,
		"max_width":  3840,
		"min_height": 720,
		"max_height": 2160,
	})
	resolutionResult := resolutionValidator.Validate("", map[string]interface{}{
		"_assistant_message": message,
	})
	printResult(resolutionResult)
}

func printResult(result interface{}) {
	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(jsonBytes))
}

func stringPtr(s string) *string {
	return &s
}
