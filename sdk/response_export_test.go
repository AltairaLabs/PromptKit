package sdk

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNewResponseForTest_TextOnly(t *testing.T) {
	resp := NewResponseForTest("hello", nil)
	if resp.Text() != "hello" {
		t.Fatalf("expected text %q, got %q", "hello", resp.Text())
	}
	if len(resp.ToolCalls()) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls()))
	}
}

func TestNewResponseForTest_WithToolCalls(t *testing.T) {
	tcs := []types.MessageToolCall{
		{ID: "tc-1", Name: "search", Args: json.RawMessage(`{"q":"test"}`)},
	}
	resp := NewResponseForTest("", tcs)
	if resp.Text() != "" {
		t.Fatalf("expected empty text, got %q", resp.Text())
	}
	if len(resp.ToolCalls()) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls()))
	}
	if resp.ToolCalls()[0].Name != "search" {
		t.Fatalf("expected tool name %q, got %q", "search", resp.ToolCalls()[0].Name)
	}
}
