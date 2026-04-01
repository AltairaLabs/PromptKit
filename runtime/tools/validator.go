package tools

import (
	"container/list"
	"encoding/json"
	"fmt"
	"strconv"
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

	// Treat nil/empty/null args as empty object for validation
	validationArgs := args
	if len(validationArgs) == 0 || string(validationArgs) == "null" {
		validationArgs = json.RawMessage(`{}`)
	}
	argsLoader := gojsonschema.NewBytesLoader(validationArgs)
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

	// Treat nil/empty result as empty object for validation
	validationData := result
	if len(validationData) == 0 || string(validationData) == "null" {
		validationData = json.RawMessage(`{}`)
	}
	resultLoader := gojsonschema.NewBytesLoader(validationData)
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

// CoerceArgs coerces string-encoded values in tool arguments to match the
// types declared in the tool's input schema. LLMs sometimes send numeric
// or boolean values as strings (e.g., "10" instead of 10, "true" instead
// of true). This normalises them before validation and execution.
func (sv *SchemaValidator) CoerceArgs(
	descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, []Coercion, error) {
	if len(descriptor.InputSchema) == 0 {
		return args, nil, nil
	}

	// Parse the schema to get property types
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
		return args, nil, nil // can't parse schema, skip coercion
	}
	if len(schema.Properties) == 0 {
		return args, nil, nil
	}

	// Parse the args
	var data map[string]any
	if err := json.Unmarshal(args, &data); err != nil {
		return args, nil, nil // can't parse args, skip coercion
	}

	var coercions []Coercion
	for key, prop := range schema.Properties {
		str, isString := data[key].(string)
		if !isString {
			continue
		}
		coerced, err := coerceStringValue(str, prop.Type)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot coerce %q=%q to %s: %w", key, str, prop.Type, err)
		}
		if coerced != nil {
			data[key] = coerced
			coercions = append(coercions, Coercion{Path: key, From: str, To: coerced})
		}
	}

	if len(coercions) == 0 {
		return args, nil, nil
	}

	coerced, err := json.Marshal(data)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot marshal coerced args: %w", err)
	}
	return coerced, coercions, nil
}

// coerceStringValue converts a string to the target JSON schema type.
// Returns nil if no coercion is needed (e.g., target type is "string").
func coerceStringValue(s, targetType string) (any, error) {
	switch targetType {
	case "integer":
		return strconv.ParseInt(s, 10, 64)
	case "number":
		return strconv.ParseFloat(s, 64)
	case "boolean":
		return strconv.ParseBool(s)
	case "object":
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err != nil {
			return nil, err
		}
		return obj, nil
	case "array":
		var arr []any
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			return nil, err
		}
		return arr, nil
	default:
		return nil, nil
	}
}
