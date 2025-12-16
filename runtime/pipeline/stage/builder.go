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

// Build constructs the pipeline from the builder's configuration.
// It validates the pipeline structure and returns an error if invalid.
func (b *PipelineBuilder) Build() (*StreamPipeline, error) {
	// Validate the pipeline
	if err := b.validate(); err != nil {
		return nil, err
	}

	// Build the pipeline
	return &StreamPipeline{
		stages:       b.stages,
		edges:        b.edges,
		config:       b.config,
		eventEmitter: b.eventEmitter,
		shutdown:     make(chan struct{}),
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
	return b.detectCycles()
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
