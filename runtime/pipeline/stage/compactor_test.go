package stage

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toolResultMsg(name, content string) types.Message {
	tr := types.NewTextToolResult("call-"+name, name, content)
	return types.NewToolResultMessage(tr)
}

func toolResultMsgWithError(name, content, errMsg string) types.Message {
	tr := types.NewTextToolResult("call-"+name, name, content)
	tr.Error = errMsg
	return types.NewToolResultMessage(tr)
}

func largeToolResult(name string, wordCount int) types.Message {
	// Use realistic content with spaces so token counting works (~1.3 tokens/word)
	content := strings.Repeat("the quick brown fox jumps over the lazy dog ", wordCount/9+1)
	maxLen := wordCount * 6
	if maxLen > len(content) {
		maxLen = len(content)
	}
	return toolResultMsg(name, content[:maxLen])
}

func TestCompactor_NoOpUnderThreshold(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 10000,
		Threshold:         0.70,
	}

	msgs := []types.Message{
		{Role: "system", Content: "You are a helper"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "user", Content: "how are you"},
		{Role: "assistant", Content: "I'm good"},
	}

	result := c.Compact(msgs, 0)
	assert.Equal(t, len(msgs), len(result.Messages), "should not compact when under threshold")
	assert.Equal(t, msgs[0].Content, result.Messages[0].Content)
}

func TestCompactor_FoldsOldToolResults(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 500, // very low budget to force compaction
		Threshold:         0.70,
		PinRecentCount:    4,
	}

	msgs := []types.Message{
		{Role: "system", Content: "You are a helper"},
		{Role: "user", Content: "read main.go"},
		{Role: "assistant", Content: "I'll read that file"},
		largeToolResult("file_read", 2000), // old — should be compacted
		{Role: "assistant", Content: "Now read config.go"},
		largeToolResult("file_read", 2000), // old — should be compacted
		// Recent window (last 4):
		{Role: "assistant", Content: "Let me edit the file"},
		toolResultMsg("file_edit", "edit applied"),
		{Role: "user", Content: "run tests"},
		{Role: "assistant", Content: "running tests now"},
	}

	result := c.Compact(msgs, 0)

	// Recent 4 messages should be preserved verbatim
	assert.Equal(t, "Let me edit the file", result.Messages[len(result.Messages)-4].Content)
	assert.Equal(t, "run tests", result.Messages[len(result.Messages)-2].Content)

	// Old large tool results should be folded (contain "compacted")
	foundCompacted := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "compacted") {
			foundCompacted = true
			break
		}
	}
	assert.True(t, foundCompacted, "old tool results should be compacted")
}

func TestCompactor_PreservesSystemMessages(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 100, // very low to force compaction
		Threshold:         0.50,
		PinRecentCount:    2,
	}

	msgs := []types.Message{
		{Role: "system", Content: strings.Repeat("system prompt ", 50)}, // large system message
		{Role: "user", Content: "hello"},
		largeToolResult("search", 1000),
		{Role: "assistant", Content: "done"},
	}

	result := c.Compact(msgs, 0)

	// System message must be preserved regardless of size
	assert.Equal(t, "system", result.Messages[0].Role)
	assert.Contains(t, result.Messages[0].Content, "system prompt")
}

func TestCompactor_PreservesErrors(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 100,
		Threshold:         0.50,
		PinRecentCount:    2,
	}

	msgs := []types.Message{
		{Role: "user", Content: "do something"},
		toolResultMsgWithError("failing_tool", strings.Repeat("error details ", 100), "tool failed"),
		{Role: "assistant", Content: "I see the error"},
		{Role: "user", Content: "try again"},
	}

	result := c.Compact(msgs, 0)

	// Error tool result must be preserved verbatim
	for _, msg := range result.Messages {
		if msg.ToolResult != nil && msg.ToolResult.Error != "" {
			assert.Contains(t, msg.Content, "error details",
				"error tool results should not be compacted")
		}
	}
}

