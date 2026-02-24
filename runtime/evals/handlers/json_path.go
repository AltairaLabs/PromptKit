package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jmespath/go-jmespath"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// JSONPathHandler validates assistant output as JSON using JMESPath expressions.
// Params:
//   - expression string (JMESPath expression)
//   - expected any (optional: exact match)
//   - contains []any (optional: array contains check)
//   - min_results int, max_results int (optional: array length bounds)
//   - min float64, max float64 (optional: numeric range)
//   - allow_wrapped bool (optional: extract JSON from code blocks)
//   - extract_json bool (optional: extract JSON from mixed text)
type JSONPathHandler struct{}

// Type returns the eval type identifier.
func (h *JSONPathHandler) Type() string { return "json_path" }

// Eval executes a JMESPath expression on the assistant output and validates the result.
func (h *JSONPathHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	expression, _ := params["expression"].(string)
	if expression == "" {
		expression, _ = params["jmespath_expression"].(string)
	}
	if expression == "" {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "no expression specified",
		}, nil
	}

	allowWrapped := extractBool(params, "allow_wrapped")
	extractJSON := extractBool(params, "extract_json")

	jsonContent := evalCtx.CurrentOutput
	if allowWrapped || extractJSON {
		if extracted := extractJSONFromContent(jsonContent, allowWrapped, extractJSON); extracted != "" {
			jsonContent = extracted
		}
	}

	var data any
	if err := json.Unmarshal([]byte(jsonContent), &data); err != nil {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: fmt.Sprintf("invalid JSON: %v", err),
		}, nil
	}

	result, err := jmespath.Search(expression, data)
	if err != nil {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: fmt.Sprintf("JMESPath error: %v", err),
			Details:     map[string]any{"expression": expression},
		}, nil
	}

	return h.validateResult(result, params)
}

func (h *JSONPathHandler) validateResult(result any, params map[string]any) (*evals.EvalResult, error) {
	if expected, ok := params["expected"]; ok {
		if !jsonCompareValues(result, expected) {
			return h.fail("result does not match expected value",
				map[string]any{"expected": expected, "actual": result}), nil
		}
	}

	if contains, ok := params["contains"]; ok {
		if err := h.checkContains(result, contains); err != "" {
			return h.fail(err, map[string]any{"result": result}), nil
		}
	}

	if r := h.checkNumericRange(result, params); r != nil {
		return r, nil
	}

	if r := h.checkArrayCount(result, params); r != nil {
		return r, nil
	}

	return &evals.EvalResult{
		Type: h.Type(), Passed: true,
		Explanation: "JSON path validation passed",
		Details:     map[string]any{"result": result},
	}, nil
}

func (h *JSONPathHandler) fail(explanation string, details map[string]any) *evals.EvalResult {
	return &evals.EvalResult{Type: h.Type(), Passed: false, Explanation: explanation, Details: details}
}

func (h *JSONPathHandler) checkNumericRange(result any, params map[string]any) *evals.EvalResult {
	minVal := extractFloat64Ptr(params, "min")
	maxVal := extractFloat64Ptr(params, "max")
	if minVal == nil && maxVal == nil {
		return nil
	}

	numValue, ok := jsonToFloat64(result)
	if !ok {
		return h.fail("result is not a number for range check", nil)
	}
	if minVal != nil && numValue < *minVal {
		return h.fail(fmt.Sprintf("value %.2f is below minimum %.2f", numValue, *minVal), nil)
	}
	if maxVal != nil && numValue > *maxVal {
		return h.fail(fmt.Sprintf("value %.2f exceeds maximum %.2f", numValue, *maxVal), nil)
	}
	return nil
}

func (h *JSONPathHandler) checkArrayCount(result any, params map[string]any) *evals.EvalResult {
	minResults := extractInt(params, "min_results", 0)
	maxResults := extractInt(params, "max_results", 0)
	if minResults == 0 && maxResults == 0 {
		return nil
	}

	arr, ok := result.([]any)
	if !ok {
		return h.fail("result is not an array for count check", nil)
	}
	count := len(arr)
	if minResults > 0 && count < minResults {
		return h.fail(fmt.Sprintf("array has %d items, minimum is %d", count, minResults), nil)
	}
	if maxResults > 0 && count > maxResults {
		return h.fail(fmt.Sprintf("array has %d items, maximum is %d", count, maxResults), nil)
	}
	return nil
}

