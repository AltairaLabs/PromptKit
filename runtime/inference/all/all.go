// Package all blank-imports every inference provider so their factories
// self-register. Import it (with _) from any consumer that builds inference
// providers from config (the SDK, Arena).
package all

import (
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/inference/huggingface" // register "huggingface"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/inference/openai"      // register "openai", "nvidia-topic-control"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/inference/systemone"   // register "systemone"
)
