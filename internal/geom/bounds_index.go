package geom

import (
	"cmp"
	"slices"
	"unsafe"
)

// BoundsIndex is an immutable hierarchy over conservative object bounds.
// Leaves retain caller IDs; query order is deterministic but not ID order.
type BoundsIndex struct {
	nodes []boundsNode
	ids   []int
}

type Bounds struct{ Min, Max V }
type boundsNode struct {
	Bounds
	left, right int
	first, end  int
}

// RetainedBytes reports the index's CPU storage, excluding allocator overhead.
func (t *BoundsIndex) RetainedBytes() int {
	if t == nil {
		return 0
	}
	return int(unsafe.Sizeof(*t)) + cap(t.nodes)*int(unsafe.Sizeof(boundsNode{})) + cap(t.ids)*int(unsafe.Sizeof(int(0)))
}

func (b Bounds) Overlaps(other Bounds) bool {
	return b.Max.X >= other.Min.X && b.Min.X <= other.Max.X && b.Max.Y >= other.Min.Y && b.Min.Y <= other.Max.Y
}

func (b Bounds) Distance2(p V) float64 {
	dx := max(0, b.Min.X-p.X, p.X-b.Max.X)
	dy := max(0, b.Min.Y-p.Y, p.Y-b.Max.Y)
	return dx*dx + dy*dy
}

func NewBoundsIndex(bounds []Bounds) *BoundsIndex {
	t := &BoundsIndex{ids: make([]int, len(bounds))}
	for i := range t.ids {
		t.ids[i] = i
	}
	var build func(int, int) int
	build = func(first, end int) int {
		b := bounds[t.ids[first]]
		for _, id := range t.ids[first+1 : end] {
			b.Min = V{min(b.Min.X, bounds[id].Min.X), min(b.Min.Y, bounds[id].Min.Y)}
			b.Max = V{max(b.Max.X, bounds[id].Max.X), max(b.Max.Y, bounds[id].Max.Y)}
		}
		i := len(t.nodes)
		t.nodes = append(t.nodes, boundsNode{Bounds: b, first: first, end: end, left: -1})
		if end-first > 8 {
			x := b.Max.X-b.Min.X >= b.Max.Y-b.Min.Y
			slices.SortFunc(t.ids[first:end], func(ia, ic int) int {
				va, vc := bounds[ia].Min.Y+bounds[ia].Max.Y, bounds[ic].Min.Y+bounds[ic].Max.Y
				if x {
					va, vc = bounds[ia].Min.X+bounds[ia].Max.X, bounds[ic].Min.X+bounds[ic].Max.X
				}
				if va == vc {
					return cmp.Compare(ia, ic)
				}
				return cmp.Compare(va, vc)
			})
			mid := first + (end-first)/2
			left, right := build(first, mid), build(mid, end)
			t.nodes[i].left, t.nodes[i].right = left, right
		}
		return i
	}
	if len(bounds) > 0 {
		build(0, len(bounds))
	}
	return t
}

// Query stops when visit returns false. It allocates no scratch storage.
func (t *BoundsIndex) Query(bounds Bounds, visit func(int) bool) {
	if t == nil || len(t.nodes) == 0 {
		return
	}
	var walk func(int) bool
	walk = func(i int) bool {
		n := t.nodes[i]
		if !n.Overlaps(bounds) {
			return true
		}
		if n.left >= 0 {
			return walk(n.left) && walk(n.right)
		}
		for _, id := range t.ids[n.first:n.end] {
			if !visit(id) {
				return false
			}
		}
		return true
	}
	walk(0)
}

// Nearest visits leaves in increasing lower-bound distance. The visitor
// returns the current exact best distance so subsequent branches can prune.
// Equal bounds are visited: a caller can preserve original-order tie rules.
func (t *BoundsIndex) Nearest(p V, best2 float64, visit func(int, float64) float64) float64 {
	if t == nil || len(t.nodes) == 0 {
		return best2
	}
	var walk func(int)
	walk = func(i int) {
		n := t.nodes[i]
		if n.Distance2(p) > best2 {
			return
		}
		if n.left >= 0 {
			a, b := n.left, n.right
			if t.nodes[a].Distance2(p) > t.nodes[b].Distance2(p) {
				a, b = b, a
			}
			walk(a)
			walk(b)
			return
		}
		for _, id := range t.ids[n.first:n.end] {
			best2 = visit(id, best2)
		}
	}
	walk(0)
	return best2
}
