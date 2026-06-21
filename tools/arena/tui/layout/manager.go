package layout

// Manager owns a layout tree and its current area, and is the single place that
// mutates the layout at runtime (resize, collapse). Each mutation reflows once,
// so consumers read the resulting rects via Rect. Construct page trees with the
// builders and hand the root to NewManager.
type Manager struct {
	root  *Node
	area  Rect
	rects map[string]Rect
}

// NewManager returns a Manager for the given layout tree.
func NewManager(root *Node) *Manager {
	return &Manager{root: root, rects: map[string]Rect{}}
}

// Root returns the layout tree, for composition via RenderTree.
func (m *Manager) Root() *Node {
	return m.root
}

// SetArea sets the available area and reflows.
func (m *Manager) SetArea(r Rect) {
	m.area = r
	m.reflow()
}

// Rect returns the computed rectangle for the leaf with the given ID. The
// second result is false when the leaf is collapsed (absent from the layout).
func (m *Manager) Rect(id string) (Rect, bool) {
	r, ok := m.rects[id]
	return r, ok
}

// Grow adjusts the flex weight of the leaf with the given ID by delta (weight
// is clamped to a minimum of 1) and reflows. Growing a fixed-size leaf has no
// effect. Unknown IDs are ignored.
func (m *Manager) Grow(id string, delta int) {
	n := findNode(m.root, id)
	if n == nil {
		return
	}
	n.Weight += delta
	if n.Weight < 1 {
		n.Weight = 1
	}
	m.reflow()
}

// ToggleCollapse flips the visibility of the leaf with the given ID and
// reflows. Unknown IDs are ignored.
func (m *Manager) ToggleCollapse(id string) {
	n := findNode(m.root, id)
	if n == nil {
		return
	}
	n.Visible = !n.Visible
	m.reflow()
}

func (m *Manager) reflow() {
	m.rects = Solve(m.root, m.area)
}

// findNode returns the first node in the tree with the given ID, or nil.
func findNode(n *Node, id string) *Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if found := findNode(c, id); found != nil {
			return found
		}
	}
	return nil
}
