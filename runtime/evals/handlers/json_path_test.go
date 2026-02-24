package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestJSONPathHandler_Type(t *testing.T) {
	h := &JSONPathHandler{}
	if h.Type() != "json_path" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestJSONPathHandler_Expected(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"name": "Alice", "age": 30}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "name",
		"expected":   "Alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestJSONPathHandler_ExpectedMismatch(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"name": "Bob"}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "name",
		"expected":   "Alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for mismatch")
	}
}

func TestJSONPathHandler_NumericMin(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"score": 85}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "score",
		"min":        50.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestJSONPathHandler_NumericMinFail(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"score": 30}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "score",
		"min":        50.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail below min")
	}
}

func TestJSONPathHandler_ArrayContains(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"items": ["a", "b", "c"]}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "items",
		"contains":   []any{"a", "c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestJSONPathHandler_MinResults(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"items": [1, 2, 3]}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression":  "items",
		"min_results": 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail below min_results")
	}
}

func TestJSONPathHandler_InvalidJSON(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "not json",
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for invalid JSON")
	}
}

func TestJSONPathHandler_NoExpression(t *testing.T) {
	h := &JSONPathHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no expression")
	}
}

func TestJSONPathHandler_AllowWrapped(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Here is the JSON:\n```json\n{\"name\": \"Alice\"}\n```\nDone.",
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression":    "name",
		"expected":      "Alice",
		"allow_wrapped": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with allow_wrapped: %s", result.Explanation)
	}
}

func TestJSONPathHandler_ExtractJSON_Object(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `Here is the data: {"name": "Bob", "age": 25} and some trailing text.`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression":   "name",
		"expected":     "Bob",
		"extract_json": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with extract_json: %s", result.Explanation)
	}
}

func TestJSONPathHandler_ExtractJSON_Array(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `Results: [1, 2, 3] end`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression":   "[1]",
		"expected":     float64(2),
		"extract_json": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass extracting array: %s", result.Explanation)
	}
}

func TestJSONPathHandler_ExtractJSON_NoJSON(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `No JSON content here at all`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression":   "name",
		"extract_json": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for no extractable JSON")
	}
}

func TestJSONPathHandler_JMESPathError(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"name": "Alice"}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "invalid..expression",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for invalid JMESPath")
	}
}

func TestJSONPathHandler_JMESPathExpression(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"name": "Alice"}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"jmespath_expression": "name",
		"expected":            "Alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with jmespath_expression: %s", result.Explanation)
	}
}

func TestJSONPathHandler_NumericMax(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"score": 95}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "score",
		"max":        80.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail above max")
	}
}

func TestJSONPathHandler_NumericRangeNonNumeric(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"value": "not a number"}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "value",
		"min":        1.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for non-numeric range check")
	}
}

func TestJSONPathHandler_MaxResults(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"items": [1, 2, 3, 4, 5]}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression":  "items",
		"max_results": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail above max_results")
	}
}

func TestJSONPathHandler_ArrayCountNotArray(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"count": 5}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression":  "count",
		"min_results": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for non-array count check")
	}
}

func TestJSONPathHandler_ContainsMissing(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"items": ["a", "b"]}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "items",
		"contains":   []any{"a", "z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing contains item")
	}
}

func TestJSONPathHandler_ContainsNotArray(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"value": "hello"}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "value",
		"contains":   []any{"hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for non-array contains check")
	}
}

