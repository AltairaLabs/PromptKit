// Package layout provides a pure-geometry layout engine for the Arena TUI.
// It computes rectangles for a tree of panes and contains no rendering code.
package layout

// Rect is a position and size in terminal cells.
type Rect struct {
	X, Y, W, H int
}
