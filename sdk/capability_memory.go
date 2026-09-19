package sdk

import (
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// MemoryCapability registers memory tools and wires the memory executor.
type MemoryCapability struct {
	store     memory.Store
	scope     map[string]string
	extractor memory.Extractor
	retriever memory.Retriever
	formatter memory.ContextFormatter
	// subjectKey names the scope entry that identifies the memory subject.
	// Defaults to [DefaultMemorySubjectKey]; hosts that spell it differently
	// set it with [WithMemorySubjectKey]. Empty disables the gate entirely.
	subjectKey string
	// toolsDisabled suppresses registration of the memory tools
	// (memory__remember / memory__recall, etc.) and their executor,
	// leaving only the retriever wired for ambient RAG injection.
	toolsDisabled bool
}

// DefaultMemorySubjectKey is the scope key MemoryCapability reads to decide
// whether the conversation has an identified memory subject. Hosts whose scope
// map spells it differently override it with [WithMemorySubjectKey].
const DefaultMemorySubjectKey = "user_id"

// NewMemoryCapability creates a MemoryCapability with the given store and scope.
func NewMemoryCapability(store memory.Store, scope map[string]string) *MemoryCapability {
	return &MemoryCapability{store: store, scope: scope, subjectKey: DefaultMemorySubjectKey}
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
//
// When the scope carries no value for the subject key — "user_id" by
// default, or whatever [WithMemorySubjectKey] declared — tools are NOT
// registered: the LLM simply doesn't see memory as an option. This
// prevents confusing backend errors when the memory store rejects
// operations for an anonymous subject. See AltairaLabs/PromptKit#852.
// The skip is logged at Warn naming both the key looked for and the keys
// the scope actually has, because the symptom otherwise surfaces three
// layers away as "tool not registered" (#1946).
//
// When tools are disabled via [WithMemoryToolsDisabled], no executor or
// tool descriptors are registered at all — the LLM never sees memory as an
// option, but ambient RAG injection still works because the retriever is
// wired separately from this method (see AltairaLabs/PromptKit#1427).
func (c *MemoryCapability) RegisterTools(registry *tools.Registry) {
	if c.toolsDisabled {
		logger.Debug("memory tools skipped: tools disabled (retriever-only mode)")
		return
	}
	if c.subjectKey != "" && c.scope[c.subjectKey] == "" {
		logger.Warn("memory tools skipped: scope has no subject key",
			"expected_key", c.subjectKey,
			"scope_keys", scopeKeys(c.scope))
		return
	}

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

// scopeKeys returns the scope's keys, sorted, for diagnostics. Values are
// never logged — a scope carries host identifiers.
func scopeKeys(scope map[string]string) []string {
	keys := make([]string, 0, len(scope))
	for k := range scope {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
