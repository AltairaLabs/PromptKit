package tools

// Child returns a registry that shares this one's tool DESCRIPTORS but owns its
// own EXECUTORS.
//
// The split is deliberate. Executors are the dangerous half: they are keyed by
// name, one per name, and several hold per-conversation state, so sharing them
// across conversations is the bug. Descriptors are data, and a host that passes
// a registry in with WithToolRegistry reads it back to inspect and override the
// tool set -- sdk/integration/contract_tool_overrides_test.go asserts exactly
// that. So Register writes through to the parent and only RegisterExecutor
// stays local.
//
// A Registry keys executors by name and holds exactly one per name, so a host
// that shares a single registry across concurrent conversations had each
// conversation's executors overwrite the previous one's -- and with them any
// per-conversation state those executors held. Giving each conversation a child
// makes that unrepresentable rather than merely avoided: RegisterExecutor
// writes to the child, and executor lookup falls through to the parent only for
// names the child never claimed, so a host's own custom executor is still used.
// See AltairaLabs/PromptKit#2011.
//
// Descriptor lookup is live, not a snapshot. A tool registered on the parent
// after the child was created is visible to the child, which is what a
// copy-at-creation child would get wrong.
//
// Child is nil-receiver safe: a nil parent yields a standalone registry, so a
// caller can write reg = hostRegistry.Child() without branching on whether the
// host supplied one.
func (r *Registry) Child(opts ...RegistryOption) *Registry {
	child := newRegistry(nil, opts...)
	if r == nil {
		return child
	}

	r.mu.RLock()
	child.defaultTimeoutMs = r.defaultTimeoutMs
	child.maxToolResultSize = r.maxToolResultSize
	child.rateLimiter = r.rateLimiter
	r.mu.RUnlock()

	for _, opt := range opts {
		opt(child)
	}
	child.parent = r
	return child
}

// lookupTool resolves a descriptor in this registry, falling back to the parent
// chain. A child normally holds no descriptors of its own -- Register writes
// through -- so this is the parent's map in practice; the local check remains so
// a registry built standalone still works.
func (r *Registry) lookupTool(name string) (*ToolDescriptor, bool) {
	for reg := r; reg != nil; reg = reg.parent {
		reg.mu.RLock()
		tool, ok := reg.tools[name]
		reg.mu.RUnlock()
		if ok {
			return tool, true
		}
	}
	return nil, false
}

// lookupExecutor resolves an executor by name in this registry, falling back to
// the parent chain, so an executor the host registered on the registry it
// passed in is still used for names the conversation never claimed.
func (r *Registry) lookupExecutor(name string) (Executor, bool) {
	for reg := r; reg != nil; reg = reg.parent {
		reg.mu.RLock()
		exec, ok := reg.executors[name]
		reg.mu.RUnlock()
		if ok {
			return exec, true
		}
	}
	return nil, false
}

// visibleTools returns every descriptor visible from this registry: its own,
// plus the parent chain's for names it does not shadow.
func (r *Registry) visibleTools() map[string]*ToolDescriptor {
	out := make(map[string]*ToolDescriptor)
	// Walk parent-first so a child's entry overwrites the parent's.
	var chain []*Registry
	for reg := r; reg != nil; reg = reg.parent {
		chain = append(chain, reg)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		reg := chain[i]
		reg.mu.RLock()
		for name, tool := range reg.tools {
			out[name] = tool
		}
		reg.mu.RUnlock()
	}
	return out
}
