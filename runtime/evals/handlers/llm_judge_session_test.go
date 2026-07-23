package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// capturingJudgeProvider records the content it was asked to judge, so a test
// can assert on what actually reached the judge through the handler path.
type capturingJudgeProvider struct {
	content string
}

func (c *capturingJudgeProvider) Judge(_ context.Context, opts JudgeOpts) (*JudgeResult, error) {
	c.content = opts.Content
	return &JudgeResult{Passed: true, Score: 1.0, Reasoning: "ok"}, nil
}

// TestRenderSessionTranscript_IncludesUserTurnsToolCallsAndResults is the core of
// #1615: the session/conversation judge must see the whole interaction, not just
// assistant prose. A silent tool-using turn (no assistant text, one tool call)
// used to render an empty string and score near zero; the transcript must now
// carry the user's turn, the tool name, its arguments, and its result.
func TestRenderSessionTranscript_IncludesUserTurnsToolCallsAndResults(t *testing.T) {
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "user", Content: "My address is 742 Evergreen Terrace, Springfield, IL"},
			{Role: "assistant", Content: ""}, // silent tool-only turn
		},
		ToolCalls: []evals.ToolCallRecord{
			{
				TurnIndex: 0,
				ToolName:  "lookup_property",
				Arguments: map[string]any{"address": "742 Evergreen Terrace, Springfield, IL"},
				Result:    map[string]any{"parcel_id": "SPR-742-ET"},
			},
		},
	}

	got := renderSessionTranscript(evalCtx)

	if strings.TrimSpace(got) == "" {
		t.Fatal("transcript must not be empty for a silent tool-only turn")
	}
	for _, want := range []string{
		"742 Evergreen Terrace", // the user's turn
		"lookup_property",       // the tool that was called
		"parcel_id",             // the tool's result
	} {
		if !strings.Contains(got, want) {
			t.Errorf("transcript missing %q\n---\n%s", want, got)
		}
	}
	if !strings.Contains(strings.ToLower(got), "user") {
		t.Errorf("transcript must label the user turn\n---\n%s", got)
	}
}

// TestRenderSessionTranscript_LabelsRolesAndSkipsSystem checks the transcript
// labels each speaking turn by its role (including non-standard roles like a
// customer/observer setup) and omits the system prompt, which is setup, not
// interaction.
func TestRenderSessionTranscript_LabelsRolesAndSkipsSystem(t *testing.T) {
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "system", Content: "SECRET-SYSTEM-PROMPT"},
			{Role: "customer", Content: "hello there"},
			{Role: "assistant", Content: "hi, how can I help?"},
		},
	}

	got := renderSessionTranscript(evalCtx)

	if strings.Contains(got, "SECRET-SYSTEM-PROMPT") {
		t.Errorf("system prompt must be excluded from the judge transcript\n---\n%s", got)
	}
	if !strings.Contains(got, "hello there") || !strings.Contains(got, "hi, how can I help?") {
		t.Errorf("transcript must include the customer and assistant turns\n---\n%s", got)
	}
	if !strings.Contains(strings.ToLower(got), "customer") {
		t.Errorf("transcript must preserve the customer role label\n---\n%s", got)
	}
}

// TestRenderSessionTranscript_EmptyContextIsSafe guards against a panic and
// returns an empty transcript when there is nothing to render.
func TestRenderSessionTranscript_EmptyContextIsSafe(t *testing.T) {
	if got := renderSessionTranscript(&evals.EvalContext{}); got != "" {
		t.Errorf("empty context should render an empty transcript, got %q", got)
	}
}

// TestLLMJudgeSessionHandler_FeedsFullTranscriptToJudge is the end-to-end guard:
// the wired handler must hand the judge the full transcript (tool calls
// included), not the old assistant-text-only view. Injects a capturing judge and
// asserts on what it received.
func TestLLMJudgeSessionHandler_FeedsFullTranscriptToJudge(t *testing.T) {
	judge := &capturingJudgeProvider{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "user", Content: "look up 742 Evergreen Terrace"},
			{Role: "assistant", Content: ""}, // tool-only turn
		},
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "lookup_property", Arguments: map[string]any{"address": "742 Evergreen Terrace"}},
		},
		Metadata: map[string]any{"judge_provider": JudgeProvider(judge)},
	}

	h := &LLMJudgeSessionHandler{}
	if _, err := h.Eval(context.Background(), evalCtx, map[string]any{"criteria": "did it look up the address?"}); err != nil {
		t.Fatalf("Eval: %v", err)
	}

	if !strings.Contains(judge.content, "lookup_property") {
		t.Errorf("judge never saw the tool call; content=\n%s", judge.content)
	}
	if !strings.Contains(judge.content, "look up 742 Evergreen Terrace") {
		t.Errorf("judge never saw the user turn; content=\n%s", judge.content)
	}
}
