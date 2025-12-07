// Package sdk provides a simple API for LLM conversations using PromptPack files.
//
// SDK v2 is a pack-first SDK where everything starts from a .pack.json file.
// The pack contains prompts, variables, tools, validators, and model configuration.
// The SDK loads the pack and provides a minimal API to interact with LLMs.
//
// # Quick Start
//
// The simplest possible usage is just 5 lines:
//
//	conv, err := sdk.Open("./assistant.pack.json", "chat")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer conv.Close()
//
//	resp, _ := conv.Send(ctx, "Hello!")
//	fmt.Println(resp.Text())
//
// # Core Concepts
//
// Opening a Conversation:
//
// Use [Open] to load a pack file and create a conversation for a specific prompt:
//
//	// Minimal - provider auto-detected from environment
//	conv, _ := sdk.Open("./demo.pack.json", "troubleshooting")
//
//	// With options - override model, provider, etc.
//	conv, _ := sdk.Open("./demo.pack.json", "troubleshooting",
//	    sdk.WithModel("gpt-4o"),
//	    sdk.WithAPIKey(os.Getenv("MY_OPENAI_KEY")),
//	)
//
// Variables:
//
// Variables defined in the pack are populated at runtime:
//
//	conv.SetVar("customer_id", "acme-corp")
//	conv.SetVars(map[string]any{
//	    "customer_name": "ACME Corporation",
//	    "tier": "premium",
//	})
//
// Tools:
//
// Tools defined in the pack just need implementation handlers:
//
//	conv.OnTool("list_devices", func(args map[string]any) (any, error) {
//	    return myAPI.ListDevices(args["customer_id"].(string))
//	})
//
// Streaming:
//
// Stream responses chunk by chunk:
//
//	for chunk := range conv.Stream(ctx, "Tell me a story") {
//	    fmt.Print(chunk.Text)
//	}
//
// # Design Principles
//
//  1. Pack is the Source of Truth - The .pack.json file defines prompts, tools, validators,
//     and pipeline configuration. The SDK configures itself automatically.
//  2. Convention Over Configuration - API keys from environment, provider auto-detection,
//     model defaults from pack. Override only when needed.
//  3. Progressive Disclosure - Simple things are simple, advanced features available but
//     not required.
//  4. Same Runtime, Same Behavior - SDK v2 uses the same runtime pipeline as Arena.
//     Pack-defined behaviors work identically.
//  5. Thin Wrapper - No type duplication. Core types like Message, ContentPart, CostInfo
//     come directly from runtime/types.
//
// # Package Structure
//
// The SDK is organized into sub-packages for specific functionality:
//
//   - sdk (this package): Entry point, [Open], [Conversation], [Response]
//   - sdk/tools: Typed tool handlers, HITL support
//   - sdk/stream: Streaming response handling
//   - sdk/message: Multimodal message building
//   - sdk/hooks: Event subscription and lifecycle hooks
//   - sdk/validation: Validator registration and error handling
//
// Most users only need to import the root sdk package.
//
// # Runtime Types
//
// The SDK uses runtime types directly - no duplication:
//
//	import "github.com/AltairaLabs/PromptKit/runtime/types"
//
//	msg := &types.Message{Role: "user"}
//	msg.AddTextPart("Hello")
//
// Key runtime types: [types.Message], [types.ContentPart], [types.MediaContent],
// [types.CostInfo], [types.ValidationResult].
//
// # Schema Reference
//
// All pack examples conform to the PromptPack Specification v1.1.0:
// https://github.com/AltairaLabs/promptpack-spec/blob/main/schema/promptpack.schema.json
package sdk
