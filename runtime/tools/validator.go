package tools

import (
	"container/list"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

// DefaultMaxSchemaCacheSize is the maximum number of compiled JSON schemas
// held in the validator cache. When full the least-recently-used entry is
// evicted.
const DefaultMaxSchemaCacheSize = 128

// schemaEntry is a single entry in the LRU schema cache.
type schemaEntry struct {
	key    string
	schema *gojsonschema.Schema
}

// SchemaValidator handles JSON schema validation for tool inputs and outputs.
// It maintains an LRU cache of compiled schemas bounded by maxCacheSize.
type SchemaValidator struct {
	cache   map[string]*list.Element
	order   *list.List // front = most recently used
	maxSize int
	mu      sync.RWMutex
}

// NewSchemaValidator creates a new schema validator with the default cache size.
func NewSchemaValidator() *SchemaValidator {
	return NewSchemaValidatorWithSize(DefaultMaxSchemaCacheSize)
}

// NewSchemaValidatorWithSize creates a new schema validator with the given
// maximum cache size. If maxSize <= 0 it defaults to DefaultMaxSchemaCacheSize.
func NewSchemaValidatorWithSize(maxSize int) *SchemaValidator {
	if maxSize <= 0 {
		maxSize = DefaultMaxSchemaCacheSize
	}
	return &SchemaValidator{
		cache:   make(map[string]*list.Element, maxSize),
		order:   list.New(),
		maxSize: maxSize,
	}
}

// ValidateArgs validates tool arguments against the input schema
func (sv *SchemaValidator) ValidateArgs(descriptor *ToolDescriptor, args json.RawMessage) error {
	// Skip validation if no input schema is defined
	if len(descriptor.InputSchema) == 0 {
		return nil
	}

	schema, err := sv.getSchema(string(descriptor.InputSchema))
	if err != nil {
		return fmt.Errorf("invalid input schema for tool %s: %w", descriptor.Name, err)
	}

	argsLoader := gojsonschema.NewBytesLoader(args)
	result, err := schema.Validate(argsLoader)
	if err != nil {
		return fmt.Errorf("validation error for tool %s: %w", descriptor.Name, err)
	}

	if !result.Valid() {
		errors := make([]string, len(result.Errors()))
		for i, desc := range result.Errors() {
			errors[i] = desc.String()
		}
		return &ValidationError{
			Type:   "args_invalid",
			Tool:   descriptor.Name,
			Detail: fmt.Sprintf("argument validation failed: %v", errors),
		}
	}

	return nil
}

// ValidateResult validates tool result against the output schema
func (sv *SchemaValidator) ValidateResult(descriptor *ToolDescriptor, result json.RawMessage) error {
	// Skip validation if no output schema is defined
	if len(descriptor.OutputSchema) == 0 {
		return nil
	}

	schema, err := sv.getSchema(string(descriptor.OutputSchema))
	if err != nil {
		return fmt.Errorf("invalid output schema for tool %s: %w", descriptor.Name, err)
	}

	resultLoader := gojsonschema.NewBytesLoader(result)
	validationResult, err := schema.Validate(resultLoader)
	if err != nil {
		return fmt.Errorf("validation error for tool %s: %w", descriptor.Name, err)
	}

	if !validationResult.Valid() {
		errors := make([]string, len(validationResult.Errors()))
		for i, desc := range validationResult.Errors() {
			errors[i] = desc.String()
		}
		return &ValidationError{
			Type:   "result_invalid",
			Tool:   descriptor.Name,
			Detail: fmt.Sprintf("result validation failed: %v", errors),
		}
	}

	return nil
}

// getSchema retrieves or compiles a JSON schema. Compiled schemas are cached
// in an LRU cache bounded by maxSize; when full the least-recently-used entry
// is evicted.
func (sv *SchemaValidator) getSchema(schemaJSON string) (*gojsonschema.Schema, error) {
	// Fast path: read lock lookup.
	sv.mu.RLock()
	if _, exists := sv.cache[schemaJSON]; exists {
		sv.mu.RUnlock()
		// Promote requires write lock. Re-lookup the element under the write
		// lock because it may have been evicted between releasing the read lock
		// and acquiring the write lock.
		sv.mu.Lock()
		if elem, stillExists := sv.cache[schemaJSON]; stillExists {
			sv.order.MoveToFront(elem)
			schema := elem.Value.(*schemaEntry).schema
			sv.mu.Unlock()
			return schema, nil
		}
		sv.mu.Unlock()
		// Element was evicted; fall through to recompile below.
	} else {
		sv.mu.RUnlock()
	}

	// Compile schema outside of lock.
	schemaLoader := gojsonschema.NewStringLoader(schemaJSON)
	schema, err := gojsonschema.NewSchema(schemaLoader)
	if err != nil {
		return nil, err
	}

	// Write to cache with write lock.
	sv.mu.Lock()
	// Double-check in case another goroutine added it.
	if elem, exists := sv.cache[schemaJSON]; exists {
		sv.order.MoveToFront(elem)
		sv.mu.Unlock()
		return elem.Value.(*schemaEntry).schema, nil
	}

	// Evict LRU if at capacity.
	if sv.order.Len() >= sv.maxSize {
		oldest := sv.order.Back()
		if oldest != nil {
			sv.order.Remove(oldest)
			delete(sv.cache, oldest.Value.(*schemaEntry).key)
		}
	}

	entry := &schemaEntry{key: schemaJSON, schema: schema}
	elem := sv.order.PushFront(entry)
	sv.cache[schemaJSON] = elem
	sv.mu.Unlock()
	return schema, nil
}

// CacheLen returns the number of entries currently in the schema cache.
// Exported for testing and monitoring.
func (sv *SchemaValidator) CacheLen() int {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	return sv.order.Len()
}

// CoerceResult attempts to coerce simple type mismatches in tool results.
//
// Currently this is a pass-through: if the result validates, it is returned as-is;
// otherwise validation is re-attempted after a round-trip through JSON (which
// normalises whitespace/encoding). Actual type coercion (e.g., string↔number)
// is not yet implemented — the Coercion slice is always empty.
func (sv *SchemaValidator) CoerceResult(
	descriptor *ToolDescriptor, result json.RawMessage,
) (json.RawMessage, []Coercion, error) {
	// Fast path: result already validates.
	if err := sv.ValidateResult(descriptor, result); err == nil {
		return result, nil, nil
	}

	// Round-trip through JSON to normalise encoding, then re-validate.
	var data any
	if err := json.Unmarshal(result, &data); err != nil {
		return nil, nil, fmt.Errorf("cannot parse result for coercion: %w", err)
	}

	normalised, err := json.Marshal(data)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot marshal normalised result: %w", err)
	}

	if err := sv.ValidateResult(descriptor, normalised); err != nil {
		return nil, nil, fmt.Errorf("coercion failed: %w", err)
	}

	return normalised, nil, nil
}

// Coercion represents a type coercion that was performed.
type Coercion struct {
	Path string `json:"path"`
	From any    `json:"from"`
	To   any    `json:"to"`
}
