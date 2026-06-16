// Package mediagen provides the built-in image__generate and video__generate
// tools. They let a text agent generate media as one step of a conversation by
// resolving an image/video provider from the shared provider pool
// (base.Registry) and invoking its Generate method.
//
// The package lives in runtime so both the SDK and Arena can wire it without
// duplicating provider-resolution logic. It depends only on runtime/providers/base,
// runtime/tools, and runtime/types — never on sdk or tools/arena.
package mediagen

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

const (
	// ImageGenerateToolName is the qualified name of the image generation tool.
	ImageGenerateToolName = "image__generate"
	// VideoGenerateToolName is the qualified name of the video generation tool.
	VideoGenerateToolName = "video__generate"
	// ExecutorMode is the tool Mode that routes to the mediagen Executor.
	ExecutorMode = "mediagen"
)

const imageToolDescription = "Generate an image from a text prompt using the configured image " +
	"provider. Returns the generated image inline in the conversation."

const videoToolDescription = "Generate a video from a text prompt using the configured video " +
	"provider. Returns the generated video inline in the conversation."

// ImageGenerateDescriptor returns the descriptor for the image__generate tool.
func ImageGenerateDescriptor() *tools.ToolDescriptor {
	return &tools.ToolDescriptor{
		Name:        ImageGenerateToolName,
		Namespace:   "image",
		Description: imageToolDescription,
		InputSchema: imageInputSchema(),
		Mode:        ExecutorMode,
	}
}

// VideoGenerateDescriptor returns the descriptor for the video__generate tool.
func VideoGenerateDescriptor() *tools.ToolDescriptor {
	return &tools.ToolDescriptor{
		Name:        VideoGenerateToolName,
		Namespace:   "video",
		Description: videoToolDescription,
		InputSchema: videoInputSchema(),
		Mode:        ExecutorMode,
	}
}

func imageInputSchema() json.RawMessage {
	return mediaInputSchema(
		"The text description of the image to generate.",
		"size", "Optional image size hint, e.g. \"1024x1024\".",
	)
}

func videoInputSchema() json.RawMessage {
	return mediaInputSchema(
		"The text description of the video to generate.",
		"aspect_ratio", "Optional aspect ratio hint, e.g. \"16:9\".",
	)
}

const (
	schemaTypeKey  = "type"
	schemaPromptID = "prompt"
)

// mediaInputSchema builds the tool input schema shared by the image and video
// tools: a required prompt plus one optional provider-specific string hint.
func mediaInputSchema(promptDesc, optName, optDesc string) json.RawMessage {
	strField := func(desc string) map[string]any {
		return map[string]any{schemaTypeKey: "string", "description": desc}
	}
	schema := map[string]any{
		schemaTypeKey: "object",
		"properties": map[string]any{
			schemaPromptID: strField(promptDesc),
			optName:        strField(optDesc),
		},
		"required": []string{schemaPromptID},
	}
	out, _ := json.Marshal(schema)
	return out
}
