package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestToolCallRecord_JSONRoundTrip(t *testing.T) {
	original := ToolCallRecord{
		TurnIndex: 2,
		ToolName:  "create_ticket",
		Arguments: map[string]any{"title": "Bug fix", "priority": "high"},
		Result:    map[string]any{"id": "T-123"},
		Error:     "",
		Duration:  150 * time.Millisecond,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ToolCallRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.ToolName != original.ToolName {
		t.Errorf("ToolName = %q, want %q", decoded.ToolName, original.ToolName)
	}
	if decoded.TurnIndex != original.TurnIndex {
		t.Errorf("TurnIndex = %d, want %d", decoded.TurnIndex, original.TurnIndex)
	}
	if decoded.Duration != original.Duration {
		t.Errorf("Duration = %v, want %v", decoded.Duration, original.Duration)
	}
}

func TestToolCallRecord_OmitsEmptyFields(t *testing.T) {
	tc := ToolCallRecord{
		TurnIndex: 0,
		ToolName:  "search",
		Arguments: map[string]any{"q": "test"},
	}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}

	if _, ok := raw["result"]; ok {
		t.Error("result should be omitted when nil")
	}
	if _, ok := raw["error"]; ok {
		t.Error("error should be omitted when empty")
	}
	if _, ok := raw["duration"]; ok {
		t.Error("duration should be omitted when zero")
	}
}
