package tools

import (
	"encoding/json"
	"testing"
)

func TestDecodeArgsExtras_TypedAndUnknown(t *testing.T) {
	type typed struct {
		Content string `json:"content"`
		Type    string `json:"type"`
	}
	args := json.RawMessage(`{"content":"hello","type":"fact","about":"the dog","priority":3}`)
	var got typed
	extras, err := DecodeArgsExtras(args, &got, "content", "type")
	if err != nil {
		t.Fatalf("DecodeArgsExtras: %v", err)
	}
	if got.Content != "hello" || got.Type != "fact" {
		t.Errorf("typed decode wrong: %+v", got)
	}
	if extras["about"] != "the dog" {
		t.Errorf("expected extras.about='the dog', got %v", extras["about"])
	}
	if v, ok := extras["priority"].(float64); !ok || v != 3 {
		t.Errorf("expected extras.priority=3 (float64), got %v", extras["priority"])
	}
	if _, leaked := extras["content"]; leaked {
		t.Errorf("known key 'content' leaked into extras")
	}
}

func TestDecodeArgsExtras_NoExtras(t *testing.T) {
	type typed struct {
		Content string `json:"content"`
	}
	args := json.RawMessage(`{"content":"hello"}`)
	var got typed
	extras, err := DecodeArgsExtras(args, &got, "content")
	if err != nil {
		t.Fatalf("DecodeArgsExtras: %v", err)
	}
	if len(extras) != 0 {
		t.Errorf("expected no extras, got %+v", extras)
	}
}

func TestDecodeArgsExtras_EmptyAndNullArgs(t *testing.T) {
	type typed struct{}
	for _, raw := range []string{"", "null"} {
		var got typed
		extras, err := DecodeArgsExtras(json.RawMessage(raw), &got)
		if err != nil {
			t.Fatalf("DecodeArgsExtras(%q): %v", raw, err)
		}
		if extras != nil {
			t.Errorf("DecodeArgsExtras(%q): expected nil extras, got %+v", raw, extras)
		}
	}
}

func TestDecodeArgsExtras_NestedObjectPreserved(t *testing.T) {
	type typed struct {
		Content string `json:"content"`
	}
	args := json.RawMessage(`{"content":"x","about":{"kind":"preference","key":"seat"}}`)
	var got typed
	extras, err := DecodeArgsExtras(args, &got, "content")
	if err != nil {
		t.Fatalf("DecodeArgsExtras: %v", err)
	}
	about, ok := extras["about"].(map[string]any)
	if !ok {
		t.Fatalf("expected extras.about to be a map, got %T", extras["about"])
	}
	if about["kind"] != "preference" || about["key"] != "seat" {
		t.Errorf("nested about not preserved: %+v", about)
	}
}

func TestDecodeArgsExtras_TypedDecodeError(t *testing.T) {
	type typed struct {
		Count int `json:"count"`
	}
	args := json.RawMessage(`{"count":"not-a-number"}`)
	var got typed
	if _, err := DecodeArgsExtras(args, &got); err == nil {
		t.Fatal("expected typed decode error, got nil")
	}
}

func TestMergeExtrasIntoMetadata_TypedWinsOnConflict(t *testing.T) {
	target := map[string]any{"x": 1, "y": 2}
	extras := map[string]any{"x": 99, "z": 3}
	got := MergeExtrasIntoMetadata(target, extras)
	if got["x"] != 1 {
		t.Errorf("typed value should win on conflict; got x=%v", got["x"])
	}
	if got["z"] != 3 {
		t.Errorf("expected z=3 from extras; got %v", got["z"])
	}
}

func TestMergeExtrasIntoMetadata_NilTarget(t *testing.T) {
	got := MergeExtrasIntoMetadata(nil, map[string]any{"a": 1})
	if got["a"] != 1 {
		t.Errorf("expected fresh map with a=1; got %+v", got)
	}
}

func TestMergeExtrasIntoMetadata_BothEmpty(t *testing.T) {
	if got := MergeExtrasIntoMetadata(nil, nil); got != nil {
		t.Errorf("expected nil result for two empty inputs; got %+v", got)
	}
	if got := MergeExtrasIntoMetadata(nil, map[string]any{}); got != nil {
		t.Errorf("expected nil result for empty extras; got %+v", got)
	}
}
