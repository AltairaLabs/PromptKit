package stage

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// PipelineBuilder constructs a pipeline DAG.
// It provides methods for creating linear chains and branching topologies.
type PipelineBuilder struct {
	stages       []Stage
	edges        map[string][]string // stage name -> downstream stage names
	config       *PipelineConfig
	eventEmitter *events.Emitter
}

// NewPipelineBuilder creates a new PipelineBuilder with default configuration.
func NewPipelineBuilder() *PipelineBuilder {
	return &PipelineBuilder{
		stages: []Stage{},
		edges:  make(map[string][]string),
		config: DefaultPipelineConfig(),
	}
}

// NewPipelineBuilderWithConfig creates a new PipelineBuilder with custom configuration.
func NewPipelineBuilderWithConfig(config *PipelineConfig) *PipelineBuilder {
	if config == nil {
		config = DefaultPipelineConfig()
	}
	return &PipelineBuilder{
		stages: []Stage{},
		edges:  make(map[string][]string),
		config: config,
	}
}

// WithConfig sets the pipeline configuration.
func (b *PipelineBuilder) WithConfig(config *PipelineConfig) *PipelineBuilder {
	b.config = config
	return b
}

// WithEventEmitter sets the event emitter for the pipeline.
func (b *PipelineBuilder) WithEventEmitter(emitter *events.Emitter) *PipelineBuilder {
	b.eventEmitter = emitter
	return b
}

// AddStage adds a stage to the builder without connecting it.
// This is useful when building complex topologies manually.
func (b *PipelineBuilder) AddStage(stage Stage) *PipelineBuilder {
	b.stages = append(b.stages, stage)
	return b
}

// Chain creates a linear chain of stages.
// This is the most common pattern: stage1 -> stage2 -> stage3.
// Each stage's output is connected to the next stage's input.
//
// Example:
//
//	pipeline := NewPipelineBuilder().
//	    Chain(
//	        NewStageA(),
//	        NewStageB(),
//	        NewStageC(),
//	    ).
//	    Build()
func (b *PipelineBuilder) Chain(stages ...Stage) *PipelineBuilder {
	if len(stages) == 0 {
		return b
	}

	// Add all stages
	b.stages = append(b.stages, stages...)

	// Connect them in sequence
	for i := 0; i < len(stages)-1; i++ {
		b.Connect(stages[i].Name(), stages[i+1].Name())
	}

	return b
}

// Connect creates a directed edge from one stage to another.
// The output of fromStage will be connected to the input of toStage.
func (b *PipelineBuilder) Connect(fromStage, toStage string) *PipelineBuilder {
	if b.edges[fromStage] == nil {
		b.edges[fromStage] = []string{}
	}
	b.edges[fromStage] = append(b.edges[fromStage], toStage)
	return b
}

// Branch creates multiple outgoing connections from a single stage.
// This allows one stage's output to fan out to multiple downstream stages.
//
// Example:
//
//	pipeline := NewPipelineBuilder().
//	    Chain(NewStageA(), NewStageB()).
//	    Branch("stageB", "stageC", "stageD").  // B's output goes to both C and D
//	    Build()
func (b *PipelineBuilder) Branch(fromStage string, toStages ...string) *PipelineBuilder {
	for _, toStage := range toStages {
		b.Connect(fromStage, toStage)
	}
	return b
}

// Merge wires multiple upstream stages into a single downstream fan-in node.
// The target stage must implement MultiInputStage (e.g. MergeStage).
func (b *PipelineBuilder) Merge(into string, from ...string) *PipelineBuilder {
	for _, f := range from {
		b.Connect(f, into)
	}
	return b
}

