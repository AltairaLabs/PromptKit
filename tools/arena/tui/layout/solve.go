package layout

// Solve computes the rectangle for every leaf in the tree rooted at root,
// laid out within area. Leaf rectangles are keyed by Node.ID. Invisible nodes
// are omitted. Solve is a pure function: same inputs always yield the same map.
func Solve(root *Node, area Rect) map[string]Rect {
	out := make(map[string]Rect)
	if root == nil || !root.Visible {
		return out
	}
	solve(root, area, out)
	return out
}

func solve(n *Node, area Rect, out map[string]Rect) {
	if n.IsLeaf() {
		if n.ID != "" {
			out[n.ID] = area
		}
		return
	}
	children := visibleChildren(n)
	if len(children) == 0 {
		return
	}
	rects := distribute(n.Dir, children, area)
	for i, child := range children {
		solve(child, rects[i], out)
	}
}

func visibleChildren(n *Node) []*Node {
	vis := make([]*Node, 0, len(n.Children))
	for _, c := range n.Children {
		if c.Visible {
			vis = append(vis, c)
		}
	}
	return vis
}

// distribute lays out children along dir within area, returning one Rect per
// child (aligned with the input slice).
func distribute(dir Direction, children []*Node, area Rect) []Rect {
	axisLen := area.H
	if dir == Horizontal {
		axisLen = area.W
	}
	sizes := axisSizes(dir, children, axisLen)

	rects := make([]Rect, len(children))
	pos := area.Y
	if dir == Horizontal {
		pos = area.X
	}
	for i, sz := range sizes {
		if dir == Vertical {
			rects[i] = Rect{X: area.X, Y: pos, W: area.W, H: sz}
		} else {
			rects[i] = Rect{X: pos, Y: area.Y, W: sz, H: area.H}
		}
		pos += sz
	}
	return rects
}

// axisSizes returns the cell length along dir for each child. Fixed children
// take their Size. Flex children share the remaining pool by weight, but a flex
// child whose weighted share would fall below its minimum is clamped up to that
// minimum, and the deficit comes out of the pool available to its siblings
// (water-filling). If fixed sizes plus flex minimums already overflow the
// available length, everything shrinks proportionally so sizes stay
// non-negative and sum to axisLen. Rounding is deterministic (earliest children
// absorb remainders), keeping output golden-stable.
func axisSizes(dir Direction, children []*Node, axisLen int) []int {
	n := len(children)
	sizes := make([]int, n)
	if axisLen <= 0 {
		return sizes
	}

	base := make([]int, n)
	baseTotal := 0
	fixedTotal := 0
	for i, c := range children {
		if c.Mode == ModeFixed {
			base[i] = c.Size
			fixedTotal += c.Size
		} else {
			base[i] = minAlong(dir, c)
		}
		baseTotal += base[i]
	}

	// Not enough room even for fixed sizes plus flex minimums: shrink all.
	if baseTotal >= axisLen {
		return shrinkProportional(base, axisLen)
	}

	// Fixed children take exactly their size.
	for i, c := range children {
		if c.Mode == ModeFixed {
			sizes[i] = c.Size
		}
	}

	// Water-fill the flex pool: repeatedly clamp the first flex child whose
	// weighted share is below its minimum, then redistribute the rest.
	pool := axisLen - fixedTotal
	settled := make([]bool, n)
	for {
		remaining := pool
		remainingWeight := 0
		for i, c := range children {
			if c.Mode != ModeFlex {
				continue
			}
			if settled[i] {
				remaining -= sizes[i]
			} else {
				remainingWeight += flexWeight(c)
			}
		}
		clamped := false
		for i, c := range children {
			if c.Mode != ModeFlex || settled[i] {
				continue
			}
			if remaining*flexWeight(c)/remainingWeight < minAlong(dir, c) {
				sizes[i] = minAlong(dir, c)
				settled[i] = true
				clamped = true
				break
			}
		}
		if !clamped {
			distributeFlex(children, sizes, settled, remaining, remainingWeight)
			break
		}
	}
	return sizes
}

// distributeFlex splits pool among the unsettled flex children by weight, with
// the rounding remainder going to the earliest children (deterministic).
func distributeFlex(children []*Node, sizes []int, settled []bool, pool, totalWeight int) {
	if totalWeight <= 0 {
		return
	}
	allocated := 0
	idxs := make([]int, 0, len(children))
	for i, c := range children {
		if c.Mode != ModeFlex || settled[i] {
			continue
		}
		share := pool * flexWeight(c) / totalWeight
		sizes[i] = share
		allocated += share
		idxs = append(idxs, i)
	}
	for k := 0; k < pool-allocated && k < len(idxs); k++ {
		sizes[idxs[k]]++
	}
}

// flexWeight returns the effective weight of a flex node (at least 1).
func flexWeight(n *Node) int {
	if n.Weight < 1 {
		return 1
	}
	return n.Weight
}

func minAlong(dir Direction, n *Node) int {
	if dir == Vertical {
		return n.MinH
	}
	return n.MinW
}

// shrinkProportional scales want down to sum to target, deterministically.
func shrinkProportional(want []int, target int) []int {
	out := make([]int, len(want))
	total := 0
	for _, w := range want {
		total += w
	}
	if total <= 0 || target <= 0 {
		return out
	}
	allocated := 0
	for i, w := range want {
		out[i] = w * target / total
		allocated += out[i]
	}
	for i := 0; i < len(out) && allocated < target; i++ {
		out[i]++
		allocated++
	}
	return out
}
