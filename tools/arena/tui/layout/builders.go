package layout

// Option configures a Node during construction.
type Option func(*Node)

// Min sets the minimum width and height in cells.
func Min(w, h int) Option {
	return func(n *Node) {
		n.MinW = w
		n.MinH = h
	}
}

// Weight sets the flex weight (share of leftover space).
func Weight(w int) Option {
	return func(n *Node) { n.Weight = w }
}

// Pane returns a leaf node. It is flex with weight 1 and visible by default.
func Pane(id string, opts ...Option) *Node {
	n := &Node{ID: id, Mode: ModeFlex, Weight: 1, Visible: true}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// VSplit returns a vertical split whose children stack top to bottom.
func VSplit(children ...*Node) *Node {
	return &Node{Dir: Vertical, Children: children, Mode: ModeFlex, Weight: 1, Visible: true}
}

// HSplit returns a horizontal split whose children sit left to right.
func HSplit(children ...*Node) *Node {
	return &Node{Dir: Horizontal, Children: children, Mode: ModeFlex, Weight: 1, Visible: true}
}

// Fixed makes child claim exactly size cells along its parent's axis.
func Fixed(size int, child *Node) *Node {
	child.Mode = ModeFixed
	child.Size = size
	return child
}

// Flex makes child share leftover space proportional to weight.
func Flex(weight int, child *Node) *Node {
	child.Mode = ModeFlex
	child.Weight = weight
	return child
}

// Optional includes child in the layout only when visible is true.
func Optional(visible bool, child *Node) *Node {
	child.Visible = visible
	return child
}