func TestCompactor_PreservesRecentMessages(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 200,
		Threshold:         0.50,
		PinRecentCount:    4,
	}

	msgs := []types.Message{
		{Role: "user", Content: "start"},
		largeToolResult("old_tool", 2000),
		// Recent 4:
		{Role: "assistant", Content: "recent-1"},
		largeToolResult("recent_tool", 2000), // large but recent — pinned
		{Role: "user", Content: "recent-3"},
		{Role: "assistant", Content: "recent-4"},
	}

	result := c.Compact(msgs, 0)

	// Last 4 messages must be untouched
	n := len(result.Messages)
	require.GreaterOrEqual(t, n, 4)
	assert.Equal(t, "recent-1", result.Messages[n-4].Content)
	assert.Equal(t, "recent-3", result.Messages[n-2].Content)
	assert.Equal(t, "recent-4", result.Messages[n-1].Content)

	// The recent tool result should NOT be compacted
	assert.NotContains(t, result.Messages[n-3].Content, "compacted",
		"recent tool result should be preserved verbatim")
}

func TestCompactor_IdempotentOnAlreadyCompacted(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 200,
		Threshold:         0.50,
		PinRecentCount:    2,
	}

	msgs := []types.Message{
		{Role: "user", Content: "hello"},
		largeToolResult("big_tool", 2000),
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}

	// First compaction
	result1 := c.Compact(msgs, 0)
	// Second compaction on already-compacted result
	result2 := c.Compact(result1.Messages, 0)

	assert.Equal(t, len(result1.Messages), len(result2.Messages), "second compaction should be a no-op")
	for i := range result1.Messages {
		assert.Equal(t, result1.Messages[i].Content, result2.Messages[i].Content)
	}
}

func TestCompactor_FoldsOldestFirst(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 2000,
		Threshold:         0.50,
		PinRecentCount:    2,
	}

	msgs := []types.Message{
		{Role: "user", Content: "step 1"},
		largeToolResult("tool_oldest", 1000),
		{Role: "assistant", Content: "step 2"},
		largeToolResult("tool_middle", 1000),
		{Role: "assistant", Content: "step 3"},
		largeToolResult("tool_recent_ish", 1000),
		// Recent 2:
		{Role: "user", Content: "final"},
		{Role: "assistant", Content: "done"},
	}

	result := c.Compact(msgs, 0)

	// The oldest tool result should be compacted first
	for _, msg := range result.Messages {
		if msg.ToolResult != nil && msg.ToolResult.Name == "tool_oldest" {
			assert.Contains(t, msg.Content, "compacted",
				"oldest tool result should be compacted first")
		}
	}
}

func TestCompactor_DisabledWhenBudgetZero(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 0, // disabled
	}

	msgs := []types.Message{
		{Role: "user", Content: "hello"},
		largeToolResult("big", 10000),
		{Role: "assistant", Content: "done"},
	}

	result := c.Compact(msgs, 0)
	assert.Equal(t, len(msgs), len(result.Messages), "should not compact when budget is 0")
}

func TestCompactor_NilCompactorSafe(t *testing.T) {
	var c *ContextCompactor
	msgs := []types.Message{{Role: "user", Content: "hello"}}

	// Should not panic
	result := c.Compact(msgs, 0)
	assert.Equal(t, msgs, result.Messages)
}

func TestFoldToolResult(t *testing.T) {
	content := fmt.Sprintf("package main\n\nfunc main() {\n%s\n}", strings.Repeat("\tfmt.Println(\"hello\")\n", 50))
	msg := toolResultMsg("file_read", content)

	folded := foldToolResult(&msg)
	assert.Contains(t, folded, "file_read")
	assert.Contains(t, folded, "compacted")
	assert.Less(t, len(folded), len(content), "folded should be much shorter")
}