func TestJSONPathHandler_ContainsNotArrayParam(t *testing.T) {
	h := &JSONPathHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"items": [1, 2]}`,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expression": "items",
		"contains":   "not an array",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for non-array contains param")
	}
}

// --- extractBalancedJSON ---

func TestExtractBalancedJSON_Object(t *testing.T) {
	result := extractBalancedJSON(`{"key": "value"} trailing`, '{', '}')
	if result != `{"key": "value"}` {
		t.Errorf("got %q", result)
	}
}

func TestExtractBalancedJSON_Nested(t *testing.T) {
	result := extractBalancedJSON(`{"a": {"b": 1}} rest`, '{', '}')
	if result != `{"a": {"b": 1}}` {
		t.Errorf("got %q", result)
	}
}

func TestExtractBalancedJSON_Array(t *testing.T) {
	result := extractBalancedJSON(`[1, [2, 3]] rest`, '[', ']')
	if result != `[1, [2, 3]]` {
		t.Errorf("got %q", result)
	}
}

func TestExtractBalancedJSON_Empty(t *testing.T) {
	result := extractBalancedJSON("", '{', '}')
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestExtractBalancedJSON_WrongStartChar(t *testing.T) {
	result := extractBalancedJSON("abc", '{', '}')
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestExtractBalancedJSON_UnbalancedBraces(t *testing.T) {
	result := extractBalancedJSON(`{"key": "value"`, '{', '}')
	if result != "" {
		t.Errorf("expected empty for unbalanced, got %q", result)
	}
}

func TestExtractBalancedJSON_StringsWithBraces(t *testing.T) {
	result := extractBalancedJSON(`{"key": "val{ue}"}`, '{', '}')
	if result != `{"key": "val{ue}"}` {
		t.Errorf("got %q", result)
	}
}

func TestExtractBalancedJSON_EscapedQuotes(t *testing.T) {
	result := extractBalancedJSON(`{"key": "val\"ue"}`, '{', '}')
	if result != `{"key": "val\"ue"}` {
		t.Errorf("got %q", result)
	}
}

// --- advanceJSONParser ---

func TestAdvanceJSONParser_Escaped(t *testing.T) {
	depth, inStr, esc := advanceJSONParser('"', '{', '}', 1, true, true)
	if depth != 1 || !inStr || esc {
		t.Errorf("escaped char should be skipped: depth=%d inStr=%v esc=%v", depth, inStr, esc)
	}
}

func TestAdvanceJSONParser_Backslash(t *testing.T) {
	depth, inStr, esc := advanceJSONParser('\\', '{', '}', 1, true, false)
	if depth != 1 || !inStr || !esc {
		t.Errorf("backslash should set escaped: depth=%d inStr=%v esc=%v", depth, inStr, esc)
	}
}

func TestAdvanceJSONParser_Quote(t *testing.T) {
	depth, inStr, esc := advanceJSONParser('"', '{', '}', 1, false, false)
	if depth != 1 || !inStr || esc {
		t.Errorf("quote should toggle inString: depth=%d inStr=%v esc=%v", depth, inStr, esc)
	}
}

func TestAdvanceJSONParser_Open(t *testing.T) {
	depth, inStr, esc := advanceJSONParser('{', '{', '}', 1, false, false)
	if depth != 2 || inStr || esc {
		t.Errorf("open should increment depth: depth=%d inStr=%v esc=%v", depth, inStr, esc)
	}
}

func TestAdvanceJSONParser_Close(t *testing.T) {
	depth, inStr, esc := advanceJSONParser('}', '{', '}', 2, false, false)
	if depth != 1 || inStr || esc {
		t.Errorf("close should decrement depth: depth=%d inStr=%v esc=%v", depth, inStr, esc)
	}
}

func TestAdvanceJSONParser_DefaultChar(t *testing.T) {
	depth, inStr, esc := advanceJSONParser('a', '{', '}', 1, false, false)
	if depth != 1 || inStr || esc {
		t.Errorf("default should not change state: depth=%d inStr=%v esc=%v", depth, inStr, esc)
	}
}

// --- jsonCompareValues ---

func TestJSONCompareValues_BothNil(t *testing.T) {
	if !jsonCompareValues(nil, nil) {
		t.Error("nil == nil should be true")
	}
}

func TestJSONCompareValues_OneNil(t *testing.T) {
	if jsonCompareValues(nil, "a") {
		t.Error("nil != non-nil should be false")
	}
	if jsonCompareValues("a", nil) {
		t.Error("non-nil != nil should be false")
	}
}

func TestJSONCompareValues_Strings(t *testing.T) {
	if !jsonCompareValues("hello", "hello") {
		t.Error("same strings should be equal")
	}
	if jsonCompareValues("hello", "world") {
		t.Error("different strings should not be equal")
	}
}

func TestJSONCompareValues_NumericCrossType(t *testing.T) {
	if !jsonCompareValues(float64(42), 42) {
		t.Error("float64(42) should equal int(42)")
	}
	if !jsonCompareValues(int64(10), float64(10)) {
		t.Error("int64(10) should equal float64(10)")
	}
}

// --- jsonCompareArrays ---

func TestJSONCompareArrays_Equal(t *testing.T) {
	handled, equal := jsonCompareArrays([]any{1, "two"}, []any{1, "two"})
	if !handled || !equal {
		t.Errorf("expected handled=true equal=true, got %v %v", handled, equal)
	}
}

func TestJSONCompareArrays_DifferentLength(t *testing.T) {
	handled, equal := jsonCompareArrays([]any{1}, []any{1, 2})
	if !handled || equal {
		t.Errorf("expected handled=true equal=false, got %v %v", handled, equal)
	}
}

func TestJSONCompareArrays_DifferentValues(t *testing.T) {
	handled, equal := jsonCompareArrays([]any{1, 2}, []any{1, 3})
	if !handled || equal {
		t.Errorf("expected handled=true equal=false, got %v %v", handled, equal)
	}
}

func TestJSONCompareArrays_NotArrays(t *testing.T) {
	handled, _ := jsonCompareArrays("a", []any{1})
	if handled {
		t.Error("expected handled=false for non-array")
	}
}

// --- jsonCompareMaps ---

func TestJSONCompareMaps_Equal(t *testing.T) {
	a := map[string]any{"k": "v"}
	b := map[string]any{"k": "v"}
	handled, equal := jsonCompareMaps(a, b)
	if !handled || !equal {
		t.Errorf("expected handled=true equal=true, got %v %v", handled, equal)
	}
}

func TestJSONCompareMaps_DifferentLength(t *testing.T) {
	a := map[string]any{"k": "v"}
	b := map[string]any{"k": "v", "k2": "v2"}
	handled, equal := jsonCompareMaps(a, b)
	if !handled || equal {
		t.Errorf("expected handled=true equal=false, got %v %v", handled, equal)
	}
}

func TestJSONCompareMaps_DifferentValues(t *testing.T) {
	a := map[string]any{"k": "v1"}
	b := map[string]any{"k": "v2"}
	handled, equal := jsonCompareMaps(a, b)
	if !handled || equal {
		t.Errorf("expected handled=true equal=false, got %v %v", handled, equal)
	}
}

func TestJSONCompareMaps_NotMaps(t *testing.T) {
	handled, _ := jsonCompareMaps("a", map[string]any{})
	if handled {
		t.Error("expected handled=false for non-map")
	}
}

// --- jsonToFloat64 ---

func TestJSONToFloat64_AllTypes(t *testing.T) {
	tests := []struct {
		input    any
		expected float64
		ok       bool
	}{
		{float64(1.5), 1.5, true},
		{float32(2.5), 2.5, true},
		{int(3), 3.0, true},
		{int64(4), 4.0, true},
		{"not a number", 0, false},
		{true, 0, false},
	}
	for _, tt := range tests {
		got, ok := jsonToFloat64(tt.input)
		if ok != tt.ok {
			t.Errorf("jsonToFloat64(%v): ok=%v, want %v", tt.input, ok, tt.ok)
		}
		if ok && got != tt.expected {
			t.Errorf("jsonToFloat64(%v) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// --- extractJSONFromContent ---

func TestExtractJSONFromContent_WrappedCodeBlock(t *testing.T) {
	content := "Some text\n```json\n{\"key\": \"value\"}\n```\nMore text"
	result := extractJSONFromContent(content, true, false)
	if result != `{"key": "value"}` {
		t.Errorf("got %q", result)
	}
}

func TestExtractJSONFromContent_ExtractObject(t *testing.T) {
	content := `Leading text {"a": 1} trailing`
	result := extractJSONFromContent(content, false, true)
	if result != `{"a": 1}` {
		t.Errorf("got %q", result)
	}
}

func TestExtractJSONFromContent_ExtractArray(t *testing.T) {
	content := `Leading text [1, 2, 3] trailing`
	result := extractJSONFromContent(content, false, true)
	if result != `[1, 2, 3]` {
		t.Errorf("got %q", result)
	}
}

func TestExtractJSONFromContent_NoMatch(t *testing.T) {
	result := extractJSONFromContent("no json here", false, false)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestExtractJSONFromContent_BothFlags(t *testing.T) {
	content := "Here is the JSON:\n```json\n{\"name\": \"Alice\"}\n```\nDone."
	result := extractJSONFromContent(content, true, true)
	if result != `{"name": "Alice"}` {
		t.Errorf("got %q", result)
	}
}

func TestExtractJSONFromContent_ExtractObjectNoArray(t *testing.T) {
	// Test where content has only an object, no array bracket.
	content := `Text before {"x": 1} text after`
	result := extractJSONFromContent(content, false, true)
	if result != `{"x": 1}` {
		t.Errorf("got %q", result)
	}
}

func TestExtractJSONFromContent_UnbalancedObjectFallsToArray(t *testing.T) {
	// Object starts but never closes; array is present after.
	content := `{unclosed but also [1, 2]`
	result := extractJSONFromContent(content, false, true)
	if result != `[1, 2]` {
		t.Errorf("expected array extraction, got %q", result)
	}
}
