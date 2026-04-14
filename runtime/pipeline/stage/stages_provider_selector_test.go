package stage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/selection"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// stubToolSelector records what it was asked to select and returns
// a canned ID list. Lets tests assert on the query and candidate set
// the ProviderStage handed it.
type stubToolSelector struct {
	selected  []string
	err       error
	calls     int
	lastQuery selection.Query
	lastCands []selection.Candidate
}

func (s *stubToolSelector) Name() string                         { return "stub" }
func (s *stubToolSelector) Init(selection.SelectorContext) error { return nil }
func (s *stubToolSelector) Select(_ context.Context, q selection.Query,
	c []selection.Candidate,
) ([]string, error) {
	s.calls++
	s.lastQuery = q
	s.lastCands = c
	return s.selected, s.err
}

func registryWithTools(t *testing.T, names ...string) *tools.Registry {
	t.Helper()
	r := tools.NewRegistry()
	for _, n := range names {
		err := r.Register(&tools.ToolDescriptor{
			Name:        n,
			Description: "desc-" + n,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Mode:        "local",
		})
		if err != nil {
			t.Fatalf("Register %q: %v", n, err)
		}
	}
	return r
}

func userMessageOnly(text string) []types.Message {
	msg := types.Message{Role: "user"}
	msg.AddTextPart(text)
	return []types.Message{msg}
}

func newSelectorTestStage(reg *tools.Registry, sel selection.Selector) *ProviderStage {
	return &ProviderStage{
		toolRegistry: reg,
		config:       &ProviderConfig{ToolSelector: sel},
	}
}

func TestApplyToolSelector_NoSelector(t *testing.T) {
	reg := registryWithTools(t, "a", "b")
	s := newSelectorTestStage(reg, nil)
	acc := &providerInput{
		messages:     userMessageOnly("hello"),
		allowedTools: []string{"a", "b"},
	}
	s.applyToolSelector(context.Background(), acc)
	if len(acc.allowedTools) != 2 {
		t.Errorf("nil selector should not narrow; got %v", acc.allowedTools)
	}
}

func TestApplyToolSelector_NoUserQuery(t *testing.T) {
	reg := registryWithTools(t, "a", "b")
	sel := &stubToolSelector{selected: []string{"a"}}
	s := newSelectorTestStage(reg, sel)
	acc := &providerInput{
		messages:     []types.Message{{Role: "system"}}, // no user message
		allowedTools: []string{"a", "b"},
	}
	s.applyToolSelector(context.Background(), acc)
	if sel.calls != 0 {
		t.Errorf("selector should not be called without user query, got %d calls", sel.calls)
	}
	if len(acc.allowedTools) != 2 {
		t.Errorf("allowedTools must be unchanged; got %v", acc.allowedTools)
	}
}

func TestApplyToolSelector_NarrowsToSelectedSubset(t *testing.T) {
	reg := registryWithTools(t, "alpha", "beta", "gamma")
	sel := &stubToolSelector{selected: []string{"beta"}}
	s := newSelectorTestStage(reg, sel)
	acc := &providerInput{
		messages:     userMessageOnly("about beta please"),
		allowedTools: []string{"alpha", "beta", "gamma"},
	}
	s.applyToolSelector(context.Background(), acc)

	if sel.calls != 1 {
		t.Fatalf("expected 1 selector call, got %d", sel.calls)
	}
	if sel.lastQuery.Text != "about beta please" || sel.lastQuery.Kind != "tool" {
		t.Errorf("query forwarded wrong: %+v", sel.lastQuery)
	}
	if len(sel.lastCands) != 3 {
		t.Errorf("expected 3 candidates, got %d", len(sel.lastCands))
	}
	if len(acc.allowedTools) != 1 || acc.allowedTools[0] != "beta" {
		t.Errorf("allowedTools = %v, want [beta]", acc.allowedTools)
	}
}

func TestApplyToolSelector_PreservesSourceOrder(t *testing.T) {
	reg := registryWithTools(t, "alpha", "beta", "gamma")
	sel := &stubToolSelector{selected: []string{"gamma", "alpha"}} // out of source order
	s := newSelectorTestStage(reg, sel)
	acc := &providerInput{
		messages:     userMessageOnly("q"),
		allowedTools: []string{"alpha", "beta", "gamma"},
	}
	s.applyToolSelector(context.Background(), acc)
	want := []string{"alpha", "gamma"}
	if len(acc.allowedTools) != 2 || acc.allowedTools[0] != want[0] || acc.allowedTools[1] != want[1] {
		t.Errorf("allowedTools = %v, want %v (source order preserved)", acc.allowedTools, want)
	}
}

func TestApplyToolSelector_SelectorError_FallsBack(t *testing.T) {
	reg := registryWithTools(t, "alpha", "beta")
	sel := &stubToolSelector{err: errors.New("rerank down")}
	s := newSelectorTestStage(reg, sel)
	acc := &providerInput{
		messages:     userMessageOnly("q"),
		allowedTools: []string{"alpha", "beta"},
	}
	s.applyToolSelector(context.Background(), acc)
	if len(acc.allowedTools) != 2 {
		t.Errorf("error should fall back to full list; got %v", acc.allowedTools)
	}
}

func TestApplyToolSelector_EmptyResult_FallsBack(t *testing.T) {
	reg := registryWithTools(t, "alpha", "beta")
	sel := &stubToolSelector{selected: nil}
	s := newSelectorTestStage(reg, sel)
	acc := &providerInput{
		messages:     userMessageOnly("q"),
		allowedTools: []string{"alpha", "beta"},
	}
	s.applyToolSelector(context.Background(), acc)
	if len(acc.allowedTools) != 2 {
		t.Errorf("empty selection should fall back; got %v", acc.allowedTools)
	}
}

func TestApplyToolSelector_NoMatchInCandidates_FallsBack(t *testing.T) {
	reg := registryWithTools(t, "alpha", "beta")
	sel := &stubToolSelector{selected: []string{"ghost"}}
	s := newSelectorTestStage(reg, sel)
	acc := &providerInput{
		messages:     userMessageOnly("q"),
		allowedTools: []string{"alpha", "beta"},
	}
	s.applyToolSelector(context.Background(), acc)
	if len(acc.allowedTools) != 2 {
		t.Errorf("ID-mismatch should fall back; got %v", acc.allowedTools)
	}
}

func TestApplyToolSelector_EmptyAllowed_NoOp(t *testing.T) {
	reg := registryWithTools(t)
	sel := &stubToolSelector{selected: []string{"x"}}
	s := newSelectorTestStage(reg, sel)
	acc := &providerInput{
		messages:     userMessageOnly("q"),
		allowedTools: nil,
	}
	s.applyToolSelector(context.Background(), acc)
	if sel.calls != 0 {
		t.Errorf("should not call selector with no allowed tools, got %d calls", sel.calls)
	}
}

func TestLastUserMessageText(t *testing.T) {
	tests := []struct {
		name string
		msgs []types.Message
		want string
	}{
		{"empty", nil, ""},
		{"system only", []types.Message{{Role: "system"}}, ""},
		{"single user", userMessageOnly("hello"), "hello"},
		{"picks last user", append(userMessageOnly("first"), userMessageOnly("second")...), "second"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := lastUserMessageText(tc.msgs); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
