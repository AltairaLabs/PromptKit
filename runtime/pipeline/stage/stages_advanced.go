package stage

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// RouterFunc determines which output channel(s) to route an element to.
// Returns a slice of output names. Empty slice means drop the element.
type RouterFunc func(elem *StreamElement) []string

// RouterStage routes elements to different output channels based on a routing function.
// This enables conditional branching and dynamic routing in the pipeline.
//
// This is a special stage type that supports multiple outputs (1:N routing).
type RouterStage struct {
	BaseStage
	routerFunc RouterFunc
	outputs    map[string]chan<- StreamElement
	mu         sync.RWMutex
}

// NewRouterStage creates a new router stage with the given routing function.
func NewRouterStage(name string, routerFunc RouterFunc) *RouterStage {
	return &RouterStage{
		BaseStage:  NewBaseStage(name, StageTypeTransform),
		routerFunc: routerFunc,
		outputs:    make(map[string]chan<- StreamElement),
	}
}

// RegisterOutput registers an output channel with a name.
// This must be called before Process() to set up routing destinations.
func (s *RouterStage) RegisterOutput(name string, output chan<- StreamElement) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outputs[name] = output
}

// Process implements the Stage interface.
// Routes each element to appropriate output channel(s) based on routing function.
func (s *RouterStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)
	defer s.closeAllOutputs()

	for elem := range input {
		// Get routing destinations
		destinations := s.routerFunc(&elem)

		if len(destinations) == 0 {
			logger.Debug("RouterStage: element dropped (no destinations)", "stage", s.Name())
			continue
		}

		// Route to each destination
		for _, dest := range destinations {
			if err := s.routeToDestination(ctx, dest, elem); err != nil {
				logger.Error("RouterStage: failed to route element", "destination", dest, "error", err)
				// Continue routing to other destinations
			}
		}
	}

	return nil
}