// Build constructs the pipeline from the builder's configuration.
// It validates the pipeline structure and returns an error if invalid.
func (b *PipelineBuilder) Build() (*StreamPipeline, error) {
	// Validate the pipeline
	if err := b.validate(); err != nil {
		return nil, err
	}

	// Precompute root stages (stages with no incoming edges) and
	// reverse-edge adjacency map for O(1) upstream lookup. Fan-in aware:
	// a stage may have multiple upstreams (see MultiInputStage).
	reverseEdges := make(map[string][]string)
	hasIncoming := make(map[string]struct{})
	for fromStage, toStages := range b.edges {
		for _, toStage := range toStages {
			hasIncoming[toStage] = struct{}{}
			reverseEdges[toStage] = append(reverseEdges[toStage], fromStage)
		}
	}
	rootStages := make(map[string]struct{})
	for _, s := range b.stages {
		if _, ok := hasIncoming[s.Name()]; !ok {
			rootStages[s.Name()] = struct{}{}
		}
	}

	// Precompute the set of stages implementing MultiOutputStage (selective
	// fan-out / 1:N routing, e.g. RouterStage), keyed by name and holding the
	// asserted MultiOutputStage reference, so getStageInputs can decide, per
	// upstream, whether a downstream reads a dedicated per-edge channel or the
	// upstream's shared output channel (linear / competitive Branch fan-out).
	// This is the single source of truth for "which stages are multi-output" —
	// registerMultiOutputEdges reuses it instead of re-asserting per Execute().
	multiOutputStages := make(map[string]MultiOutputStage)
	for _, s := range b.stages {
		if mo, ok := s.(MultiOutputStage); ok {
			multiOutputStages[s.Name()] = mo
		}
	}

	// Build the pipeline
	return &StreamPipeline{
		stages:            b.stages,
		edges:             b.edges,
		reverseEdges:      reverseEdges,
		rootStages:        rootStages,
		multiOutputStages: multiOutputStages,
		config:            b.config,
		eventEmitter:      b.eventEmitter,
		shutdown:          make(chan struct{}),
	}, nil
}

// validate checks if the pipeline configuration is valid.
func (b *PipelineBuilder) validate() error {
	// Check if there are any stages
	if len(b.stages) == 0 {
		return ErrNoStages
	}

	// Validate config
	if err := b.config.Validate(); err != nil {
		return err
	}

	// Check for duplicate stage names
	stageNames := make(map[string]bool)
	for _, stage := range b.stages {
		if stageNames[stage.Name()] {
			return fmt.Errorf("%w: %s", ErrDuplicateStageName, stage.Name())
		}
		stageNames[stage.Name()] = true
	}

	// Validate all edges reference existing stages
	for fromStage, toStages := range b.edges {
		if !stageNames[fromStage] {
			return fmt.Errorf("%w: %s (referenced in edges)", ErrStageNotFound, fromStage)
		}
		for _, toStage := range toStages {
			if !stageNames[toStage] {
				return fmt.Errorf("%w: %s (referenced in edges from %s)", ErrStageNotFound, toStage, fromStage)
			}
		}
	}

	// Check for cycles using depth-first search
	if err := b.detectCycles(); err != nil {
		return err
	}

	// Reject fan-in into a stage that cannot handle multiple upstreams.
	if err := b.validateFanIn(stageNames); err != nil {
		return err
	}

	// Validate format capabilities between connected stages (logs warnings)
	ValidateCapabilities(b.stages, b.edges)

	return nil
}

// validateFanIn rejects a stage with more than one upstream edge unless it
// implements MultiInputStage (e.g. MergeStage). stageNames is passed in from
// validate() since it has already confirmed all stage names are unique.
// It also detects and rejects duplicate upstream edges into any stage.
func (b *PipelineBuilder) validateFanIn(stageNames map[string]bool) error {
	upstreamCount, upstreamPairs := b.buildUpstreamMaps()
	if err := checkDuplicateFanInEdges(upstreamPairs); err != nil {
		return err
	}
	return b.checkFanInSupport(upstreamCount, stageNames)
}

