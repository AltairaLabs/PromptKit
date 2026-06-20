package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeRawMessage(t *testing.T) {
	cases := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{"nil", nil, "{}"},
		{"empty", json.RawMessage{}, "{}"},
		{"null stays null (valid JSON)", json.RawMessage(`null`), "null"},
		{"object passthrough", json.RawMessage(`{"a":1}`), `{"a":1}`},
		// max_tokens cut off mid-tool-call: non-empty but invalid JSON.
		{"truncated object", json.RawMessage(`{"command":"cat fi`), "{}"},
		{"truncated string", json.RawMessage(`"foo`), "{}"},
		{"bare garbage", json.RawMessage(`{`), "{}"},
	}
	for _, c := range cases {
		if got := string(NormalizeRawMessage(c.in)); got != c.want {
			t.Errorf("%s: NormalizeRawMessage(%q) = %q, want %q", c.name, string(c.in), got, c.want)
		}
	}
}

func TestMessageToolCall_EmptyArgsMarshals(t *testing.T) {
	b, err := json.Marshal(MessageToolCall{ID: "1", Name: "noop"}) // Args nil
	if err != nil {
		t.Fatalf("empty tool-call args must marshal, got %v", err)
	}
	if !strings.Contains(string(b), `"args":{}`) {
		t.Fatalf("expected args:{}, got %s", b)
	}
}

func TestToolDef_EmptyInputSchemaMarshals(t *testing.T) {
	b, err := json.Marshal(ToolDef{Name: "noop", Description: "d"}) // InputSchema nil
	if err != nil {
		t.Fatalf("empty input schema must marshal, got %v", err)
	}
	if !strings.Contains(string(b), `"input_schema":{}`) {
		t.Fatalf("expected input_schema:{}, got %s", b)
	}
}

// The exact regression that lost a whole run's output: a Message carrying a tool
// call whose args the LLM left empty must still marshal.
func TestMessage_WithEmptyToolCallArgs_Marshals(t *testing.T) {
	m := Message{Role: "assistant", ToolCalls: []MessageToolCall{{ID: "1", Name: "noop"}}}
	if _, err := json.Marshal(m); err != nil {
		t.Fatalf("message with empty tool-call args must marshal, got %v", err)
	}
}
