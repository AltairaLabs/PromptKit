package gemini

import (
	"reflect"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
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

	t.Run("IsError sets error:true", func(t *testing.T) {
		msg := buildToolResponseMessage([]providers.ToolResponse{
			{ToolCallID: "id3", Result: "boom", IsError: true},
		})
		frs := unwrap(t, msg)
		if frs[0]["error"] != true {
			t.Errorf("expected error:true, got %v", frs[0]["error"])
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
		msg := buildToolResponseMessage(nil)
		frs := unwrap(t, msg)
		if len(frs) != 0 {
			t.Errorf("expected empty functionResponses, got %v", frs)
		}
	})
}
