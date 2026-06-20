package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// Every tool record with a non-omitempty json.RawMessage must marshal when that
// field is empty (LLM tool call with no args, tool with no schema, etc.) instead
// of failing with "unexpected end of JSON input".
func TestToolRecordMarshal_EmptyRawFields(t *testing.T) {
	check := func(name string, v any, wants ...string) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Errorf("%s: empty raw field must marshal, got %v", name, err)
			return
		}
		for _, w := range wants {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s: want %q in %s", name, w, b)
			}
		}
	}
	check("ToolDescriptor", ToolDescriptor{}, `"input_schema":{}`, `"output_schema":{}`)
	check("ToolCall", ToolCall{}, `"args":{}`)
	check("ToolResult", ToolResult{}, `"result":{}`)
	check("PendingToolInfo", PendingToolInfo{}, `"args":{}`)
}
