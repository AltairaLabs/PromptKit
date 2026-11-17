package gemini

import (
	"reflect"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func TestBuildToolResponseMessage(t *testing.T) {
	// unwrap navigates toolResponse.functionResponses[i].
	unwrap := func(t *testing.T, msg map[string]interface{}) []map[string]interface{} {
		t.Helper()
		tr, ok := msg["toolResponse"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected toolResponse key, got %v", msg)
		}
		frs, ok := tr["functionResponses"].([]map[string]interface{})
		if !ok {
			t.Fatalf("expected functionResponses slice, got %v", tr["functionResponses"])
		}
		return frs
	}

	t.Run("JSON result is parsed into a structured object", func(t *testing.T) {
		msg := buildToolResponseMessage([]providers.ToolResponse{
			{ToolCallID: "id1", Result: `{"temp":72,"unit":"F"}`},
		})
		frs := unwrap(t, msg)
		if len(frs) != 1 {
			t.Fatalf("expected 1 response, got %d", len(frs))
		}
		if frs[0]["id"] != "id1" {
			t.Errorf("id = %v", frs[0]["id"])
		}
		resp, ok := frs[0]["response"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected parsed object response, got %T", frs[0]["response"])
		}
		// JSON numbers decode to float64
		if resp["temp"].(float64) != 72 || resp["unit"] != "F" {
			t.Errorf("parsed response = %v", resp)
		}
		if _, ok := frs[0]["error"]; ok {
			t.Error("expected no error key for successful result")
		}
	})

	t.Run("non-JSON string is wrapped as {result: ...}", func(t *testing.T) {
		msg := buildToolResponseMessage([]providers.ToolResponse{
			{ToolCallID: "id2", Result: "plain text"},
		})
		frs := unwrap(t, msg)
		want := map[string]interface{}{"result": "plain text"}
		if !reflect.DeepEqual(frs[0]["response"], want) {
			t.Errorf("response = %v, want %v", frs[0]["response"], want)
		}
	})

	// FunctionResponse has no error flag: the Live API reads a failure from
	// the response's "error" key ("output" and "error" name the outcome; with
	// neither, the whole response is the output). A top-level "error": true is
	// an unknown field, and the session never answers the call.
	t.Run("IsError puts the text under response.error", func(t *testing.T) {
		msg := buildToolResponseMessage([]providers.ToolResponse{
			{ToolCallID: "id3", Result: "Tool x blocked by hook: frozen", IsError: true},
		})
		frs := unwrap(t, msg)
		want := map[string]interface{}{
			"id":       "id3",
			"response": map[string]interface{}{"error": "Tool x blocked by hook: frozen"},
		}
		if !reflect.DeepEqual(frs[0], want) {
			t.Errorf("function response = %v, want %v", frs[0], want)
		}
	})

	t.Run("a JSON error result is still reported as an error", func(t *testing.T) {
		msg := buildToolResponseMessage([]providers.ToolResponse{
			{ToolCallID: "id4", Result: `{"code":503}`, IsError: true},
		})
		frs := unwrap(t, msg)
		want := map[string]interface{}{"error": `{"code":503}`}
		if !reflect.DeepEqual(frs[0]["response"], want) {
			t.Errorf("response = %v, want %v", frs[0]["response"], want)
		}
	})

	// response is a JSON object (a protobuf Struct): a result that parses to
	// an array or a scalar is wrapped, as a text turn's function response is.
	t.Run("a JSON array or scalar result is wrapped as {result: ...}", func(t *testing.T) {
		msg := buildToolResponseMessage([]providers.ToolResponse{
			{ToolCallID: "a", Result: `[{"content":"teal"}]`},
			{ToolCallID: "b", Result: `42`},
			{ToolCallID: "c", Result: `null`},
		})
		frs := unwrap(t, msg)
		wants := []interface{}{
			map[string]interface{}{"result": []interface{}{map[string]interface{}{"content": "teal"}}},
			map[string]interface{}{"result": float64(42)},
			map[string]interface{}{"result": nil},
		}
		for i, want := range wants {
			if !reflect.DeepEqual(frs[i]["response"], want) {
				t.Errorf("response[%d] = %#v, want %#v", i, frs[i]["response"], want)
			}
		}
	})

	t.Run("multiple parallel responses preserve order", func(t *testing.T) {
		msg := buildToolResponseMessage([]providers.ToolResponse{
			{ToolCallID: "a", Result: "1"},
			{ToolCallID: "b", Result: "2"},
		})
		frs := unwrap(t, msg)
		if len(frs) != 2 || frs[0]["id"] != "a" || frs[1]["id"] != "b" {
			t.Errorf("responses = %v", frs)
		}
	})

	t.Run("empty input yields empty slice", func(t *testing.T) {
		msg := toolResponseMessage(nil, nil)
		frs := unwrap(t, msg)
		if len(frs) != 0 {
			t.Errorf("expected empty functionResponses, got %v", frs)
		}
	})
}

// buildToolResponseMessage is the toolResponse message of text-only responses.
func buildToolResponseMessage(responses []providers.ToolResponse) map[string]interface{} {
	return toolResponseMessage(responses, nil)
}
