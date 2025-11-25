package assertions

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jmespath/go-jmespath"
	"github.com/xeipuuv/gojsonschema"

	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// IsValidJSONValidator validates that content is parseable JSON
type IsValidJSONValidator struct {
	allowWrapped bool
	extractJSON  bool
}

// NewIsValidJSONValidator creates a new is_valid_json validator
func NewIsValidJSONValidator(params map[string]interface{}) runtimeValidators.Validator {
	allowWrapped, _ := params["allow_wrapped"].(bool) // NOSONAR: Type assertion failure returns false (desired default)
	extractJSON, _ := params["extract_json"].(bool)   // NOSONAR: Type assertion failure returns false (desired default)

	return &IsValidJSONValidator{
		allowWrapped: allowWrapped,
		extractJSON:  extractJSON,
	}
}

// Validate checks if content is valid JSON
func (v *IsValidJSONValidator) Validate(
	content string,
	params map[string]interface{},
) runtimeValidators.ValidationResult {
	// Extract JSON if needed
	jsonContent := content
	if v.allowWrapped || v.extractJSON {
		extracted := v.extractJSONFromContent(content)
		if extracted != "" {
			jsonContent = extracted
		}
	}

	// Try to parse JSON
	var data interface{}
	err := json.Unmarshal([]byte(jsonContent), &data)

	if err != nil {
		const maxContentLen = 200
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error":   err.Error(),
				"content": truncateString(jsonContent, maxContentLen),
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed:  true,
		Details: "Valid JSON",
	}
}

// extractJSONFromContent attempts to extract JSON from wrapped or mixed content
func (v *IsValidJSONValidator) extractJSONFromContent(content string) string {
	// Try to extract from markdown code blocks
	if v.allowWrapped {
		codeBlockRegex := regexp.MustCompile("```(?:json)?\\s*\\n([\\s\\S]*?)\\n```")
		matches := codeBlockRegex.FindStringSubmatch(content)
		if len(matches) > 1 {
			return strings.TrimSpace(matches[1])
		}
	}

	// Try to extract JSON object or array from mixed text
	if v.extractJSON {
		// Look for JSON objects
		if idx := strings.Index(content, "{"); idx >= 0 {
			extracted := v.extractBalancedJSON(content[idx:], '{', '}')
			if extracted != "" {
				return extracted
			}
		}

		// Look for JSON arrays
		if idx := strings.Index(content, "["); idx >= 0 {
			extracted := v.extractBalancedJSON(content[idx:], '[', ']')
			if extracted != "" {
				return extracted
			}
		}
	}

	return ""
}

// extractBalancedJSON extracts a balanced JSON structure from content
func (v *IsValidJSONValidator) extractBalancedJSON(content string, openChar, closeChar rune) string {
	if content == "" || rune(content[0]) != openChar {
		return ""
	}

	state := &balanceState{depth: 0, inString: false, escaped: false}

	for i, ch := range content {
		if state.processChar(ch, openChar, closeChar) {
			return content[:i+1]
		}
	}

	return ""
}

type balanceState struct {
	depth    int
	inString bool
	escaped  bool
}

func (s *balanceState) processChar(ch, openChar, closeChar rune) bool {
	if s.escaped {
		s.escaped = false
		return false
	}

	switch ch {
	case '\\':
		s.escaped = true
	case '"':
		s.inString = !s.inString
	case openChar:
		if !s.inString {
			s.depth++
		}
	case closeChar:
		if !s.inString {
			s.depth--
			return s.depth == 0
		}
	}
	return false
}

// JSONSchemaValidator validates JSON against a JSON Schema
type JSONSchemaValidator struct {
	schema       map[string]interface{}
	schemaFile   string
	allowWrapped bool
	extractJSON  bool
}

// NewJSONSchemaValidator creates a new json_schema validator
func NewJSONSchemaValidator(params map[string]interface{}) runtimeValidators.Validator {
	schema, _ := params["schema"].(map[string]interface{}) // NOSONAR: Type assertion failure returns nil (desired default)
	schemaFile, _ := params["schema_file"].(string)        // NOSONAR: Type assertion failure returns empty string (desired default)
	allowWrapped, _ := params["allow_wrapped"].(bool)      // NOSONAR: Type assertion failure returns false (desired default)
	extractJSON, _ := params["extract_json"].(bool)        // NOSONAR: Type assertion failure returns false (desired default)

	return &JSONSchemaValidator{
		schema:       schema,
		schemaFile:   schemaFile,
		allowWrapped: allowWrapped,
		extractJSON:  extractJSON,
	}
}

