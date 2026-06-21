package layout

// Direction is the axis along which a split arranges its children.
type Direction int

const (
	// Vertical stacks children top to bottom.
	Vertical Direction = iota
	// Horizontal places children left to right.
	Horizontal
)

// SizeMode controls how a node claims space within its parent split.
type SizeMode int

const (
	// ModeFlex shares leftover space proportional to Weight.
	ModeFlex SizeMode = iota
	// ModeFixed claims exactly Size cells along the parent's axis.
	ModeFixed
)

// Node is one element of a layout tree. A node is either a leaf (ID set, no
// Children) or a split (Children set). Construct nodes with the builders rather
// than literals so defaults (Visible, Weight) are set correctly.
type Node struct {
	ID       string
	Dir      Direction
	Children []*Node

	Mode    SizeMode
	Size    int
	Weight  int
	MinW    int
	MinH    int
	Visible bool
}

// IsLeaf reports whether n has no children.
func (n *Node) IsLeaf() bool {
	return len(n.Children) == 0
}