// routeToDestination sends an element to a specific destination.
func (s *RouterStage) routeToDestination(ctx context.Context, dest string, elem StreamElement) error {
	s.mu.RLock()
	outputChan, exists := s.outputs[dest]
	s.mu.RUnlock()

	if !exists {
		return errors.New("destination not found: " + dest)
	}

	select {
	case outputChan <- elem:
		logger.Debug("RouterStage: routed element", "destination", dest)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// closeAllOutputs closes all registered output channels.
func (s *RouterStage) closeAllOutputs() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for name, ch := range s.outputs {
		close(ch)
		logger.Debug("RouterStage: closed output", "name", name)
	}
}

// MergeStage merges multiple input channels into a single output channel.
// This enables fan-in patterns where multiple stages feed into one.
//
// This is an Accumulate stage type that handles multiple inputs (N:1 merge).
type MergeStage struct {
	BaseStage
	inputCount int
}

// NewMergeStage creates a new merge stage that merges N inputs into 1 output.
func NewMergeStage(name string, inputCount int) *MergeStage {
	return &MergeStage{
		BaseStage:  NewBaseStage(name, StageTypeAccumulate),
		inputCount: inputCount,
	}
}

// ProcessMultiple processes multiple input channels and merges them into one output.
// This is a special method for merge stages that differs from the standard Process signature.
func (s *MergeStage) ProcessMultiple(
	ctx context.Context,
	inputs []<-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	if len(inputs) != s.inputCount {
		return errors.New("merge stage: input count mismatch")
	}

	// Use WaitGroup to track all input goroutines
	var wg sync.WaitGroup
	wg.Add(len(inputs))

	// Spawn a goroutine for each input channel
	for i, input := range inputs {
		go func(idx int, in <-chan StreamElement) {
			defer wg.Done()
			s.forwardInput(ctx, idx, in, output)
		}(i, input)
	}

	// Wait for all inputs to complete
	wg.Wait()

	logger.Debug("MergeStage: all inputs merged", "stage", s.Name())
	return nil
}

// Process implements the Stage interface (single input).
// For merge stage, this is not typically used - use ProcessMultiple instead.
func (s *MergeStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	// Single input mode - just forward elements
	defer close(output)

	for elem := range input {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// forwardInput forwards elements from one input to the output.
func (s *MergeStage) forwardInput(
	ctx context.Context,
	inputIdx int,
	input <-chan StreamElement,
	output chan<- StreamElement,
) {
	for elem := range input {
		// Add input source metadata
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}
		elem.Metadata["merge_input_index"] = inputIdx

		select {
		case output <- elem:
			logger.Debug("MergeStage: forwarded element", "input", inputIdx)
		case <-ctx.Done():
			return
		}
	}
}

// StageMetrics contains performance metrics for a stage.
type StageMetrics struct {
	StageName       string
	ElementsIn      int64
	ElementsOut     int64
	ElementsErrored int64
	TotalLatency    time.Duration
	MinLatency      time.Duration
	MaxLatency      time.Duration
	AvgLatency      time.Duration
	LastUpdated     time.Time
	mu              sync.RWMutex
}

// NewStageMetrics creates a new metrics collector for a stage.
func NewStageMetrics(stageName string) *StageMetrics {
	return &StageMetrics{
		StageName:   stageName,
		LastUpdated: time.Now(),
		MinLatency:  time.Hour, // Initialize to high value
	}
}

// RecordElement records metrics for a processed element.
func (m *StageMetrics) RecordElement(latency time.Duration, hasError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ElementsIn++
	if hasError {
		m.ElementsErrored++
	} else {
		m.ElementsOut++
	}

	m.TotalLatency += latency

	if latency < m.MinLatency {
		m.MinLatency = latency
	}
	if latency > m.MaxLatency {
		m.MaxLatency = latency
	}

	if m.ElementsOut > 0 {
		m.AvgLatency = m.TotalLatency / time.Duration(m.ElementsOut)
	}

	m.LastUpdated = time.Now()
}

// GetMetrics returns a copy of the current metrics (thread-safe).
func (m *StageMetrics) GetMetrics() StageMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return StageMetrics{
		StageName:       m.StageName,
		ElementsIn:      m.ElementsIn,
		ElementsOut:     m.ElementsOut,
		ElementsErrored: m.ElementsErrored,
		TotalLatency:    m.TotalLatency,
		MinLatency:      m.MinLatency,
		MaxLatency:      m.MaxLatency,
		AvgLatency:      m.AvgLatency,
		LastUpdated:     m.LastUpdated,
	}
}

// Reset resets all metrics to zero.
func (m *StageMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ElementsIn = 0
	m.ElementsOut = 0
	m.ElementsErrored = 0
	m.TotalLatency = 0
	m.MinLatency = time.Hour
	m.MaxLatency = 0
	m.AvgLatency = 0
	m.LastUpdated = time.Now()
}

// MetricsStage wraps another stage and collects metrics about its performance.
// This is a transparent wrapper that doesn't modify element flow.
type MetricsStage struct {
	BaseStage
	wrappedStage Stage
	metrics      *StageMetrics
}

// NewMetricsStage wraps a stage with metrics collection.
func NewMetricsStage(wrappedStage Stage) *MetricsStage {
	return &MetricsStage{
		BaseStage:    NewBaseStage(wrappedStage.Name()+"_metrics", StageTypeTransform),
		wrappedStage: wrappedStage,
		metrics:      NewStageMetrics(wrappedStage.Name()),
	}
}

// Process implements the Stage interface with metrics collection.
func (s *MetricsStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	// Create intermediate channel to intercept elements
	intermediate := make(chan StreamElement, 16)

	// Process elements through wrapped stage in a goroutine
	errChan := make(chan error, 1)
	go func() {
		err := s.wrappedStage.Process(ctx, input, intermediate)
		errChan <- err
	}()

	// Forward elements from intermediate to output while collecting metrics
	for elem := range intermediate {
		startTime := time.Now()

		// Check if element has error
		hasError := elem.Error != nil

		// Forward element
		select {
		case output <- elem:
			// Record metrics
			latency := time.Since(startTime)
			s.metrics.RecordElement(latency, hasError)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	close(output)

	// Wait for wrapped stage to complete
	return <-errChan
}

// GetMetrics returns the collected metrics.
func (s *MetricsStage) GetMetrics() StageMetrics {
	return s.metrics.GetMetrics()
}

// PriorityChannel is a channel that supports priority-based element delivery.
// Higher priority elements are delivered before lower priority elements.
type PriorityChannel struct {
	queues   [4][]StreamElement // One queue per priority level
	cond     *sync.Cond
	closed   bool
	mu       sync.Mutex
	size     int // Total number of elements across all queues
	capacity int // Maximum total capacity
}

// NewPriorityChannel creates a new priority channel with the given capacity.
func NewPriorityChannel(capacity int) *PriorityChannel {
	pc := &PriorityChannel{
		capacity: capacity,
	}
	pc.cond = sync.NewCond(&pc.mu)
	return pc
}

// Send sends an element to the priority channel.
// Blocks if the channel is at capacity.
func (pc *PriorityChannel) Send(ctx context.Context, elem StreamElement) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Wait while at capacity
	for pc.size >= pc.capacity && !pc.closed {
		// Check context before waiting
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		pc.cond.Wait()
	}

	if pc.closed {
		return errors.New("priority channel closed")
	}

	// Add to appropriate queue based on priority
	priority := elem.Priority
	pc.queues[priority] = append(pc.queues[priority], elem)
	pc.size++

	// Signal waiting receivers
	pc.cond.Signal()
	return nil
}

// Receive receives the highest priority element from the channel.
// Blocks if the channel is empty.
func (pc *PriorityChannel) Receive(ctx context.Context) (StreamElement, bool, error) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Wait while empty
	for pc.size == 0 && !pc.closed {
		// Check context before waiting
		select {
		case <-ctx.Done():
			return StreamElement{}, false, ctx.Err()
		default:
		}
		pc.cond.Wait()
	}

	// If closed and empty, return closed signal
	if pc.size == 0 && pc.closed {
		return StreamElement{}, false, nil
	}

	// Get highest priority element
	// Priority order: Critical > High > Normal > Low
	for priority := PriorityCritical; priority >= PriorityLow; priority-- {
		if len(pc.queues[priority]) > 0 {
			elem := pc.queues[priority][0]
			pc.queues[priority] = pc.queues[priority][1:]
			pc.size--

			// Signal waiting senders
			pc.cond.Signal()
			return elem, true, nil
		}
	}

	// Should never reach here
	return StreamElement{}, false, errors.New("priority channel: inconsistent state")
}

// Close closes the priority channel.
func (pc *PriorityChannel) Close() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	pc.closed = true
	pc.cond.Broadcast()
}