// buildUpstreamMaps tallies, per destination stage, the number of incoming
// edges (upstreamCount) and the count of edges from each source
// (upstreamPairs), the latter used to detect duplicate edges.
func (b *PipelineBuilder) buildUpstreamMaps() (upstreamCount map[string]int, upstreamPairs map[string]map[string]int) {
	upstreamCount = make(map[string]int)
	upstreamPairs = make(map[string]map[string]int)
	for fromStage, toStages := range b.edges {
		for _, toStage := range toStages {
			upstreamCount[toStage]++
			if upstreamPairs[toStage] == nil {
				upstreamPairs[toStage] = make(map[string]int)
			}
			upstreamPairs[toStage][fromStage]++
		}
	}
	return upstreamCount, upstreamPairs
}

// checkDuplicateFanInEdges rejects more than one edge between the same
// (source, destination) stage pair.
func checkDuplicateFanInEdges(upstreamPairs map[string]map[string]int) error {
	for toStage, upstreams := range upstreamPairs {
		for fromStage, count := range upstreams {
			if count > 1 {
				return fmt.Errorf("%w: stage %s has %d edges from %s",
					ErrDuplicateFanInEdge, toStage, count, fromStage)
			}
		}
	}
	return nil
}

// checkFanInSupport rejects a stage that receives multiple inputs unless it
// implements MultiInputStage. Unknown stage names are ignored here — the
// edge-reference check reports them.
func (b *PipelineBuilder) checkFanInSupport(upstreamCount map[string]int, stageNames map[string]bool) error {
	stageByName := make(map[string]Stage, len(b.stages))
	for _, s := range b.stages {
		stageByName[s.Name()] = s
	}
	for name, n := range upstreamCount {
		if !stageNames[name] || n <= 1 {
			continue
		}
		if _, ok := stageByName[name].(MultiInputStage); !ok {
			return fmt.Errorf("%w: %s", ErrFanInNotSupported, name)
		}
	}
	return nil
}

// detectCycles checks if the pipeline DAG contains cycles.
func (b *PipelineBuilder) detectCycles() error {
	detector := &cycleDetector{
		graph:    b.edges,
		visited:  make(map[string]bool),
		recStack: make(map[string]bool),
	}

	for _, stage := range b.stages {
		if detector.hasCycleFrom(stage.Name()) {
			return ErrCyclicDependency
		}
	}

	return nil
}

// cycleDetector implements DFS-based cycle detection for a directed graph.
type cycleDetector struct {
	graph    map[string][]string
	visited  map[string]bool
	recStack map[string]bool
}

// hasCycleFrom checks if there's a cycle starting from the given node.
func (d *cycleDetector) hasCycleFrom(node string) bool {
	if d.visited[node] {
		return false
	}
	return d.dfs(node)
}

// dfs performs depth-first search to detect cycles.
func (d *cycleDetector) dfs(node string) bool {
	d.visited[node] = true
	d.recStack[node] = true

	if d.hasNeighborCycle(node) {
		return true
	}

	d.recStack[node] = false
	return false
}

// hasNeighborCycle checks if any neighbor creates a cycle.
func (d *cycleDetector) hasNeighborCycle(node string) bool {
	for _, neighbor := range d.graph[node] {
		if d.recStack[neighbor] {
			return true
		}
		if !d.visited[neighbor] && d.dfs(neighbor) {
			return true
		}
	}
	return false
}

// Clone creates a deep copy of the builder.
func (b *PipelineBuilder) Clone() *PipelineBuilder {
	clone := &PipelineBuilder{
		stages:       make([]Stage, len(b.stages)),
		edges:        make(map[string][]string),
		config:       b.config, // Config is immutable, so can share
		eventEmitter: b.eventEmitter,
	}

	// Copy stages
	copy(clone.stages, b.stages)

	// Deep copy edges
	for fromStage, toStages := range b.edges {
		clone.edges[fromStage] = make([]string, len(toStages))
		copy(clone.edges[fromStage], toStages)
	}

	return clone
}