func TestFoldToolResult_MultimodalParts(t *testing.T) {
	text1 := strings.Repeat("hello ", 100)
	msg := types.Message{
		Role: "tool",
		ToolResult: &types.MessageToolResult{
			ID:   "call-img",
			Name: "screenshot",
		},
		Parts: []types.ContentPart{
			{Type: "text", Text: &text1},
			{Type: "image", Media: &types.MediaContent{Data: stringPtr(strings.Repeat("x", 500)), MIMEType: "image/png"}},
		},
	}

	folded := foldToolResult(&msg)
	assert.Contains(t, folded, "screenshot")
	assert.Contains(t, folded, "2 parts")
	assert.Contains(t, folded, compactedMarker)
}

func TestFoldToolResult_NoToolResultName(t *testing.T) {
	msg := types.Message{
		Role:    "tool",
		Content: strings.Repeat("data ", 100),
	}

	folded := foldToolResult(&msg)
	assert.Contains(t, folded, "tool:")
	assert.Contains(t, folded, compactedMarker)
}

func TestFoldToolResult_ShortContent(t *testing.T) {
	msg := toolResultMsg("short_tool", "brief result here")

	folded := foldToolResult(&msg)
	// Short content — no "..." truncation
	assert.Contains(t, folded, "brief result here")
	assert.NotContains(t, folded, "...")
	assert.Contains(t, folded, compactedMarker)
}

func TestCompactor_LastInputTokensTriggersCompaction(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 10000,
		Threshold:         0.70,
		PinRecentCount:    2,
	}

	msgs := []types.Message{
		{Role: "user", Content: "hello"},
		largeToolResult("file_read", 500),
		{Role: "assistant", Content: "got it"},
		{Role: "user", Content: "next"},
	}

	// With heuristic counting, these messages are well under budget (10000 * 0.7 = 7000).
	result := c.Compact(msgs, 0)
	assert.Zero(t, result.MessagesFolded, "heuristic should not trigger compaction")

	// Pretend the provider reported 8000 input tokens (above 7000 threshold).
	// This should trigger compaction even though heuristic says we're fine.
	result = c.Compact(msgs, 8000)
	assert.Greater(t, result.MessagesFolded, 0, "lastInputTokens above threshold should trigger compaction")
}

func TestCompactor_LastInputTokensBelowThresholdSkips(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 10000,
		Threshold:         0.70,
		PinRecentCount:    2,
	}

	msgs := []types.Message{
		{Role: "user", Content: "hello"},
		largeToolResult("file_read", 500),
		{Role: "assistant", Content: "got it"},
		{Role: "user", Content: "next"},
	}

	// Provider says 5000 tokens — under 7000 threshold.
	result := c.Compact(msgs, 5000)
	assert.Zero(t, result.MessagesFolded, "lastInputTokens below threshold should skip compaction")
}

func TestCompactor_CompactResultMetadata(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 500,
		Threshold:         0.70,
		PinRecentCount:    2,
	}

	msgs := []types.Message{
		{Role: "user", Content: "start"},
		largeToolResult("big_tool", 2000),
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}

	result := c.Compact(msgs, 0)
	assert.Greater(t, result.MessagesFolded, 0, "should fold at least one message")
	assert.Greater(t, result.OriginalTokens, 0, "OriginalTokens should be positive")
	assert.Greater(t, result.CompactedTokens, 0, "CompactedTokens should be positive")
	assert.Less(t, result.CompactedTokens, result.OriginalTokens,
		"CompactedTokens should be less than OriginalTokens after folding")
}

func TestCompactor_NoOpResultMetadata(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 10000,
		Threshold:         0.70,
	}

	msgs := []types.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}

	result := c.Compact(msgs, 0)
	assert.Zero(t, result.MessagesFolded)
	assert.Zero(t, result.OriginalTokens, "no-op should not compute tokens")
	assert.Zero(t, result.CompactedTokens)
}

// --- Strategy interface tests ---

type mockStrategy struct {
	called bool
	budget int
}

