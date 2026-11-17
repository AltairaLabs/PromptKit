package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The model is given a JSON tool result as text, with every value exact.
func TestFormatResultText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"string is unquoted", `"shipped"`, "shipped"},
		{"object is indented, keys sorted", `{"b":1,"a":"x"}`, "{\n  \"a\": \"x\",\n  \"b\": 1\n}"},
		{"array is indented", `["a","b"]`, "[\n  \"a\",\n  \"b\"\n]"},
		{"small integer", `42`, "42"},
		{"round integer keeps its digits", `10000000`, "10000000"},
		{"integer beyond float precision stays exact", `9007199254740993`, "9007199254740993"},
		{"large integer inside an object stays exact",
			`{"id":9007199254740993}`, "{\n  \"id\": 9007199254740993\n}"},
		{"fraction", `1.5`, "1.5"},
		{"boolean", `true`, "true"},
		{"null renders as null", `null`, "null"},
		{"null inside an object", `{"a":null}`, "{\n  \"a\": null\n}"},
		{"text that is not JSON passes through", `plain text`, "plain text"},
		{"JSON followed by text passes through", `{"a":1} trailing`, `{"a":1} trailing`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FormatResultText(json.RawMessage(tt.raw)))
		})
	}
}

// A result over the limit is cut at it and says so; one within it, or with no
// limit, is untouched.
func TestTruncateResultText(t *testing.T) {
	assert.Equal(t, "abcdef", TruncateResultText("abcdef", 0))
	assert.Equal(t, "abcdef", TruncateResultText("abcdef", 6))
	assert.Equal(t, "abc\n... [truncated, 6 bytes exceeded limit of 3 bytes]", TruncateResultText("abcdef", 3))
}
