package layout

import "testing"

func TestSolveSingleLeafFillsArea(t *testing.T) {
	got := Solve(Pane("only"), Rect{X: 0, Y: 0, W: 80, H: 24})
	want := Rect{X: 0, Y: 0, W: 80, H: 24}
	if got["only"] != want {
		t.Fatalf("only = %+v, want %+v", got["only"], want)
	}
}

func TestSolveVerticalFlexEqual(t *testing.T) {
	root := VSplit(Pane("top"), Pane("bottom")) // weights 1:1
	got := Solve(root, Rect{X: 0, Y: 0, W: 80, H: 24})
	if got["top"] != (Rect{X: 0, Y: 0, W: 80, H: 12}) {
		t.Fatalf("top = %+v", got["top"])
	}
	if got["bottom"] != (Rect{X: 0, Y: 12, W: 80, H: 12}) {
		t.Fatalf("bottom = %+v", got["bottom"])
	}
}

func TestSolveHorizontalWeighted(t *testing.T) {
	root := HSplit(Pane("left"), Flex(2, Pane("right"))) // 1:2 of width 90 -> 30:60
	got := Solve(root, Rect{X: 0, Y: 0, W: 90, H: 10})
	if got["left"] != (Rect{X: 0, Y: 0, W: 30, H: 10}) {
		t.Fatalf("left = %+v", got["left"])
	}
	if got["right"] != (Rect{X: 30, Y: 0, W: 60, H: 10}) {
		t.Fatalf("right = %+v", got["right"])
	}
}

func TestSolveRoundingIsDeterministic(t *testing.T) {
	// width 10 split 1:1:1 -> 4,3,3 (earliest child gets the extra cell)
	root := HSplit(Pane("a"), Pane("b"), Pane("c"))
	got := Solve(root, Rect{X: 0, Y: 0, W: 10, H: 1})
	if got["a"].W != 4 || got["b"].W != 3 || got["c"].W != 3 {
		t.Fatalf("widths = %d,%d,%d; want 4,3,3", got["a"].W, got["b"].W, got["c"].W)
	}
}

func TestSolveFixedReservesThenFlexFillsRest(t *testing.T) {
	// header fixed 3, footer fixed 1, body flex -> body = 24-3-1 = 20
	root := VSplit(
		Fixed(3, Pane("header")),
		Flex(1, Pane("body")),
		Fixed(1, Pane("footer")),
	)
	got := Solve(root, Rect{X: 0, Y: 0, W: 80, H: 24})
	if got["header"] != (Rect{X: 0, Y: 0, W: 80, H: 3}) {
		t.Fatalf("header = %+v", got["header"])
	}
	if got["body"] != (Rect{X: 0, Y: 3, W: 80, H: 20}) {
		t.Fatalf("body = %+v", got["body"])
	}
	if got["footer"] != (Rect{X: 0, Y: 23, W: 80, H: 1}) {
		t.Fatalf("footer = %+v", got["footer"])
	}
}

func TestSolveTwoFixedTwoFlex(t *testing.T) {
	// fixed 2 + fixed 2 = 4; remainder 16 split 1:3 -> 4 and 12
	root := HSplit(
		Fixed(2, Pane("g1")),
		Flex(1, Pane("f1")),
		Fixed(2, Pane("g2")),
		Flex(3, Pane("f2")),
	)
	got := Solve(root, Rect{X: 0, Y: 0, W: 20, H: 5})
	if got["g1"].W != 2 || got["f1"].W != 4 || got["g2"].W != 2 || got["f2"].W != 12 {
		t.Fatalf("widths = %d,%d,%d,%d; want 2,4,2,12", got["g1"].W, got["f1"].W, got["g2"].W, got["f2"].W)
	}
}

func TestSolveOptionalAbsentReclaimsSpace(t *testing.T) {
	// audio optional+absent: body should fill the space the audio row would take
	root := VSplit(
		Fixed(3, Pane("header")),
		Optional(false, Fixed(2, Pane("audio"))),
		Flex(1, Pane("body")),
	)
	got := Solve(root, Rect{X: 0, Y: 0, W: 80, H: 24})
	if _, ok := got["audio"]; ok {
		t.Fatal("audio should be absent from the result when invisible")
	}
	if got["body"] != (Rect{X: 0, Y: 3, W: 80, H: 21}) {
		t.Fatalf("body = %+v, want H=21 starting at Y=3", got["body"])
	}
}

