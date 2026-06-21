package layout

import (
	"strings"
	"testing"
)

func TestRenderTreeVerticalJoinsLeaves(t *testing.T) {
	root := VSplit(Pane("a"), Pane("b"))
	got := RenderTree(root, map[string]string{"a": "AAA", "b": "BBB"})
	if got != "AAA\nBBB" {
		t.Fatalf("got %q, want %q", got, "AAA\nBBB")
	}
}

func TestRenderTreeSkipsInvisible(t *testing.T) {
	root := VSplit(Pane("a"), Optional(false, Pane("hidden")), Pane("b"))
	got := RenderTree(root, map[string]string{"a": "AAA", "hidden": "XXX", "b": "BBB"})
	if strings.Contains(got, "XXX") {
		t.Fatalf("invisible pane rendered: %q", got)
	}
}

func TestRenderTreeHorizontalSideBySide(t *testing.T) {
	root := HSplit(Pane("l"), Pane("r"))
	got := RenderTree(root, map[string]string{"l": "L", "r": "R"})
	if got != "LR" {
		t.Fatalf("got %q, want %q", got, "LR")
	}
}