func (h *JSONPathHandler) checkContains(result, contains any) string {
	resultArray, ok := result.([]any)
	if !ok {
		return "result is not an array, cannot check contains"
	}
	containsArray, ok := contains.([]any)
	if !ok {
		return "contains parameter must be an array"
	}
	for _, expected := range containsArray {
		found := false
		for _, item := range resultArray {
			if jsonCompareValues(item, expected) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("expected item %v not found in result", expected)
		}
	}
	return ""
}

// extractJSONFromContent extracts JSON from mixed/wrapped content.
func extractJSONFromContent(content string, allowWrapped, extractJSON bool) string {
	if allowWrapped {
		re := regexp.MustCompile("```(?:json)?\\s*\\n([\\s\\S]*?)\\n```")
		matches := re.FindStringSubmatch(content)
		if len(matches) > 1 {
			return strings.TrimSpace(matches[1])
		}
	}

	if extractJSON {
		if idx := strings.Index(content, "{"); idx >= 0 {
			if extracted := extractBalancedJSON(content[idx:], '{', '}'); extracted != "" {
				return extracted
			}
		}
		if idx := strings.Index(content, "["); idx >= 0 {
			if extracted := extractBalancedJSON(content[idx:], '[', ']'); extracted != "" {
				return extracted
			}
		}
	}

	return ""
}

func extractBalancedJSON(content string, openChar, closeChar byte) string {
	if content == "" || content[0] != openChar {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := range len(content) {
		ch := content[i]
		depth, inString, escaped = advanceJSONParser(ch, openChar, closeChar, depth, inString, escaped)
		if depth == 0 && !inString && i > 0 {
			return content[:i+1]
		}
	}
	return ""
}

// advanceJSONParser advances the JSON parser state by one character.
func advanceJSONParser(
	ch, openChar, closeChar byte, depth int, inString, escaped bool,
) (newDepth int, newInString, newEscaped bool) {
	if escaped {
		return depth, inString, false
	}
	switch {
	case ch == '\\' && inString:
		return depth, inString, true
	case ch == '"':
		return depth, !inString, false
	case ch == openChar && !inString:
		return depth + 1, inString, false
	case ch == closeChar && !inString:
		return depth - 1, inString, false
	default:
		return depth, inString, false
	}
}

func jsonCompareValues(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if ok, result := jsonCompareNumeric(a, b); ok {
		return result
	}
	if ok, result := jsonCompareArrays(a, b); ok {
		return result
	}
	if ok, result := jsonCompareMaps(a, b); ok {
		return result
	}
	return a == b
}

func jsonCompareNumeric(a, b any) (handled, equal bool) {
	aNum, aOK := jsonToFloat64(a)
	bNum, bOK := jsonToFloat64(b)
	if aOK && bOK {
		return true, aNum == bNum
	}
	return false, false
}

func jsonCompareArrays(a, b any) (handled, equal bool) {
	aArr, aIsArr := a.([]any)
	bArr, bIsArr := b.([]any)
	if !aIsArr || !bIsArr {
		return false, false
	}
	if len(aArr) != len(bArr) {
		return true, false
	}
	for i := range aArr {
		if !jsonCompareValues(aArr[i], bArr[i]) {
			return true, false
		}
	}
	return true, true
}

func jsonCompareMaps(a, b any) (handled, equal bool) {
	aMap, aIsMap := a.(map[string]any)
	bMap, bIsMap := b.(map[string]any)
	if !aIsMap || !bIsMap {
		return false, false
	}
	if len(aMap) != len(bMap) {
		return true, false
	}
	for k, v := range aMap {
		if !jsonCompareValues(v, bMap[k]) {
			return true, false
		}
	}
	return true, true
}

func jsonToFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}
