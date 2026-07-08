package types_test

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ExampleCostInfo shows reading the cost of an LLM turn. The flat headline
// fields are the quick view; Quantities/Breakdown carry the per-unit detail
// populated on the unit-keyed cost path.
func ExampleCostInfo() {
	ci := types.CostInfo{
		InputTokens:   1000,
		OutputTokens:  500,
		InputCostUSD:  0.003,
		OutputCostUSD: 0.015,
		TotalCost:     0.018,
		Quantities:    map[string]float64{"input_token": 1000, "output_token": 500},
	}
	fmt.Printf("$%.3f total (%d in / %d out)\n", ci.TotalCost, ci.InputTokens, ci.OutputTokens)
	// Output: $0.018 total (1000 in / 500 out)
}

// ExampleNewTextMessage builds a simple text turn.
func ExampleNewTextMessage() {
	msg := types.NewTextMessage("user", "Hello!")
	fmt.Println(msg.Role, "->", msg.GetContent())
	// Output: user -> Hello!
}

// ExampleNewImagePartFromURL builds a multimodal image part from a URL. Parts
// are the unit of multimodal content; a Message carries a []ContentPart.
func ExampleNewImagePartFromURL() {
	part := types.NewImagePartFromURL("https://example.com/cat.png", nil)
	fmt.Println("type:", part.Type)
	// Output: type: image
}

// ExampleNewMultimodalMessage builds a message combining text and image
// parts. GetContent() extracts the text part(s) from Parts.
func ExampleNewMultimodalMessage() {
	parts := []types.ContentPart{
		types.NewTextPart("What is in this image?"),
		types.NewImagePartFromURL("https://example.com/cat.png", nil),
	}
	msg := types.NewMultimodalMessage("user", parts)
	fmt.Println(msg.GetContent())
	fmt.Println("parts:", len(msg.Parts))
	// Output:
	// What is in this image?
	// parts: 2
}
