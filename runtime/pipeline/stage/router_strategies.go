// Package stage provides the reactive streams architecture for pipeline execution.
package stage

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"hash/fnv"
	"sync"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// RoutingRule defines a predicate-based routing rule.
type RoutingRule struct {
	// Name identifies this rule for logging/debugging.
	Name string
	// Predicate returns true if the element should be routed to this rule's output.
	Predicate func(StreamElement) bool
	// Output is the destination name for matching elements.
	Output string
}

// RouteWhen creates a routing rule with the given predicate.
func RouteWhen(output string, predicate func(StreamElement) bool) RoutingRule {
	return RoutingRule{
		Name:      output,
		Predicate: predicate,
		Output:    output,
	}
}

// RouteAudio creates a routing rule for audio elements with specific format.
func RouteAudio(output string, format AudioFormat) RoutingRule {
	return RoutingRule{
		Name: output,
		Predicate: func(e StreamElement) bool {
			return e.Audio != nil && e.Audio.Format == format
		},
		Output: output,
	}
}

// RouteContentType creates a routing rule for elements of a specific content type.
func RouteContentType(output string, ct ContentType) RoutingRule {
	return RoutingRule{
		Name: output,
		Predicate: func(e StreamElement) bool {
			switch ct {
			case ContentTypeText:
				return e.Text != nil
			case ContentTypeAudio:
				return e.Audio != nil
			case ContentTypeVideo:
				return e.Video != nil
			case ContentTypeImage:
				return e.Image != nil
			case ContentTypeMessage:
				return e.Message != nil
			case ContentTypeToolCall:
				return e.ToolCall != nil
			case ContentTypeAny:
				return true
			default:
				return false
			}
		},
		Output: output,
	}
}

// ContentRouter routes elements to different outputs based on predicate rules.
// Rules are evaluated in order; the first matching rule determines the destination.
// Elements that don't match any rule are dropped with a warning log.
type ContentRouter struct {
	BaseStage
	rules   []RoutingRule
	outputs map[string]chan<- StreamElement
	mu      sync.RWMutex

	// Track dropped elements for logging
	droppedCount atomic.Int64
}

// NewContentRouter creates a new content-aware router with the given rules.
func NewContentRouter(name string, rules ...RoutingRule) *ContentRouter {
	return &ContentRouter{
		BaseStage: NewBaseStage(name, StageTypeTransform),
		rules:     rules,
		outputs:   make(map[string]chan<- StreamElement),
	}
}

// RegisterOutput registers an output channel with a name.
// This must be called before Process() to set up routing destinations.
func (r *ContentRouter) RegisterOutput(name string, output chan<- StreamElement) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputs[name] = output
}

// Process routes elements based on the configured rules.
func (r *ContentRouter) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		destinations := r.route(elem)

		if len(destinations) == 0 {
			count := r.droppedCount.Add(1)
			if count == 1 || count%100 == 0 {
				logger.Warn("ContentRouter: element dropped (no matching rule)",
					"router", r.Name(),
					"dropped_count", count,
				)
			}
			continue
		}

		r.mu.RLock()
		for _, dest := range destinations {
			if ch, ok := r.outputs[dest]; ok {
				select {
				case ch <- elem:
				case <-ctx.Done():
					r.mu.RUnlock()
					return ctx.Err()
				}
			}
		}
		r.mu.RUnlock()
	}

	return nil
}

// route returns the destination names for the given element.
func (r *ContentRouter) route(elem StreamElement) []string {
	var destinations []string
	for _, rule := range r.rules {
		if rule.Predicate(elem) {
			destinations = append(destinations, rule.Output)
			break // First match wins
		}
	}
	return destinations
}

// RoundRobinRouter distributes elements across outputs in sequence.
type RoundRobinRouter struct {
	BaseStage
	outputNames []string
	outputs     map[string]chan<- StreamElement
	counter     atomic.Uint64
	mu          sync.RWMutex
}

// NewRoundRobinRouter creates a router that cycles through outputs sequentially.
func NewRoundRobinRouter(name string, outputNames []string) *RoundRobinRouter {
	return &RoundRobinRouter{
		BaseStage:   NewBaseStage(name, StageTypeTransform),
		outputNames: outputNames,
		outputs:     make(map[string]chan<- StreamElement),
	}
}

// RegisterOutput registers an output channel with a name.
func (r *RoundRobinRouter) RegisterOutput(name string, output chan<- StreamElement) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputs[name] = output
}

// Process distributes elements in round-robin fashion.
func (r *RoundRobinRouter) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		idx := r.counter.Add(1) - 1
		destName := r.outputNames[idx%uint64(len(r.outputNames))]

		r.mu.RLock()
		if ch, ok := r.outputs[destName]; ok {
			select {
			case ch <- elem:
			case <-ctx.Done():
				r.mu.RUnlock()
				return ctx.Err()
			}
		}
		r.mu.RUnlock()
	}

	return nil
}

// WeightedRouter distributes elements across outputs based on configured weights.
type WeightedRouter struct {
	BaseStage
	weights    map[string]float64
	thresholds []weightThreshold
	outputs    map[string]chan<- StreamElement
	mu         sync.RWMutex
}

type weightThreshold struct {
	name      string
	threshold float64
}

