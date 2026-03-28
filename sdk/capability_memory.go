package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// MemoryCapability registers memory tools and wires the memory executor.
type MemoryCapability struct {
	store     memory.Store
	scope     map[string]string
	extractor memory.Extractor
	retriever memory.Retriever
}

// NewMemoryCapability creates a MemoryCapability with the given store and scope.
func NewMemoryCapability(store memory.Store, scope map[string]string) *MemoryCapability {
	return &MemoryCapability{store: store, scope: scope}
}

// WithExtractor sets the memory extractor for automatic extraction.
func (c *MemoryCapability) WithExtractor(e memory.Extractor) *MemoryCapability {
	c.extractor = e
	return c
}

// WithRetriever sets the memory retriever for automatic RAG injection.
func (c *MemoryCapability) WithRetriever(r memory.Retriever) *MemoryCapability {
	c.retriever = r
	return c
}

// Name implements Capability.
func (c *MemoryCapability) Name() string { return memory.ExecutorMode }

// Init implements Capability.
func (c *MemoryCapability) Init(_ CapabilityContext) error { return nil }

// RegisterTools implements Capability. Registers the memory executor and
// tool descriptors, plus any custom tools from ToolProvider stores.
func (c *MemoryCapability) RegisterTools(registry *tools.Registry) {
	exec := memory.NewExecutor(c.store, c.scope)
	registry.RegisterExecutor(exec)
	memory.RegisterMemoryTools(registry)

	// Let stores register additional tools (e.g., graph traversal, temporal queries)
	if tp, ok := c.store.(memory.ToolProvider); ok {
		tp.RegisterTools(registry)
	}
}

// Close implements Capability.
func (c *MemoryCapability) Close() error { return nil }