func (m *mockStrategy) Compact(msgs []types.Message, _ int) CompactResult {
	m.called = true
	return CompactResult{Messages: msgs, MessagesFolded: 42}
}
func (m *mockStrategy) BudgetTokens() int { return m.budget }

func TestCompactionStrategy_CustomStrategy(t *testing.T) {
	s := &mockStrategy{budget: 99999}
	msgs := []types.Message{{Role: "user", Content: "hello"}}

	result := s.Compact(msgs, 0)
	assert.True(t, s.called)
	assert.Equal(t, 42, result.MessagesFolded)
	assert.Equal(t, 99999, s.BudgetTokens())
}

func TestContextCompactor_ImplementsCompactionStrategy(t *testing.T) {
	// Compile-time check
	var _ CompactionStrategy = (*ContextCompactor)(nil)
}

// --- Custom rule tests ---

type alwaysFoldRule struct {
	name string
}

func (r *alwaysFoldRule) Name() string { return r.name }

func (r *alwaysFoldRule) CanFold(msg *types.Message, _ int, _ *CompactionContext) bool {
	return msg.Role == roleTool
}

func (r *alwaysFoldRule) Fold(msg *types.Message, _ int, _ *CompactionContext) (string, []int) {
	return "[custom-folded]", nil
}

func TestCompactor_CustomRules(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 100,
		Threshold:         0.50,
		PinRecentCount:    2,
		Rules:             []CompactionRule{&alwaysFoldRule{name: "custom"}},
	}

	msgs := []types.Message{
		{Role: "user", Content: "start"},
		largeToolResult("big_tool", 2000),
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}

	result := c.Compact(msgs, 0)
	assert.Greater(t, result.MessagesFolded, 0)

	// The custom rule's fold text should appear
	found := false
	for _, msg := range result.Messages {
		if strings.Contains(msg.Content, "custom-folded") {
			found = true
			break
		}
	}
	assert.True(t, found, "custom rule fold text should appear")
}

func TestCompactor_RuleFirstMatchWins(t *testing.T) {
	called := map[string]int{}
	rule1 := &trackingRule{name: "first", called: &called, canFold: true}
	rule2 := &trackingRule{name: "second", called: &called, canFold: true}

	c := &ContextCompactor{
		BudgetTokensValue: 100,
		Threshold:         0.50,
		PinRecentCount:    2,
		Rules:             []CompactionRule{rule1, rule2},
	}

	msgs := []types.Message{
		{Role: "user", Content: "start"},
		largeToolResult("tool", 2000),
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}

	c.Compact(msgs, 0)
	assert.Equal(t, 1, called["first"], "first rule should be called once")
	assert.Zero(t, called["second"], "second rule should not be called (first matched)")
}

type trackingRule struct {
	name    string
	called  *map[string]int
	canFold bool
}

func (r *trackingRule) Name() string { return r.name }
func (r *trackingRule) CanFold(msg *types.Message, _ int, _ *CompactionContext) bool {
	return r.canFold && msg.Role == roleTool && len(msg.GetContent()) >= 50
}
func (r *trackingRule) Fold(msg *types.Message, _ int, _ *CompactionContext) (string, []int) {
	(*r.called)[r.name]++
	return "[folded-by-" + r.name + "]", nil
}

func TestCompactor_DefaultRuleWhenNoRulesSet(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 100,
		Threshold:         0.50,
		PinRecentCount:    2,
		// Rules intentionally nil — should default to FoldToolResults()
	}

	msgs := []types.Message{
		{Role: "user", Content: "start"},
		largeToolResult("file_read", 2000),
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}

	result := c.Compact(msgs, 0)
	assert.Greater(t, result.MessagesFolded, 0)
	// Should use default FoldToolResults format
	for _, msg := range result.Messages {
		if msg.Role == roleTool && strings.Contains(msg.Content, compactedMarker) {
			assert.Contains(t, msg.Content, "file_read")
			break
		}
	}
}

