package terrain

import (
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// OwnedBoundaryEdges joins disjoint, already-unioned section bands. Only their
// horizontal ownership seams need splitting; material adjacency is irrelevant.
func OwnedBoundaryEdges(grid RockGrid) []RockEdge {
	seams := make(map[int64][]RockEdge)
	ordinary := make(map[[4]int64]RockEdge)
	for cell, c := range grid {
		for i, a := range c.Polygon {
			b := c.Polygon[(i+1)%len(c.Polygon)]
			if a.Sub(b).Len2() < 1e-20 {
				continue
			}
			e := RockEdge{A: a, B: b, Cell: cell}
			if math.Abs(a.Y-b.Y) < 1e-12 && math.Abs(a.Y-math.Round(a.Y)) < 1e-10 {
				key := int64(math.Round(a.Y))
				seams[key] = append(seams[key], e)
				continue
			}
			key := geom.EdgeKey(a, b)
			if old, ok := ordinary[key]; ok {
				if old.A.Sub(b).Len2() < 1e-18 {
					delete(ordinary, key)
				}
				continue
			}
			ordinary[key] = e
		}
	}
	var result []RockEdge
	for _, group := range seams {
		var xs []float64
		for _, e := range group {
			xs = append(xs, e.A.X, e.B.X)
		}
		sort.Float64s(xs)
		unique := xs[:0]
		for _, x := range xs {
			if len(unique) == 0 || x-unique[len(unique)-1] > 1e-10 {
				unique = append(unique, x)
			}
		}
		for i := 1; i < len(unique); i++ {
			lo, hi := unique[i-1], unique[i]
			mid := (lo + hi) * .5
			winding, owner := 0, 0
			for j, e := range group {
				if mid > min(e.A.X, e.B.X) && mid < max(e.A.X, e.B.X) {
					if e.B.X > e.A.X {
						winding++
					} else {
						winding--
					}
					owner = j
				}
			}
			if winding == 0 {
				continue
			}
			e := group[owner]
			a, b := geom.V{X: lo, Y: e.A.Y}, geom.V{X: hi, Y: e.A.Y}
			if winding < 0 {
				a, b = b, a
			}
			result = append(result, RockEdge{A: a, B: b, Cell: e.Cell})
		}
	}
	for _, e := range ordinary {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := geom.EdgeKey(result[i].A, result[i].B), geom.EdgeKey(result[j].A, result[j].B)
		for k := range a {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return false
	})
	return result
}