// Validate checks if JSON content matches the schema
func (v *JSONSchemaValidator) Validate(
	content string,
	params map[string]interface{},
) runtimeValidators.ValidationResult {
	// First validate it's valid JSON
	jsonValidator := &IsValidJSONValidator{
		allowWrapped: v.allowWrapped,
		extractJSON:  v.extractJSON,
	}

	jsonResult := jsonValidator.Validate(content, params)
	if !jsonResult.Passed {
		return jsonResult
	}

	// Extract JSON if needed
	jsonContent := content
	if v.allowWrapped || v.extractJSON {
		extracted := jsonValidator.extractJSONFromContent(content)
		if extracted != "" {
			jsonContent = extracted
		}
	}

	// Load schema
	var schemaLoader gojsonschema.JSONLoader
	if v.schemaFile != "" {
		schemaLoader = gojsonschema.NewReferenceLoader("file://" + v.schemaFile)
	} else if v.schema != nil {
		schemaLoader = gojsonschema.NewGoLoader(v.schema)
	} else {
		return runtimeValidators.ValidationResult{
			Passed:  false,
			Details: "No schema provided (use 'schema' or 'schema_file' parameter)",
		}
	}

	// Load document
	documentLoader := gojsonschema.NewStringLoader(jsonContent)

	// Validate
	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("Schema validation error: %v", err),
			},
		}
	}

	if !result.Valid() {
		errors := make([]string, 0, len(result.Errors()))
		for _, err := range result.Errors() {
			errors = append(errors, err.String())
		}

		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"errors": errors,
				"count":  len(errors),
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed:  true,
		Details: "JSON matches schema",
	}
}

// JSONPathValidator validates JSON using JMESPath expressions
type JSONPathValidator struct {
	jmespathExpression string
	expected           interface{}
	contains           interface{}
	minResults         int
	maxResults         int
	min                *float64
	max                *float64
	allowWrapped       bool
	extractJSON        bool
}

// NewJSONPathValidator creates a new json_path validator
func NewJSONPathValidator(params map[string]interface{}) runtimeValidators.Validator {
	// Support both jmespath_expression (new) and expression (deprecated) for backward compatibility
	jmespathExpression, _ := params["jmespath_expression"].(string) // NOSONAR: Type assertion failure returns empty string, validated below
	if jmespathExpression == "" {
		jmespathExpression, _ = params["expression"].(string) // NOSONAR: Backward compatibility fallback
	}
	expected := params["expected"]
	contains := params["contains"]
	minResults, _ := params["min_results"].(int)      // NOSONAR: Type assertion failure returns 0 (desired default)
	maxResults, _ := params["max_results"].(int)      // NOSONAR: Type assertion failure returns 0 (desired default)
	allowWrapped, _ := params["allow_wrapped"].(bool) // NOSONAR: Type assertion failure returns false (desired default)
	extractJSON, _ := params["extract_json"].(bool)   // NOSONAR: Type assertion failure returns false (desired default)

	var minVal, maxVal *float64
	if val, ok := params["min"].(float64); ok {
		minVal = &val
	}
	if val, ok := params["max"].(float64); ok {
		maxVal = &val
	}

	return &JSONPathValidator{
		jmespathExpression: jmespathExpression,
		expected:           expected,
		contains:           contains,
		minResults:         minResults,
		maxResults:         maxResults,
		min:                minVal,
		max:                maxVal,
		allowWrapped:       allowWrapped,
		extractJSON:        extractJSON,
	}
}

// Validate executes JMESPath expression and validates result
func (v *JSONPathValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	// First validate it's valid JSON
	jsonValidator := &IsValidJSONValidator{
		allowWrapped: v.allowWrapped,
		extractJSON:  v.extractJSON,
	}

	jsonResult := jsonValidator.Validate(content, params)
	if !jsonResult.Passed {
		return jsonResult
	}

	// Extract JSON if needed
	jsonContent := content
	if v.allowWrapped || v.extractJSON {
		extracted := jsonValidator.extractJSONFromContent(content)
		if extracted != "" {
			jsonContent = extracted
		}
	}

	// Parse JSON
	var data interface{}
	if err := json.Unmarshal([]byte(jsonContent), &data); err != nil {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("Failed to parse JSON: %v", err),
			},
		}
	}

	// Execute JMESPath expression
	result, err := jmespath.Search(v.jmespathExpression, data)
	if err != nil {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error":               fmt.Sprintf("JMESPath error: %v", err),
				"jmespath_expression": v.jmespathExpression,
			},
		}
	}

	// Validate result based on parameters
	return v.validateResult(result)
}

