package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestOutputConfigFor verifies the ResponseFormat -> Anthropic output_config mapping.
func TestOutputConfigFor(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"type":{"type":"string"}},"required":["type"],"additionalProperties":false}`)

	if oc := outputConfigFor(nil); oc != nil {
		t.Errorf("nil ResponseFormat: want nil, got %#v", oc)
	}
	if oc := outputConfigFor(&providers.ResponseFormat{Type: providers.ResponseFormatText}); oc != nil {
		t.Errorf("text ResponseFormat: want nil, got %#v", oc)
	}
	if oc := outputConfigFor(&providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema}); oc != nil {
		t.Errorf("json_schema with empty schema: want nil, got %#v", oc)
	}

	oc := outputConfigFor(&providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema, JSONSchema: schema})
	if oc == nil {
		t.Fatal("json_schema ResponseFormat with schema: want config, got nil")
	}
	if oc.Format.Type != "json_schema" {
		t.Errorf("format type = %q, want json_schema", oc.Format.Type)
	}
	if string(oc.Format.Schema) != string(schema) {
		t.Errorf("schema = %s, want %s", oc.Format.Schema, schema)
	}
}

// TestClaudeRequest_OutputConfigSerialization verifies output_config serializes to
// the Anthropic-expected shape and is omitted when unset.
func TestClaudeRequest_OutputConfigSerialization(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"type":{"type":"string"}},"required":["type"],"additionalProperties":false}`)

	withOC := claudeRequest{
		Model:        "claude-haiku-4-5",
		MaxTokens:    512,
		OutputConfig: outputConfigFor(&providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema, JSONSchema: schema}),
	}
	b, err := json.Marshal(withOC)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, `"output_config":{"format":{"type":"json_schema","schema":{`) {
		t.Errorf("serialized request missing expected output_config shape: %s", got)
	}

	// Omitted when no ResponseFormat is set.
	withoutOC := claudeRequest{Model: "claude-haiku-4-5", MaxTokens: 512}
	b2, err := json.Marshal(withoutOC)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b2), "output_config") {
		t.Errorf("output_config should be omitted when unset: %s", string(b2))
	}
}
