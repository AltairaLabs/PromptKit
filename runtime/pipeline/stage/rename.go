package stage

// prefixedStage wraps a Stage to override its Name(), leaving all other
// behavior to the embedded stage.
type prefixedStage struct {
	Stage
	name string
}

// Name returns the prefixed stage name.
func (p *prefixedStage) Name() string { return p.name }

// WithNamePrefix returns s renamed to "<prefix>_<name>", so the same stage
// constructors can be instantiated once per branch in a single pipeline.
//
// PipelineBuilder.Build rejects duplicate stage names, so a fan-out that runs
// identical sub-chains — one audio track per speaker on a two-party call, one
// per camera, one per tenant — must give each branch's stages distinct names.
// Without a shared helper, every consumer re-derives the same wrapper by
// embedding Stage and overriding Name().
//
// An empty prefix returns s unchanged, which is the single-branch case: the
// constructors' natural names are already unique and no wrapper is warranted.
//
// The original stage is not mutated, so the same instance can be wrapped more
// than once — though note that wrapping shares the underlying stage, including
// any state it holds. Branches needing independent state must construct
// separate stages.
func WithNamePrefix(prefix string, s Stage) Stage {
	if prefix == "" {
		return s
	}
	return &prefixedStage{Stage: s, name: prefix + "_" + s.Name()}
}