// Len returns the current number of elements in the channel.
func (pc *PriorityChannel) Len() int {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.size
}

// TracingStage wraps another stage and adds element-level tracing.
// Each element gets a trace ID and timing information.
type TracingStage struct {
	BaseStage
	wrappedStage Stage
	traceIDGen   func() string // Function to generate trace IDs
}

// NewTracingStage wraps a stage with tracing support.
func NewTracingStage(wrappedStage Stage, traceIDGen func() string) *TracingStage {
	return &TracingStage{
		BaseStage:    NewBaseStage(wrappedStage.Name()+"_tracing", StageTypeTransform),
		wrappedStage: wrappedStage,
		traceIDGen:   traceIDGen,
	}
}

// Process implements the Stage interface with tracing.
func (s *TracingStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	// Create intermediate channel to intercept elements
	intermediate := make(chan StreamElement, 16)

	// Process elements through wrapped stage in a goroutine
	errChan := make(chan error, 1)
	go func() {
		err := s.wrappedStage.Process(ctx, input, intermediate)
		errChan <- err
	}()

	// Forward elements from intermediate to output while adding trace info
	for elem := range intermediate {
		// Add or update trace metadata
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}

		// Add trace ID if not present
		if _, exists := elem.Metadata["trace_id"]; !exists && s.traceIDGen != nil {
			elem.Metadata["trace_id"] = s.traceIDGen()
		}

		// Add stage timing
		stageTimingKey := "stage_times"
		var stageTimes map[string]time.Time
		if existing, ok := elem.Metadata[stageTimingKey].(map[string]time.Time); ok {
			stageTimes = existing
		} else {
			stageTimes = make(map[string]time.Time)
		}
		stageTimes[s.wrappedStage.Name()] = time.Now()
		elem.Metadata[stageTimingKey] = stageTimes

		// Forward element
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	close(output)

	// Wait for wrapped stage to complete
	return <-errChan
}

// GetTraceInfo extracts trace information from an element.
func GetTraceInfo(elem *StreamElement) (traceID string, stageTimes map[string]time.Time) {
	if elem.Metadata == nil {
		return "", nil
	}

	if id, ok := elem.Metadata["trace_id"].(string); ok {
		traceID = id
	}

	if times, ok := elem.Metadata["stage_times"].(map[string]time.Time); ok {
		stageTimes = times
	}

	return traceID, stageTimes
}
