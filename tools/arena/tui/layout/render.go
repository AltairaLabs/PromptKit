package layout

import "github.com/charmbracelet/lipgloss"

// RenderTree composes the rendered content of each leaf into the layout's
// shape. content maps a leaf ID to its already-rendered string. Vertical splits
// stack with JoinVertical(Left); horizontal splits sit side by side with
// JoinHorizontal(Top). Invisible nodes are skipped. Unlike the geometry files,
// this rendering layer may depend on lipgloss.
func RenderTree(root *Node, content map[string]string) string {
	if root == nil || !root.Visible {
		return ""
	}
	if root.IsLeaf() {
		return content[root.ID]
	}
	children := visibleChildren(root)
	parts := make([]string, 0, len(children))
	for _, c := range children {
		parts = append(parts, RenderTree(c, content))
	}
	if root.Dir == Vertical {
		return lipgloss.JoinVertical(lipgloss.Left, parts...)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}