func TestSolveOptionalPresentTakesSpace(t *testing.T) {
	root := VSplit(
		Fixed(3, Pane("header")),
		Optional(true, Fixed(2, Pane("audio"))),
		Flex(1, Pane("body")),
	)
	got := Solve(root, Rect{X: 0, Y: 0, W: 80, H: 24})
	if got["audio"] != (Rect{X: 0, Y: 3, W: 80, H: 2}) {
		t.Fatalf("audio = %+v", got["audio"])
	}
	if got["body"] != (Rect{X: 0, Y: 5, W: 80, H: 19}) {
		t.Fatalf("body = %+v, want H=19 starting at Y=5", got["body"])
	}
}

func TestSolveFlexRespectsMin(t *testing.T) {
	// width 50; left min 40, right min 0; 1:1 would give 25 each,
	// but left's min 40 wins -> left 40, right 10.
	root := HSplit(
		Pane("left", Min(40, 0)),
		Pane("right"),
	)
	got := Solve(root, Rect{X: 0, Y: 0, W: 50, H: 10})
	if got["left"].W != 40 || got["right"].W != 10 {
		t.Fatalf("widths = %d,%d; want 40,10", got["left"].W, got["right"].W)
	}
}

func TestSolveProportionalShrinkWhenMinsOverflow(t *testing.T) {
	// width 30 but mins demand 40+40=80; shrink proportionally -> 15,15.
	root := HSplit(
		Pane("a", Min(40, 0)),
		Pane("b", Min(40, 0)),
	)
	got := Solve(root, Rect{X: 0, Y: 0, W: 30, H: 10})
	if got["a"].W+got["b"].W != 30 {
		t.Fatalf("widths must sum to 30, got %d+%d", got["a"].W, got["b"].W)
	}
	if got["a"].W != 15 || got["b"].W != 15 {
		t.Fatalf("widths = %d,%d; want 15,15", got["a"].W, got["b"].W)
	}
}

func TestSolveNeverNegativeOrZeroOnTinyArea(t *testing.T) {
	root := VSplit(Pane("x"), Pane("y"), Pane("z"))
	got := Solve(root, Rect{X: 0, Y: 0, W: 1, H: 2})
	total := got["x"].H + got["y"].H + got["z"].H
	if total != 2 {
		t.Fatalf("heights must sum to 2, got %d", total)
	}
	for _, id := range []string{"x", "y", "z"} {
		if got[id].H < 0 {
			t.Fatalf("%s height negative: %d", id, got[id].H)
		}
	}
}

func TestSolveNested(t *testing.T) {
	// runs on top (1/3), logs+result split below (50/50).
	root := VSplit(
		Flex(1, Pane("runs")),
		Flex(2, HSplit(Pane("logs"), Pane("result"))),
	)
	got := Solve(root, Rect{X: 0, Y: 0, W: 100, H: 30})
	if got["runs"] != (Rect{X: 0, Y: 0, W: 100, H: 10}) {
		t.Fatalf("runs = %+v", got["runs"])
	}
	if got["logs"] != (Rect{X: 0, Y: 10, W: 50, H: 20}) {
		t.Fatalf("logs = %+v", got["logs"])
	}
	if got["result"] != (Rect{X: 50, Y: 10, W: 50, H: 20}) {
		t.Fatalf("result = %+v", got["result"])
	}
}

func TestSolveInvariantsAcrossSizeMatrix(t *testing.T) {
	root := VSplit(
		Fixed(2, Pane("header")),
		Flex(2, HSplit(
			Pane("logs", Min(20, 0)),
			Pane("conversation", Min(30, 0)),
		)),
		Fixed(1, Pane("footer")),
	)
	sizes := []Rect{
		{W: 80, H: 24}, {W: 100, H: 30}, {W: 120, H: 40}, {W: 160, H: 50}, {W: 60, H: 20},
	}
	for _, area := range sizes {
		got := Solve(root, Rect{X: 0, Y: 0, W: area.W, H: area.H})
		for id, r := range got {
			if r.W < 0 || r.H < 0 {
				t.Fatalf("%dx%d: %s has negative size %+v", area.W, area.H, id, r)
			}
			if r.X < 0 || r.Y < 0 || r.X+r.W > area.W || r.Y+r.H > area.H {
				t.Fatalf("%dx%d: %s overflows area: %+v", area.W, area.H, id, r)
			}
		}
	}
}
