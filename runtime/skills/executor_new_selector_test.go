package skills

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/selection"
)

// fakeSelector is a test double for selection.Selector.
type fakeSelector struct {
	name     string
	selected []string
	err      error
	saw      selection.Query
	called   bool
}

func (f *fakeSelector) Name() string                         { return f.name }
func (f *fakeSelector) Init(selection.SelectorContext) error { return nil }

func (f *fakeSelector) Select(_ context.Context, q selection.Query,
	_ []selection.Candidate,
) ([]string, error) {
	f.called = true
	f.saw = q
	return f.selected, f.err
}

func newSelectorTestExecutor(t *testing.T, names ...string) *Executor {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		writeSkillWithTools(t, dir, n, "desc-"+n, "body-"+n, nil)
	}
	return newTestExecutor(t, dir, nil, 0)
}

func TestSkillIndexFiltered_NoSelector_AllSurface(t *testing.T) {
	e := newSelectorTestExecutor(t, "alpha", "beta", "gamma")
	out := e.SkillIndexFiltered(context.Background(), "anything", "")
	for _, n := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(out, n) {
			t.Errorf("expected %q in output:\n%s", n, out)
		}
	}
}

func TestSkillIndexFiltered_EmptyQuery_BypassesSelector(t *testing.T) {
	sel := &fakeSelector{name: "s", selected: []string{"alpha"}}
	e := newSelectorTestExecutor(t, "alpha", "beta")
	e.SetNewSelector(sel)

	out := e.SkillIndexFiltered(context.Background(), "", "")
	if sel.called {
		t.Fatal("selector must not be called when query is empty")
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("beta should surface when selector skipped:\n%s", out)
	}
}

func TestSkillIndexFiltered_SelectorNarrows(t *testing.T) {
	sel := &fakeSelector{name: "s", selected: []string{"beta"}}
	e := newSelectorTestExecutor(t, "alpha", "beta", "gamma")
	e.SetNewSelector(sel)

	out := e.SkillIndexFiltered(context.Background(), "about beta", "")
	if !sel.called {
		t.Fatal("selector should have been called")
	}
	if sel.saw.Text != "about beta" || sel.saw.Kind != "skill" {
		t.Errorf("query forwarded wrong: %+v", sel.saw)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("beta missing:\n%s", out)
	}
	if strings.Contains(out, "alpha") || strings.Contains(out, "gamma") {
		t.Errorf("alpha/gamma should have been filtered out:\n%s", out)
	}
}

func TestSkillIndexFiltered_SelectorError_FallsBack(t *testing.T) {
	sel := &fakeSelector{name: "s", err: errors.New("rerank service down")}
	e := newSelectorTestExecutor(t, "alpha", "beta")
	e.SetNewSelector(sel)

	out := e.SkillIndexFiltered(context.Background(), "q", "")
	for _, n := range []string{"alpha", "beta"} {
		if !strings.Contains(out, n) {
			t.Errorf("expected %q in output after selector error:\n%s", n, out)
		}
	}
}

func TestSkillIndexFiltered_EmptyResult_FallsBack(t *testing.T) {
	sel := &fakeSelector{name: "s", selected: nil}
	e := newSelectorTestExecutor(t, "alpha", "beta")
	e.SetNewSelector(sel)

	out := e.SkillIndexFiltered(context.Background(), "q", "")
	for _, n := range []string{"alpha", "beta"} {
		if !strings.Contains(out, n) {
			t.Errorf("expected %q after empty selection:\n%s", n, out)
		}
	}
}

func TestSkillIndexFiltered_NoMatchInCandidates_FallsBack(t *testing.T) {
	// Selector returns IDs that don't match any candidate — treat as
	// empty and fall back to full set rather than surfacing nothing.
	sel := &fakeSelector{name: "s", selected: []string{"ghost"}}
	e := newSelectorTestExecutor(t, "alpha", "beta")
	e.SetNewSelector(sel)

	out := e.SkillIndexFiltered(context.Background(), "q", "")
	for _, n := range []string{"alpha", "beta"} {
		if !strings.Contains(out, n) {
			t.Errorf("expected %q in fallback:\n%s", n, out)
		}
	}
}