// --- CollapsePairs rule tests ---

func assistantToolCallMsg(toolCallID, toolName, argsJSON string) types.Message {
	return types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{ID: toolCallID, Name: toolName, Args: json.RawMessage(argsJSON)},
		},
	}
}

func toolResultMsgWithID(id, name, content string) types.Message {
	tr := types.NewTextToolResult(id, name, content)
	return types.NewToolResultMessage(tr)
}

func TestCollapsePairs_SupersededPairCollapsed(t *testing.T) {
	c := &ContextCompactor{
		BudgetTokensValue: 100,
		Threshold:         0.50,
		PinRecentCount:    2,
		Rules:             []CompactionRule{CollapsePairs(), FoldToolResults()},
	}

	msgs := []types.Message{
		{Role: "user", Content: "start"},
		// First file_read("main.go") — should be collapsed
		assistantToolCallMsg("call-1", "file_read", `{"path":"main.go"}`),
		toolResultMsgWithID("call-1", "file_read", strings.Repeat("package main\n", 200)),
		// Second file_read("main.go") — supersedes the first
		assistantToolCallMsg("call-2", "file_read", `{"path":"main.go"}`),
		toolResultMsgWithID("call-2", "file_read", strings.Repeat("package main\nfunc main() {}\n", 200)),
		// Recent:
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}

	result := c.Compact(msgs, 0)

	// The first assistant+tool pair should be collapsed
	foundSuperseded := false
	for _, msg := range result.Messages {
		if strings.Contains(msg.Content, "superseded") {
			foundSuperseded = true
			break
		}
	}
	assert.True(t, foundSuperseded, "superseded pair should be collapsed")

	// The message count should be less (assistant msg removed)
	assert.Less(t, len(result.Messages), len(msgs), "collapsed pair should reduce message count")
}

func TestCollapsePairs_DifferentArgsDontCollapse(t *testing.T) {
	rule := CollapsePairs()

	msgs := []types.Message{
		{Role: "user", Content: "start"},
		assistantToolCallMsg("call-1", "file_read", `{"path":"main.go"}`),
		toolResultMsgWithID("call-1", "file_read", strings.Repeat("content a ", 100)),
		assistantToolCallMsg("call-2", "file_read", `{"path":"config.go"}`),
		toolResultMsgWithID("call-2", "file_read", strings.Repeat("content b ", 100)),
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}

	ctx := &CompactionContext{
		Messages:    msgs,
		PinBoundary: 5,
	}

	// The first file_read("main.go") should NOT be superseded by file_read("config.go")
	assert.False(t, rule.CanFold(&msgs[2], 2, ctx),
		"different args should not be considered superseded")
}

func TestCollapsePairs_ErrorsNotCollapsed(t *testing.T) {
	rule := CollapsePairs()

	errResult := toolResultMsgWithError("file_read", strings.Repeat("error ", 50), "file not found")
	msgs := []types.Message{
		{Role: "user", Content: "start"},
		errResult,
		toolResultMsgWithID("call-2", "file_read", strings.Repeat("content ", 100)),
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}

	ctx := &CompactionContext{Messages: msgs, PinBoundary: 3}
	assert.False(t, rule.CanFold(&msgs[1], 1, ctx), "error results should never be collapsed")
}

func TestCollapsePairs_AlreadyCompactedSkipped(t *testing.T) {
	rule := CollapsePairs()

	msg := toolResultMsg("file_read", "[file_read: superseded — 500 bytes compacted]")
	msgs := []types.Message{
		msg,
		toolResultMsgWithID("call-2", "file_read", strings.Repeat("content ", 100)),
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}

	ctx := &CompactionContext{Messages: msgs, PinBoundary: 2}
	assert.False(t, rule.CanFold(&msgs[0], 0, ctx), "already compacted should be skipped")
}

func stringPtr(s string) *string { return &s }
