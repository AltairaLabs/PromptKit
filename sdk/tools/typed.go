// Package tools provides typed tool handlers and utilities for SDK v2.
//
// This package extends the basic tool registration provided by [sdk.Conversation.OnTool]
// with additional capabilities:
//
//   - Type-safe handlers using Go generics (deprecated; see OnTyped)
//   - Handler adapters for the runtime tool registry
//   - HTTP tool executors
//   - Pending tool management for HITL workflows
//
// Most users will only need [sdk.Conversation.OnTool] from the main sdk package.
// This sub-package is for advanced use cases.
//
// # Typed Handlers (deprecated)
//
// OnTyped, ToolRegistrar, ToolHandler and TypedHandler are deprecated: they
// never worked with *sdk.Conversation, whose OnTool takes sdk.ToolHandler, not
// the func type ToolRegistrar names. Register the handler with conv.OnTool and
// read its map[string]any arguments:
//
//	conv.OnTool("get_weather", func(args map[string]any) (any, error) {
//	    city, _ := args["city"].(string)
//	    return weatherAPI.GetCurrent(city)
//	})
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// structFieldMeta holds pre-computed metadata for a single struct field.
type structFieldMeta struct {
	index  int    // field index within the struct
	mapKey string // key to look up in the args map
}

// structMeta holds cached reflection metadata for a struct type.
type structMeta struct {
	fields []structFieldMeta
}

// structMetaCache maps reflect.Type → *structMeta. Using sync.Map because
// the cache is read-heavy after warm-up and the key set grows monotonically.
var structMetaCache sync.Map

// getStructMeta returns cached metadata for the given struct type, computing
// it on the first call.
func getStructMeta(rt reflect.Type) *structMeta {
	if cached, ok := structMetaCache.Load(rt); ok {
		return cached.(*structMeta)
	}

	meta := &structMeta{
		fields: make([]structFieldMeta, 0, rt.NumField()),
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		mapKey := f.Tag.Get("map")
		if mapKey == "" {
			mapKey = f.Name
		}
		// Respect "map:\"-\"" to skip fields, and strip options after comma.
		if mapKey == "-" {
			continue
		}
		if idx := strings.IndexByte(mapKey, ','); idx != -1 {
			mapKey = mapKey[:idx]
		}
		meta.fields = append(meta.fields, structFieldMeta{index: i, mapKey: mapKey})
	}

	actual, _ := structMetaCache.LoadOrStore(rt, meta)
	return actual.(*structMeta)
}

// TypedHandler is a function that executes a tool call with typed arguments.
// T must be a struct type; see [OnTyped] for how its fields are populated.
//
// Deprecated: TypedHandler exists only for [OnTyped], which never worked with
// *sdk.Conversation. Use conv.OnTool with a map[string]any handler.
type TypedHandler[T any] func(args T) (any, error)

// OnTyped registers a typed handler for a tool.
//
// The type parameter T must be a struct type. Each exported field is populated
// from the args map entry keyed by:
//   - The "map" struct tag value, or
//   - The field name exactly as written (case-sensitive) if no tag is present,
//     so an untagged City field reads args["City"], not args["city"]
//
// A field tagged `map:"-"` is skipped.
//
// Example:
//
//	type SearchArgs struct {
//	    Query    string `map:"query"`
//	    MaxResults int  `map:"max_results"`
//	}
//
//	tools.OnTyped(reg, "search", func(args SearchArgs) (any, error) {
//	    return search(args.Query, args.MaxResults)
//	})
//
// Deprecated: OnTyped never worked with *sdk.Conversation: its OnTool takes
// sdk.ToolHandler, so *sdk.Conversation does not implement [ToolRegistrar] and
// the call does not compile. Use conv.OnTool with a map[string]any handler.
func OnTyped[T any](conv ToolRegistrar, name string, handler TypedHandler[T]) {
	conv.OnTool(name, func(args map[string]any) (any, error) {
		var typedArgs T
		if err := mapToStruct(args, &typedArgs); err != nil {
			return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
		}
		return handler(typedArgs)
	})
}

// ToolRegistrar is the interface [OnTyped] registers handlers through.
//
// Deprecated: *sdk.Conversation does not implement ToolRegistrar (its OnTool
// takes sdk.ToolHandler), so OnTyped never worked with it. Use conv.OnTool
// with a map[string]any handler.
type ToolRegistrar interface {
	OnTool(name string, handler ToolHandler)
}

// ToolHandler is the handler type [ToolRegistrar] takes.
//
// Deprecated: it is not sdk.ToolHandler, which is a distinct defined type, so
// it never let *sdk.Conversation implement [ToolRegistrar]. Use conv.OnTool
// with an sdk.ToolHandler.
type ToolHandler = func(args map[string]any) (any, error)

// mapToStruct converts a map[string]any to a struct using reflection.
// It looks for "map" struct tags or uses the field name as-is.
// Field metadata is cached per type via structMetaCache to avoid repeated
// tag parsing and field enumeration on hot paths with many tool calls.
func mapToStruct(m map[string]any, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("v must be a non-nil pointer to a struct")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("v must be a pointer to a struct")
	}

	rt := rv.Type()
	meta := getStructMeta(rt)

	for _, fm := range meta.fields {
		mapVal, ok := m[fm.mapKey]
		if !ok {
			continue
		}

		fieldValue := rv.Field(fm.index)
		if err := setFieldValue(fieldValue, mapVal); err != nil {
			return fmt.Errorf("failed to set field %s: %w", rt.Field(fm.index).Name, err)
		}
	}

	return nil
}

// setFieldValue sets a reflect.Value from an interface value.
func setFieldValue(field reflect.Value, val any) error {
	if val == nil {
		return nil
	}

	fieldType := field.Type()
	valReflect := reflect.ValueOf(val)

	// Direct assignment if types match
	if valReflect.Type().AssignableTo(fieldType) {
		field.Set(valReflect)
		return nil
	}

	// Handle numeric conversions (JSON numbers are float64)
	if valReflect.Kind() == reflect.Float64 {
		//nolint:exhaustive // Only handling numeric types, others fall through to JSON
		switch fieldType.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			field.SetInt(int64(val.(float64)))
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			field.SetUint(uint64(val.(float64)))
			return nil
		case reflect.Float32:
			field.SetFloat(val.(float64))
			return nil
		default:
			// Fall through to JSON marshal/unmarshal
		}
	}

	// Try JSON marshal/unmarshal for complex types
	jsonBytes, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("cannot marshal value: %w", err)
	}

	newVal := reflect.New(fieldType)
	if err := json.Unmarshal(jsonBytes, newVal.Interface()); err != nil {
		return fmt.Errorf("cannot unmarshal to %s: %w", fieldType.String(), err)
	}

	field.Set(newVal.Elem())
	return nil
}

// HandlerAdapter adapts an SDK handler to the runtime's tools.Executor interface.
type HandlerAdapter struct {
	name    string
	handler func(args map[string]any) (any, error)
}

// NewHandlerAdapter creates a new adapter for the given handler.
func NewHandlerAdapter(name string, handler func(args map[string]any) (any, error)) *HandlerAdapter {
	return &HandlerAdapter{name: name, handler: handler}
}

// Name returns the tool name.
func (a *HandlerAdapter) Name() string {
	return a.name
}

// Execute runs the handler with the given arguments.
func (a *HandlerAdapter) Execute(
	_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	// Parse args to map
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	// Call handler
	result, err := a.handler(argsMap)
	if err != nil {
		return nil, err
	}

	// Serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tool result: %w", err)
	}

	return resultJSON, nil
}
