package inspect

import (
	"strings"
	"testing"
)

func TestComputeColumns(t *testing.T) {
	cases := map[int]int{
		0:   1, // CLI / unsized
		80:  1,
		120: 2,
		160: 3,
		400: 3, // capped at maxColumns
		-1:  1,
	}
	for width, want := range cases {
		if got := computeColumns(width); got != want {
			t.Errorf("computeColumns(%d) = %d, want %d", width, got, want)
		}
	}
}

func TestColumnWidth(t *testing.T) {
	// Single column falls back to the full content width.
	if got := columnWidth(120, 1); got != effectiveBoxWidth(120) {
		t.Errorf("columnWidth(120,1) = %d, want %d", got, effectiveBoxWidth(120))
	}
	// Two columns each get roughly half, never below the minimum.
	if got := columnWidth(120, 2); got < minBoxWidth || got > 120 {
		t.Errorf("columnWidth(120,2) = %d, out of range", got)
	}
	if got := columnWidth(20, 3); got != minBoxWidth {
		t.Errorf("columnWidth(20,3) = %d, want floor %d", got, minBoxWidth)
	}
}

// TestRenderText_MultiColumn verifies item boxes flow into a grid on a wide
// terminal: a row carries two box borders, and nothing exceeds the width.
func TestRenderText_MultiColumn(t *testing.T) {
	data := &InspectionData{
		ConfigFile: "x.yaml",
		Scenarios: []ScenarioInspectData{
			{ID: "alpha", TaskType: "chat", TurnCount: 1},
			{ID: "bravo", TaskType: "chat", TurnCount: 2},
		},
	}
	out := RenderText(data, RenderOptions{Width: 130})

	for _, ln := range strings.Split(out, "\n") {
		if n := len([]rune(ln)); n > 130 {
			t.Fatalf("line width %d exceeds 130", n)
		}
	}
	// At 130 cols (2-wide grid) a scenario row should pair two box tops.
	gridded := false
	for _, ln := range strings.Split(out, "\n") {
		if strings.Count(ln, "╭") >= 2 {
			gridded = true
			break
		}
	}
	if !gridded {
		t.Fatal("expected two boxes side by side in the scenarios grid")
	}
}
