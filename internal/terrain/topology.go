package terrain

import (
	"math"
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
	// Appearance is bound during materialization; web fitting belongs to the
	// later decoration phase. Published snapshots remain immutable.
	Appearance   RockGrid
	SpiderWebs   []SpiderWeb
	webCorners   []webCorner
	websPrepared bool
	webMaxSize   float64 // cache keys for the prepared decoration
	webDensity   float64
}

// Neighbors returns immutable adjacency in Grid order. Callers must not modify it.
func (t *RockTopology) Neighbors() [][]int { return t.neighbors }

// NewRockTopology prepares the inset silhouette without decoration.
func NewRockTopology(grid RockGrid) *RockTopology {
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

// Merged faces can omit a collinear vertex retained by their neighbor. Make
// those junctions explicit before sampling heights or moving contour points,
// so both faces use the same edge intervals and move their shared endpoints.
func splitRockJunctions(grid RockGrid) RockGrid {
	neighbors := RockNeighbors(grid)
	out := append(RockGrid(nil), grid...)
	parallelFor(len(grid), func(i int) {
		var poly []geom.V
		for j, a := range grid[i].Polygon {
			b := grid[i].Polygon[(j+1)%len(grid[i].Polygon)]
			d := b.Sub(a)
			poly = append(poly, a)
			if d.Len2() < mergeTolerance*mergeTolerance {
				continue
			}
			var ts []float64
			for _, k := range neighbors[i] {
				for _, p := range grid[k].Polygon {
					t := p.Sub(a).Dot(d) / d.Len2()
					if t > 0 && t < 1 && p.Sub(a.Add(d.Mul(t))).Len() < mergeTolerance {
						ts = append(ts, t)
					}
				}
			}
			sort.Float64s(ts)
			for _, t := range ts {
				p := a.Add(d.Mul(t))
				if p.Sub(poly[len(poly)-1]).Len() > mergeTolerance && p.Sub(b).Len() > mergeTolerance {
					poly = append(poly, p)
				}
			}
		}
		out[i].Polygon = poly
	})
	return out
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
