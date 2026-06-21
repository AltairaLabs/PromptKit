package layout

import "testing"

func newTestManager() *Manager {
	return NewManager(VSplit(
		Flex(1, Pane("runs", Min(0, 5))),
		Flex(2, HSplit(
			Pane("logs", Min(10, 0)),
			Pane("result", Min(10, 0)),
		)),
	))
}

func TestManagerSetAreaAndRect(t *testing.T) {
	m := newTestManager()
	m.SetArea(Rect{W: 100, H: 30})

	runs, ok := m.Rect("runs")
	if !ok {
		t.Fatal("runs should have a rect")
	}
	if runs.W != 100 {
		t.Fatalf("runs width = %d, want 100", runs.W)
	}
	logs, _ := m.Rect("logs")
	result, _ := m.Rect("result")
	if logs.W+result.W != 100 {
		t.Fatalf("logs+result width = %d, want 100", logs.W+result.W)
	}
}

func TestManagerGrowChangesProportions(t *testing.T) {
	m := newTestManager()
	m.SetArea(Rect{W: 100, H: 30})
	before, _ := m.Rect("runs")

	m.Grow("runs", 2) // weight 1 -> 3

	after, _ := m.Rect("runs")
	if after.H <= before.H {
		t.Fatalf("runs height should grow: before=%d after=%d", before.H, after.H)
	}
}

func TestManagerGrowClampsToMinWeight(t *testing.T) {
	m := newTestManager()
	m.SetArea(Rect{W: 100, H: 30})

	m.Grow("runs", -100) // would drive weight negative; clamps to 1

	runs, ok := m.Rect("runs")
	if !ok || runs.H <= 0 {
		t.Fatalf("runs should still have positive height, got %+v ok=%v", runs, ok)
	}
}

func TestManagerToggleCollapseHidesAndRestores(t *testing.T) {
	m := newTestManager()
	m.SetArea(Rect{W: 100, H: 30})

	m.ToggleCollapse("logs")
	if _, ok := m.Rect("logs"); ok {
		t.Fatal("logs should be absent after collapse")
	}
	result, _ := m.Rect("result")
	if result.W != 100 {
		t.Fatalf("result should fill the bottom row after logs collapse, got W=%d", result.W)
	}

	m.ToggleCollapse("logs")
	if _, ok := m.Rect("logs"); !ok {
		t.Fatal("logs should reappear after restore")
	}
}

func TestManagerUnknownIDNoop(t *testing.T) {
	m := newTestManager()
	m.SetArea(Rect{W: 100, H: 30})
	m.Grow("nope", 5)        // must not panic
	m.ToggleCollapse("nope") // must not panic
	if _, ok := m.Rect("runs"); !ok {
		t.Fatal("layout should be intact after no-op mutations")
	}
}
