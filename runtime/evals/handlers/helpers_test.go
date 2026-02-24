package handlers

import (
	"testing"
)

func TestAsString_NonString(t *testing.T) {
	// Cover the fmt.Sprintf branch for non-string values.
	tests := []struct {
		input    any
		expected string
	}{
		{42, "42"},
		{3.14, "3.14"},
		{true, "true"},
		{nil, "<nil>"},
		{[]int{1, 2}, "[1 2]"},
	}
	for _, tt := range tests {
		got := asString(tt.input)
		if got != tt.expected {
			t.Errorf("asString(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestAsString_String(t *testing.T) {
	got := asString("hello")
	if got != "hello" {
		t.Errorf("asString(\"hello\") = %q, want \"hello\"", got)
	}
}

func TestExtractFloat64Ptr_Float64(t *testing.T) {
	params := map[string]any{"val": 3.14}
	result := extractFloat64Ptr(params, "val")
	if result == nil || *result != 3.14 {
		t.Fatalf("expected 3.14, got %v", result)
	}
}

func TestExtractFloat64Ptr_Int(t *testing.T) {
	params := map[string]any{"val": 42}
	result := extractFloat64Ptr(params, "val")
	if result == nil || *result != 42.0 {
		t.Fatalf("expected 42.0, got %v", result)
	}
}

func TestExtractFloat64Ptr_Missing(t *testing.T) {
	params := map[string]any{}
	result := extractFloat64Ptr(params, "val")
	if result != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestExtractFloat64Ptr_UnsupportedType(t *testing.T) {
	params := map[string]any{"val": "not a number"}
	result := extractFloat64Ptr(params, "val")
	if result != nil {
		t.Fatal("expected nil for unsupported type")
	}
}

func TestExtractIntPtr_Int(t *testing.T) {
	params := map[string]any{"val": 42}
	result := extractIntPtr(params, "val")
	if result == nil || *result != 42 {
		t.Fatalf("expected 42, got %v", result)
	}
}

func TestExtractIntPtr_Float64(t *testing.T) {
	params := map[string]any{"val": 3.7}
	result := extractIntPtr(params, "val")
	if result == nil || *result != 3 {
		t.Fatalf("expected 3, got %v", result)
	}
}

func TestExtractIntPtr_Missing(t *testing.T) {
	params := map[string]any{}
	result := extractIntPtr(params, "val")
	if result != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestExtractIntPtr_UnsupportedType(t *testing.T) {
	params := map[string]any{"val": "not a number"}
	result := extractIntPtr(params, "val")
	if result != nil {
		t.Fatal("expected nil for unsupported type")
	}
}

func TestExtractMapStringString_MapStringAny(t *testing.T) {
	params := map[string]any{
		"data": map[string]any{"key1": "val1", "key2": "val2"},
	}
	result := extractMapStringString(params, "data")
	if result == nil || result["key1"] != "val1" || result["key2"] != "val2" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestExtractMapStringString_MapStringString(t *testing.T) {
	params := map[string]any{
		"data": map[string]string{"key1": "val1"},
	}
	result := extractMapStringString(params, "data")
	if result == nil || result["key1"] != "val1" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestExtractMapStringString_Missing(t *testing.T) {
	result := extractMapStringString(map[string]any{}, "data")
	if result != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestExtractMapStringString_WrongType(t *testing.T) {
	params := map[string]any{"data": "not a map"}
	result := extractMapStringString(params, "data")
	if result != nil {
		t.Fatal("expected nil for wrong type")
	}
}

func TestExtractMapStringString_NonStringValues(t *testing.T) {
	// When map[string]any contains non-string values, they should be skipped.
	params := map[string]any{
		"data": map[string]any{"key1": "val1", "key2": 42},
	}
	result := extractMapStringString(params, "data")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["key1"] != "val1" {
		t.Errorf("expected key1=val1, got %q", result["key1"])
	}
	if _, exists := result["key2"]; exists {
		t.Error("non-string value should not be in result")
	}
}

func TestExtractMapAny_Present(t *testing.T) {
	inner := map[string]any{"a": 1}
	params := map[string]any{"data": inner}
	result := extractMapAny(params, "data")
	if result == nil || result["a"] != 1 {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestExtractMapAny_Missing(t *testing.T) {
	result := extractMapAny(map[string]any{}, "data")
	if result != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestExtractMapAny_WrongType(t *testing.T) {
	params := map[string]any{"data": "not a map"}
	result := extractMapAny(params, "data")
	if result != nil {
		t.Fatal("expected nil for wrong type")
	}
}
