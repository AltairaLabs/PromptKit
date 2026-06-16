package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/mediagen"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// registerMediaGenTools wires the built-in image__generate and video__generate
// tools during pipeline construction.
//
// Registration is capability-gated: a tool is registered only when a matching
// provider is present in the pool (an image provider for image__generate, a
// video provider for video__generate). Once registered, the tool is exposed to
// every prompt in the conversation without a tools: entry, because image/video
// are system namespaces (like workflow__/memory__). With no matching provider
// the tool is absent entirely.
//
// The executor resolves the default provider of the relevant type from the pool
// at call time. There is no concrete video provider yet, so GetAll(video) is
// empty and video__generate stays unregistered until one lands.
func (c *Conversation) registerMediaGenTools() {
	// Ensure the pool exists so GetAll/Base are safe even before any provider
	// option ran (the pointer is stable; entries are added during Open).
	ensureProviderPool(c.config)
	pool := c.config.providers.Base()

	hasImage := len(pool.GetAll(base.ProviderTypeImage)) > 0
	hasVideo := len(pool.GetAll(base.ProviderTypeVideo)) > 0
	if !hasImage && !hasVideo {
		return
	}

	c.toolRegistry.RegisterExecutor(mediagen.NewExecutor(pool))
	if hasImage {
		_ = c.toolRegistry.Register(mediagen.ImageGenerateDescriptor())
	}
	if hasVideo {
		_ = c.toolRegistry.Register(mediagen.VideoGenerateDescriptor())
	}
}
