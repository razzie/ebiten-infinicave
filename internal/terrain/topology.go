package terrain

import (
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type RockEdge struct {
	A, B geom.V
	Cell int
}

// The final inset grid is immutable through mesh/collision preparation. Share
// its adjacency and boundary instead of rediscovering partial edges per layer.
type RockTopology struct {
	Grid        RockGrid
	neighbors   [][]int
	Boundary    []RockEdge
	allBoundary []RockEdge // includes generation-window edges for collision
	Cuts        []RockCut
	Top         float64
}

func newRockTopology(grid RockGrid) *RockTopology {
	t := &RockTopology{Grid: InsetForegroundGrid(grid)}
	t.neighbors = RockNeighbors(t.Grid)
	t.prepareBoundary()
	return t
}

func (t *RockTopology) prepareBoundary() {
	t.allBoundary = RockBoundaryEdges(t.Grid, t.neighbors, true)
	for _, edge := range t.allBoundary {
		if !rockWindowEdge(edge.A, edge.B) {
			t.Boundary = append(t.Boundary, edge)
		}
	}
}

func rockWindowEdge(a, b geom.V) bool {
	return (a.X == b.X && (a.X == 0 || a.X == generationWidth)) ||
		(a.Y == b.Y && (a.Y == GenerationMinY || a.Y == generationMaxY))
}

// Subdivide partial shared edges before cancellation, so merged guide faces
// never grow a side wall through a neighboring rock. Corner contacts survive.
func ExposedRockEdges(grid RockGrid) []RockEdge {
	return RockBoundaryEdges(grid, RockNeighbors(grid), false)
}

func RockBoundaryEdges(grid RockGrid, neighbors [][]int, includeWindow bool) []RockEdge {
	type entry struct {
		edge  RockEdge
		count int
	}
	edges := make(map[[4]int64]entry)
	for i, c := range grid {
		for j, a := range c.Polygon {
			b := c.Polygon[(j+1)%len(c.Polygon)]
			d := b.Sub(a)
			if d.Len2() < 1e-18 {
				continue
			}
			// Generation-window cuts have no physical thickness.
			if !includeWindow && rockWindowEdge(a, b) {
				continue
			}
			ts := []float64{0, 1}
			for _, k := range neighbors[i] {
				for _, p := range grid[k].Polygon {
					t := p.Sub(a).Dot(d) / d.Len2()
					if t > 0 && t < 1 && p.Sub(a.Add(d.Mul(t))).Len() < mergeTolerance {
						ts = append(ts, t)
					}
				}
			}
			sort.Float64s(ts)
			for k := 1; k < len(ts); k++ {
				p, q := a.Add(d.Mul(ts[k-1])), a.Add(d.Mul(ts[k]))
				if p.Sub(q).Len() < mergeTolerance {
					continue
				}
				key := geom.EdgeKey(p, q)
				e := edges[key]
				e.count++
				e.edge = RockEdge{p, q, i}
				edges[key] = e
			}
		}
	}
	keys := make([][4]int64, 0, len(edges))
	for key, e := range edges {
		if e.count == 1 {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		for k := 0; k < 4; k++ {
			if keys[i][k] != keys[j][k] {
				return keys[i][k] < keys[j][k]
			}
		}
		return false
	})
	out := make([]RockEdge, 0, len(keys))
	for _, key := range keys {
		out = append(out, edges[key].edge)
	}
	return out
}
