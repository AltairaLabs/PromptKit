package tools

import (
	"container/list"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
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

// coerceArrayElements coerces string elements in an array to the target item type.
// Returns a new slice if any coercions were applied, nil otherwise.
func coerceArrayElements(arr []any, itemType string) []any {
	result := make([]any, len(arr))
	changed := false
	for i, elem := range arr {
		str, ok := elem.(string)
		if !ok {
			result[i] = elem
			continue
		}
		coerced, err := coerceStringValue(str, itemType)
		if err != nil || coerced == nil {
			result[i] = elem
			continue
		}
		result[i] = coerced
		changed = true
	}
	if !changed {
		return nil
	}
	return result
}

// Coercion represents a type coercion that was performed.
type Coercion struct {
	Path string `json:"path"`
	From any    `json:"from"`
	To   any    `json:"to"`
}

// JSON Schema type names used in coercion logic.
const (
	schemaTypeString  = "string"
	schemaTypeBoolean = "boolean"
	schemaTypeArray   = "array"
)

// schemaItems holds the type of array elements.
type schemaItems struct {
	Type string `json:"type"`
}

// schemaProperty holds parsed schema metadata for a single property.
type schemaProperty struct {
	Type  string      `json:"type"`
	Enum  []string    `json:"enum,omitempty"`
	Items schemaItems `json:"items,omitempty"`
}

// parsedSchema holds the parsed schema metadata needed for coercion.
type parsedSchema struct {
	Properties map[string]schemaProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

// CoerceArgs normalises LLM tool arguments to match the types declared in the
// tool's input schema. Weak LLMs produce a variety of non-conformant output:
//
//   - null for optional fields (Ollama, Llama)
//   - empty strings for non-string optional fields (GPT-3.5, small models)
//   - string-encoded numbers/booleans ("10", "true", "0.8")
//   - bare strings instead of arrays ("preference" instead of ["preference"])
//   - wrong enum case ("escalate" instead of "Escalate")
//   - whitespace around enum values ("Escalate ")
//   - "yes"/"no" for booleans, integer 1/0 for booleans
//   - "5.0" string for integer fields
//
// All normalisation happens here, before ValidateArgs, keeping the schema
// validator strict and tool executors simple.
func (sv *SchemaValidator) CoerceArgs(
	descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, []Coercion, error) {
	if len(descriptor.InputSchema) == 0 {
		return args, nil, nil
	}

	var schema parsedSchema
	if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
		return args, nil, nil
	}
	if len(schema.Properties) == 0 {
		return args, nil, nil
	}

	var data map[string]any
	if err := json.Unmarshal(args, &data); err != nil {
		return args, nil, nil
	}

	requiredSet := make(map[string]bool, len(schema.Required))
	for _, r := range schema.Required {
		requiredSet[r] = true
	}

	var coercions []Coercion

	// Phase 1: strip nulls and empty strings from non-required fields.
	for key, prop := range schema.Properties {
		val, exists := data[key]
		if !exists {
			continue
		}

		// Strip null from non-required fields.
		if val == nil && !requiredSet[key] {
			delete(data, key)
			coercions = append(coercions, Coercion{Path: key, From: nil, To: "<stripped>"})
			continue
		}

		// Strip empty strings from non-required fields when the empty string
		// is not a valid value: non-string types, or string types with an enum constraint.
		if str, ok := val.(string); ok && str == "" && !requiredSet[key] {
			if prop.Type != schemaTypeString || len(prop.Enum) > 0 {
				delete(data, key)
				coercions = append(coercions, Coercion{Path: key, From: "", To: "<stripped>"})
				continue
			}
		}
	}

	// Phase 2: type coercion for remaining fields.
	for key, prop := range schema.Properties {
		val, exists := data[key]
		if !exists {
			continue
		}

		// String value coercion (string → target type).
		if str, ok := val.(string); ok {
			coerced, err := coerceStringValue(str, prop.Type)
			if err != nil {
				return nil, nil, fmt.Errorf("cannot coerce %q=%q to %s: %w", key, str, prop.Type, err)
			}
			if coerced != nil {
				data[key] = coerced
				coercions = append(coercions, Coercion{Path: key, From: str, To: coerced})
			}

			// Enum case/whitespace normalisation for string fields.
			if prop.Type == schemaTypeString && len(prop.Enum) > 0 {
				// Re-read: coercion above returns nil for string→string, so val is unchanged.
				current, _ := data[key].(string)
				if normalized, ok := normalizeEnum(current, prop.Enum); ok {
					data[key] = normalized
					coercions = append(coercions, Coercion{Path: key, From: current, To: normalized})
				}
			}
			continue
		}

		// Non-string coercion: number → boolean (LLMs send 1/0 for booleans).
		if num, ok := val.(float64); ok && prop.Type == schemaTypeBoolean {
			switch num {
			case 0:
				data[key] = false
				coercions = append(coercions, Coercion{Path: key, From: num, To: false})
			case 1:
				data[key] = true
				coercions = append(coercions, Coercion{Path: key, From: num, To: true})
			}
		}

		// Array element coercion: coerce string elements to match items.type.
		isTypedArray := prop.Type == schemaTypeArray &&
			prop.Items.Type != "" && prop.Items.Type != schemaTypeString
		if arr, ok := val.([]any); ok && isTypedArray {
			if coerced := coerceArrayElements(arr, prop.Items.Type); coerced != nil {
				data[key] = coerced
				coercions = append(coercions, Coercion{Path: key, From: "array elements", To: "coerced"})
			}
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

// normalizeEnum tries to match a value against enum entries with whitespace
// trimming and case-insensitive fallback. Returns the canonical enum value
// and true if a match was found that differs from the input.
func normalizeEnum(val string, enum []string) (string, bool) {
	trimmed := strings.TrimSpace(val)

	// Exact match after trimming.
	for _, e := range enum {
		if trimmed == e {
			if trimmed != val {
				return trimmed, true // whitespace was stripped
			}
			return "", false // already correct
		}
	}

	// Case-insensitive match.
	var match string
	matchCount := 0
	for _, e := range enum {
		if strings.EqualFold(e, trimmed) {
			match = e
			matchCount++
		}
	}
	if matchCount == 1 {
		return match, true
	}

	return "", false // no match or ambiguous
}

// coerceStringValue converts a string to the target JSON schema type.
// Returns (nil, nil) if no coercion is defined for targetType (e.g., "string"
// or anything unrecognized — the caller treats that as "leave the value alone").
func coerceStringValue(s, targetType string) (any, error) {
	switch targetType {
	case "integer":
		return coerceStringToInteger(s)
	case "number":
		return strconv.ParseFloat(s, 64)
	case "boolean":
		return coerceStringToBoolean(s)
	case "object":
		return coerceStringToObject(s)
	case "array":
		return coerceStringToArray(s)
	default:
		return nil, nil
	}
}

// coerceStringToInteger parses s as int64 directly, falling back to a
// whole-number float (so "5.0" is acceptable but "3.14" is not).
func coerceStringToInteger(s string) (any, error) {
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f == math.Trunc(f) {
		return int64(f), nil
	}
	return nil, fmt.Errorf("cannot parse %q as integer", s)
}

// coerceStringToBoolean parses s as a bool. Accepts strconv.ParseBool's
// canonical forms ("true"/"false"/"1"/"0"/"t"/"f") and, case-insensitively
// with whitespace trimmed, the natural-language forms "yes" and "no".
func coerceStringToBoolean(s string) (any, error) {
	if v, err := strconv.ParseBool(s); err == nil {
		return v, nil
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	}
	return nil, fmt.Errorf("cannot parse %q as boolean", s)
}

// coerceStringToObject parses s as a JSON object. Returns an error when the
// string isn't valid JSON — unlike array coercion there's no sensible fallback
// for a bare value.
func coerceStringToObject(s string) (any, error) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// coerceStringToArray parses s as a JSON array, falling back to wrapping the
// bare string in a single-element slice so simple tool arguments like "tag1"
// still validate against schemas expecting an array.
func coerceStringToArray(s string) (any, error) {
	var arr []any
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return arr, nil
	}
	return []any{s}, nil
}
