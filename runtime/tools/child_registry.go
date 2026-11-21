package tools

import "errors"

// Child returns a registry that shares this one's tool DESCRIPTORS but owns its
// own EXECUTORS.
//
// Executors are keyed by name, one per name, and several hold per-conversation
// state, so conversations must not share them. Descriptors are data, and a host
// that passes a registry in with WithToolRegistry reads it back to inspect and
// override the tool set. So Register and Unregister act on the parent, and only
// RegisterExecutor stays local to the child.
//
// Executor lookup falls through to the parent only for names the child never
// registered, so a host's own custom executor is still used. Descriptor lookup
// is live, not a snapshot: a tool registered on the parent after the child was
// created is visible to the child.
//
// Child is nil-receiver safe: a nil parent yields a standalone registry, so a
// caller can write reg = hostRegistry.Child() without checking whether the
// host supplied one. The child takes the parent's default timeout, result size
// limit and rate limiter, and options passed to Child override them.
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
//
// A descriptor registered with RegisterLocal resolves last, so a tool the
// registry or its parent chain holds under the same name always wins over it.
func (r *Registry) lookupTool(name string) (*ToolDescriptor, bool) {
	for reg := r; reg != nil; reg = reg.parent {
		reg.mu.RLock()
		tool, ok := reg.tools[name]
		reg.mu.RUnlock()
		if ok {
			return tool, true
		}
	}
	if tool := r.localTool(name); tool != nil {
		return tool, true
	}
	return nil, false
}

// RegisterLocal registers a descriptor on this registry alone: unlike
// Register, never on the parent of a registry made with Child, so it is
// invisible to the parent and to the parent's other children. Get, GetTool
// and tool execution resolve it, after every descriptor the registry and its
// parent chain hold, so one registered there under the same name, even
// later, wins over it. List, GetTools, IterateTools and GetByNamespace do not
// include it. It replaces a local descriptor of the same name, and sets its
// Namespace from its name.
//
// It is for a descriptor that belongs to one conversation's run, such as a
// tool an application offers the model at runtime.
func (r *Registry) RegisterLocal(descriptor *ToolDescriptor) error {
	if descriptor == nil {
		return errors.New("tool registry: cannot register a nil descriptor")
	}
	if descriptor.Name == "" {
		return errors.New("tool registry: cannot register a descriptor with an empty name")
	}
	_ = r.validator.ValidateArgs(descriptor, []byte("{}"))
	descriptor.Namespace, _ = ParseToolName(descriptor.Name)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.local == nil {
		r.local = make(map[string]*ToolDescriptor)
	}
	r.local[descriptor.Name] = descriptor
	return nil
}

// UnregisterLocal removes a descriptor RegisterLocal registered, and reports
// whether there was one.
func (r *Registry) UnregisterLocal(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.local[name]
	delete(r.local, name)
	return ok
}

// localTool returns the descriptor RegisterLocal registered under name, or nil.
func (r *Registry) localTool(name string) *ToolDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.local[name]
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
