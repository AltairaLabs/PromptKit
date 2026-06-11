// Package all blank-imports every classify backend so their factories
// self-register. Import this (with _) from any consumer that builds a
// classify.Registry from config (the SDK, Arena).
package all

import (
	_ "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf" // register "huggingface" factory
)
