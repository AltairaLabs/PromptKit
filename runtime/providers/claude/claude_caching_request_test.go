package claude

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// A tool-using request must (1) carry cache_control breakpoints (system +
// first-message prefix) so prompt caching can engage, and (2) serialize
// byte-identically for identical input so the cached prefix is stable across
// agent-loop rounds. A non-deterministic or breakpoint-less request means
// caching never engages and the full context is re-billed every round.
func TestToolRequest_CachingBreakpointsAndDeterminism(t *testing.T) {
	provider, err := providers.CreateProviderFromSpec(newClaudeSpec("https://example.invalid", nil))
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	tp := provider.(*ToolProvider)

	system := strings.Repeat("You are a meticulous codegen agent. ", 500) // ~18k chars > threshold
	tools, _ := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "Bash", Description: "run a shell command", InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`)},
		{Name: "Write", Description: "write a file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
		{Name: "Read", Description: "read a file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
	})

	bigOut := strings.Repeat("line of tool output\n", 300)
	msgs := []types.Message{
		{Role: "user", Content: strings.Repeat("Please author a kit. ", 50)},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "1", Name: "Bash", Args: json.RawMessage(`{"command":"ls"}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "1", Name: "Bash", Parts: []types.ContentPart{types.NewTextPart(bigOut)}}},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "2", Name: "Write", Args: json.RawMessage(`{"path":"kit/config.yaml"}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "2", Name: "Write", Parts: []types.ContentPart{types.NewTextPart(bigOut)}}},
		{Role: "assistant", Content: "Continuing..."},
	}

	build := func() []byte {
		req := tp.buildToolRequest(providers.PredictionRequest{System: system, Messages: msgs, MaxTokens: 100}, tools, "")
		b, mErr := json.Marshal(req)
		if mErr != nil {
			t.Fatalf("marshal request: %v", mErr)
		}
		return b
	}

	b1 := build()
	if n := bytes.Count(b1, []byte(`"cache_control"`)); n < 2 {
		t.Fatalf("expected >=2 cache_control breakpoints (system + first message), got %d", n)
	}
	// Identical input -> identical bytes, or the cached prefix shifts every round.
	if b2 := build(); !bytes.Equal(b1, b2) {
		t.Fatal("tool request is not byte-deterministic for identical input — cached prefix would change every round")
	}
}

// The cache breakpoint must sit on the LAST message so the whole *growing*
// conversation prefix is cached (rolling cache). Marking only the first message
// caches the static base and re-bills the growing tool-result history every round.
func TestToolRequest_RollingBreakpointOnLastMessage(t *testing.T) {
	provider, err := providers.CreateProviderFromSpec(newClaudeSpec("https://example.invalid", nil))
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	tp := provider.(*ToolProvider)
	tools, _ := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "Bash", Description: "run", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	msgs := []types.Message{
		{Role: "user", Content: "start"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "1", Name: "Bash", Args: json.RawMessage(`{"command":"ls"}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "1", Name: "Bash", Parts: []types.ContentPart{types.NewTextPart("big output")}}},
	}
	req := tp.buildToolRequest(providers.PredictionRequest{System: "sys", Messages: msgs, MaxTokens: 100}, tools, "")
	cms, ok := req["messages"].([]claudeToolMessage)
	if !ok || len(cms) == 0 {
		t.Fatalf("expected claude messages, got %T", req["messages"])
	}
	if !messageHasCacheControl(cms[len(cms)-1]) {
		t.Fatal("last message must carry a cache_control breakpoint so the growing prefix is cached")
	}
}

func messageHasCacheControl(m claudeToolMessage) bool {
	for _, b := range m.Content {
		switch v := b.(type) {
		case claudeTextContent:
			if v.CacheControl != nil {
				return true
			}
		case claudeToolUse:
			if v.CacheControl != nil {
				return true
			}
		case claudeToolResult:
			if v.CacheControl != nil {
				return true
			}
		}
	}
	return false
}
