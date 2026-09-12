// Package all provides a convenient way to register all PromptKit providers
// with a single import. Instead of importing each provider individually:
//
//	import (
//	    _ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
//	    _ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
//	    _ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"
//	    _ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
//	)
//
// You can simply import this package:
//
//	import _ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/all"
//
// This registers all available providers with the provider registry,
// making them available for use in your application.
package all

import (
	// Register Claude provider
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"

	// Register Gemini provider
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"

	// Register Imagen provider (Google's image generation)
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/imagen"

	// Register Mock provider (for testing)
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"

	// Register Ollama provider (local models)
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"

	// Register OpenAI provider
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"

	// Register Replay provider (for deterministic testing)
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/replay"

	// Register vLLM provider (for self-hosted inference)
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
)
