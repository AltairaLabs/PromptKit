package layout

import "testing"

func TestPaneDefaults(t *testing.T) {
	p := Pane("logs")
	if p.ID != "logs" {
		t.Fatalf("ID = %q, want logs", p.ID)
	}
	if p.Mode != ModeFlex || p.Weight != 1 || !p.Visible {
		t.Fatalf("got mode=%v weight=%d visible=%v; want Flex,1,true", p.Mode, p.Weight, p.Visible)
	}
	if !p.IsLeaf() {
		t.Fatal("Pane should be a leaf")
	}
}

func TestBuilderComposition(t *testing.T) {
	root := VSplit(
		Fixed(3, Pane("header")),
		Optional(false, Pane("audio")),
		Flex(2, Pane("body", Min(40, 5))),
	)
	if root.Dir != Vertical || root.IsLeaf() {
		t.Fatal("VSplit should be a vertical split")
	}
	if root.Children[0].Mode != ModeFixed || root.Children[0].Size != 3 {
		t.Fatalf("header: got mode=%v size=%d; want Fixed,3", root.Children[0].Mode, root.Children[0].Size)
	}
	if root.Children[1].Visible {
		t.Fatal("audio should be invisible via Optional(false)")
	}
	body := root.Children[2]
	if body.Mode != ModeFlex || body.Weight != 2 || body.MinW != 40 || body.MinH != 5 {
		t.Fatalf("body: got mode=%v weight=%d min=(%d,%d); want Flex,2,(40,5)", body.Mode, body.Weight, body.MinW, body.MinH)
	}
}
