// Package main demonstrates tool handling with the PromptKit SDK.
//
// This example shows:
//   - Defining tools in the pack file
//   - Registering tool handlers with OnTool
//   - Automatic JSON serialization of results
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"

	"github.com/AltairaLabs/PromptKit/sdk"
)

// Weather represents weather data.
type Weather struct {
	City        string  `json:"city"`
	Country     string  `json:"country"`
	Temperature float64 `json:"temperature"`
	Conditions  string  `json:"conditions"`
}

func main() {
	// Open a conversation with tool support
	conv, err := sdk.Open("./tools.pack.json", "assistant")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// Register a simple tool handler
	conv.OnTool("get_time", func(args map[string]any) (any, error) {
		timezone := "UTC"
		if tz, ok := args["timezone"].(string); ok && tz != "" {
			timezone = tz
		}
		return map[string]string{
			"time":     "14:30:00",
			"timezone": timezone,
			"date":     "2025-01-15",
		}, nil
	})

	// Register a weather tool handler
	conv.OnTool("get_weather", func(args map[string]any) (any, error) {
		city, _ := args["city"].(string)
		country, _ := args["country"].(string)

		// Simulate weather API call
		conditions := []string{"Sunny", "Cloudy", "Rainy", "Partly Cloudy"}
		return Weather{
			City:        city,
			Country:     country,
			Temperature: 15.0 + rand.Float64()*20.0,
			Conditions:  conditions[rand.Intn(len(conditions))],
		}, nil
	})

	// Ask about the weather - the LLM will call our tool
	ctx := context.Background()
	resp, err := conv.Send(ctx, "What's the weather like in London, UK?")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	fmt.Println("Response:", resp.Text())

	// Ask about the time
	resp, err = conv.Send(ctx, "What time is it in Tokyo?")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	fmt.Println("Response:", resp.Text())
}
