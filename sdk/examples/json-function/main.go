// Package main demonstrates the "function-style" PromptKit pattern: a prompt
// invoked like a serverless function — structured JSON in, one-shot, JSON out.
//
// It shows:
//   - WithJSONInput: binding a structured input to the prompt's {{variables}}
//     (top-level fields become {{topic}}/{{audience}}; the whole payload is {{input}})
//   - WithResponseFormat: requesting schema-enforced JSON output
//   - A single Send with an empty message, so the input JSON also fills the user turn
//   - Unmarshaling the JSON response into a Go struct
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// PlanRequest is the function input. Its top-level fields bind to the pack's
// {{topic}} and {{audience}} template variables.
type PlanRequest struct {
	Topic    string `json:"topic"`
	Audience string `json:"audience"`
}

// PlanResponse is the function output, matching outputSchema below.
type PlanResponse struct {
	Summary   string   `json:"summary"`
	Questions []string `json:"questions"`
}

// outputSchema constrains the model to return a JSON object with exactly these
// fields. additionalProperties:false + required are needed for strict
// structured output (e.g. Anthropic / OpenAI strict mode).
var outputSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"summary": {"type": "string"},
		"questions": {"type": "array", "items": {"type": "string"}}
	},
	"required": ["summary", "questions"],
	"additionalProperties": false
}`)

func main() {
	// Open the prompt and request schema-enforced JSON output. WithResponseFormat
	// is the proven cross-provider output mechanism (Claude output_config,
	// OpenAI response_format, Gemini responseSchema).
	conv, err := sdk.Open("./research.pack.json", "plan",
		sdk.WithResponseFormat(&providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			JSONSchema: outputSchema,
			SchemaName: "research_plan",
			Strict:     true,
		}),
	)
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	input := PlanRequest{
		Topic:    "utility-scale battery storage growth in 2023",
		Audience: "executive",
	}

	// One-shot invocation. The empty message means the input JSON also becomes
	// the user turn; WithJSONInput binds its fields to {{topic}}/{{audience}}.
	resp, err := conv.Send(context.Background(), "", sdk.WithJSONInput(input))
	if err != nil {
		log.Fatalf("Failed to invoke function: %v", err)
	}

	var plan PlanResponse
	if err := json.Unmarshal([]byte(resp.Text()), &plan); err != nil {
		log.Fatalf("Response was not valid JSON: %v\nraw: %s", err, resp.Text())
	}

	fmt.Printf("Summary: %s\n", plan.Summary)
	for i, q := range plan.Questions {
		fmt.Printf("  %d. %s\n", i+1, q)
	}
}