// validateResult checks if the JMESPath result matches expectations
func (v *JSONPathValidator) validateResult(result interface{}) runtimeValidators.ValidationResult {
	// Check expected value
	if v.expected != nil {
		if !compareValues(result, v.expected) {
			return runtimeValidators.ValidationResult{
				Passed: false,
				Details: map[string]interface{}{
					"expected": v.expected,
					"actual":   result,
					"message":  "Result does not match expected value",
				},
			}
		}
	}

	// Check contains for arrays
	if v.contains != nil {
		containsResult := v.checkContains(result)
		if !containsResult.Passed {
			return containsResult
		}
	}

	// Check numeric constraints
	if v.min != nil || v.max != nil {
		numResult := v.checkNumericConstraints(result)
		if !numResult.Passed {
			return numResult
		}
	}

	// Check array result count
	if v.minResults > 0 || v.maxResults > 0 {
		countResult := v.checkArrayCount(result)
		if !countResult.Passed {
			return countResult
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: true,
		Details: map[string]interface{}{
			"result": result,
		},
	}
}

// checkContains verifies that result contains expected items
func (v *JSONPathValidator) checkContains(result interface{}) runtimeValidators.ValidationResult {
	resultArray, ok := result.([]interface{})
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message": "Result is not an array, cannot check contains",
				"result":  result,
			},
		}
	}

	containsArray, ok := v.contains.([]interface{})
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message": "Contains parameter must be an array",
			},
		}
	}

	for _, expected := range containsArray {
		found := false
		for _, item := range resultArray {
			if compareValues(item, expected) {
				found = true
				break
			}
		}
		if !found {
			return runtimeValidators.ValidationResult{
				Passed: false,
				Details: map[string]interface{}{
					"message": "Expected item not found in result",
					"missing": expected,
					"result":  result,
				},
			}
		}
	}

	return runtimeValidators.ValidationResult{Passed: true}
}

// checkNumericConstraints validates numeric min/max constraints
func (v *JSONPathValidator) checkNumericConstraints(result interface{}) runtimeValidators.ValidationResult {
	numValue, ok := toFloat64(result)
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message": "Result is not a number",
				"result":  result,
			},
		}
	}

	if v.min != nil && numValue < *v.min {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message": fmt.Sprintf("Value %.2f is below minimum %.2f", numValue, *v.min),
				"actual":  numValue,
				"min":     *v.min,
			},
		}
	}

	if v.max != nil && numValue > *v.max {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message": fmt.Sprintf("Value %.2f exceeds maximum %.2f", numValue, *v.max),
				"actual":  numValue,
				"max":     *v.max,
			},
		}
	}

	return runtimeValidators.ValidationResult{Passed: true}
}

// checkArrayCount validates array result count constraints
func (v *JSONPathValidator) checkArrayCount(result interface{}) runtimeValidators.ValidationResult {
	resultArray, ok := result.([]interface{})
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message": "Result is not an array, cannot check count",
				"result":  result,
			},
		}
	}

	count := len(resultArray)

	if v.minResults > 0 && count < v.minResults {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message":     fmt.Sprintf("Array has %d items, minimum is %d", count, v.minResults),
				"actual":      count,
				"min_results": v.minResults,
			},
		}
	}

	if v.maxResults > 0 && count > v.maxResults {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message":     fmt.Sprintf("Array has %d items, maximum is %d", count, v.maxResults),
				"actual":      count,
				"max_results": v.maxResults,
			},
		}
	}

	return runtimeValidators.ValidationResult{Passed: true}
}

// compareValues compares two values for equality, handling different types
func compareValues(a, b interface{}) bool {
	if !areNilsEqual(a, b) {
		return false
	}

	if areNumbersEqual(a, b) {
		return true
	}

	if areArraysEqual(a, b) {
		return true
	}

	if areMapsEqual(a, b) {
		return true
	}

	// Direct comparison
	return a == b
}

// areNilsEqual checks if both values are nil or if one is nil
func areNilsEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	return a != nil && b != nil
}

// areNumbersEqual checks if both values are numbers and equal
func areNumbersEqual(a, b interface{}) bool {
	aNum, aIsNum := toFloat64(a)
	bNum, bIsNum := toFloat64(b)
	return aIsNum && bIsNum && aNum == bNum
}

// areArraysEqual checks if both values are arrays and equal
func areArraysEqual(a, b interface{}) bool {
	aArr, aIsArr := a.([]interface{})
	bArr, bIsArr := b.([]interface{})
	if !aIsArr || !bIsArr {
		return false
	}
	if len(aArr) != len(bArr) {
		return false
	}
	for i := range aArr {
		if !compareValues(aArr[i], bArr[i]) {
			return false
		}
	}
	return true
}

// areMapsEqual checks if both values are maps and equal
func areMapsEqual(a, b interface{}) bool {
	aMap, aIsMap := a.(map[string]interface{})
	bMap, bIsMap := b.(map[string]interface{})
	if !aIsMap || !bIsMap {
		return false
	}
	if len(aMap) != len(bMap) {
		return false
	}
	for k, v := range aMap {
		if !compareValues(v, bMap[k]) {
			return false
		}
	}
	return true
}

// toFloat64 converts various numeric types to float64
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint8:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint64:
		return float64(val), true
	default:
		return 0, false
	}
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