// NewWeightedRouter creates a router that distributes elements based on weights.
// Weights are normalized to sum to 1.0.
// Example: {"primary": 0.7, "secondary": 0.3} routes 70% to primary, 30% to secondary.
func NewWeightedRouter(name string, weights map[string]float64) *WeightedRouter {
	// Normalize weights and build threshold table
	var total float64
	for _, w := range weights {
		total += w
	}

	thresholds := make([]weightThreshold, 0, len(weights))
	cumulative := 0.0
	for name, w := range weights {
		cumulative += w / total
		thresholds = append(thresholds, weightThreshold{
			name:      name,
			threshold: cumulative,
		})
	}

	return &WeightedRouter{
		BaseStage:  NewBaseStage(name, StageTypeTransform),
		weights:    weights,
		thresholds: thresholds,
		outputs:    make(map[string]chan<- StreamElement),
	}
}

// RegisterOutput registers an output channel with a name.
func (r *WeightedRouter) RegisterOutput(name string, output chan<- StreamElement) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputs[name] = output
}

// Process distributes elements based on weights.
func (r *WeightedRouter) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		destName := r.selectDestination()

		r.mu.RLock()
		if ch, ok := r.outputs[destName]; ok {
			select {
			case ch <- elem:
			case <-ctx.Done():
				r.mu.RUnlock()
				return ctx.Err()
			}
		}
		r.mu.RUnlock()
	}

	return nil
}

func (r *WeightedRouter) selectDestination() string {
	v := cryptoRandFloat64()

	for _, t := range r.thresholds {
		if v <= t.threshold {
			return t.name
		}
	}
	// Fallback to last (shouldn't happen due to normalization)
	return r.thresholds[len(r.thresholds)-1].name
}

// cryptoRandFloat64 returns a cryptographically secure random float64 in [0.0, 1.0).
func cryptoRandFloat64() float64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	// Convert to uint64 and normalize to [0.0, 1.0)
	u := binary.LittleEndian.Uint64(b[:])
	return float64(u) / float64(1<<64)
}

// cryptoRandIntn returns a cryptographically secure random int in [0, n).
func cryptoRandIntn(n int) int {
	if n <= 0 {
		return 0
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	u := binary.LittleEndian.Uint64(b[:])
	return int(u % uint64(n))
}

// HashRouter routes elements based on consistent hashing of a key.
// This ensures elements with the same key always go to the same destination.
type HashRouter struct {
	BaseStage
	keyFunc     func(StreamElement) string
	outputNames []string
	outputs     map[string]chan<- StreamElement
	mu          sync.RWMutex
}

// NewHashRouter creates a router that uses consistent hashing.
// The keyFunc extracts a key from each element (e.g., session ID).
// Elements with the same key always route to the same destination.
func NewHashRouter(name string, outputNames []string, keyFunc func(StreamElement) string) *HashRouter {
	return &HashRouter{
		BaseStage:   NewBaseStage(name, StageTypeTransform),
		keyFunc:     keyFunc,
		outputNames: outputNames,
		outputs:     make(map[string]chan<- StreamElement),
	}
}

// RegisterOutput registers an output channel with a name.
func (r *HashRouter) RegisterOutput(name string, output chan<- StreamElement) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputs[name] = output
}

// Process routes elements based on hash of key.
func (r *HashRouter) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		key := r.keyFunc(elem)
		idx := r.hash(key) % uint32(len(r.outputNames))
		destName := r.outputNames[idx]

		r.mu.RLock()
		if ch, ok := r.outputs[destName]; ok {
			select {
			case ch <- elem:
			case <-ctx.Done():
				r.mu.RUnlock()
				return ctx.Err()
			}
		}
		r.mu.RUnlock()
	}

	return nil
}

func (r *HashRouter) hash(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

// RandomRouter distributes elements randomly across outputs.
type RandomRouter struct {
	BaseStage
	outputNames []string
	outputs     map[string]chan<- StreamElement
	mu          sync.RWMutex
}

// NewRandomRouter creates a router that distributes elements randomly.
func NewRandomRouter(name string, outputNames []string) *RandomRouter {
	return &RandomRouter{
		BaseStage:   NewBaseStage(name, StageTypeTransform),
		outputNames: outputNames,
		outputs:     make(map[string]chan<- StreamElement),
	}
}

// RegisterOutput registers an output channel with a name.
func (r *RandomRouter) RegisterOutput(name string, output chan<- StreamElement) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputs[name] = output
}

// Process distributes elements randomly.
func (r *RandomRouter) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		idx := cryptoRandIntn(len(r.outputNames))
		destName := r.outputNames[idx]

		r.mu.RLock()
		if ch, ok := r.outputs[destName]; ok {
			select {
			case ch <- elem:
			case <-ctx.Done():
				r.mu.RUnlock()
				return ctx.Err()
			}
		}
		r.mu.RUnlock()
	}

	return nil
}

// BroadcastRouter sends each element to ALL registered outputs.
// Useful for fan-out scenarios where all consumers need every element.
type BroadcastRouter struct {
	BaseStage
	outputs map[string]chan<- StreamElement
	mu      sync.RWMutex
}

// NewBroadcastRouter creates a router that broadcasts to all outputs.
func NewBroadcastRouter(name string) *BroadcastRouter {
	return &BroadcastRouter{
		BaseStage: NewBaseStage(name, StageTypeTransform),
		outputs:   make(map[string]chan<- StreamElement),
	}
}

// RegisterOutput registers an output channel with a name.
func (r *BroadcastRouter) RegisterOutput(name string, output chan<- StreamElement) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputs[name] = output
}

// Process broadcasts each element to all outputs.
func (r *BroadcastRouter) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		r.mu.RLock()
		for _, ch := range r.outputs {
			select {
			case ch <- elem:
			case <-ctx.Done():
				r.mu.RUnlock()
				return ctx.Err()
			}
		}
		r.mu.RUnlock()
	}

	return nil
}
