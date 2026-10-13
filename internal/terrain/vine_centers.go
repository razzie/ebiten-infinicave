package terrain

import "github.com/razzie/ebiten-infinicave/internal/geom"

// Join cell centers through the midpoint of each shared edge.
// Cells across a gap never become neighbors, even when their centers are close.
func newVineCenterGraph(grid RockGrid) *vineEdgeGraph {
	g := &vineEdgeGraph{}
	for _, cell := range grid {
		if len(cell.Polygon) < 3 {
			continue
		}
		center := cell.Center
		if !geom.InsidePolygon(center, cell.Polygon) {
			center = geom.PolygonCenter(cell.Polygon)
		}
		g.points = append(g.points, center)
		g.links = append(g.links, nil)
	}
	g.centerCount = len(g.points)
	owners := make(map[[4]int64]int)
	seen := make(map[[2]int]bool)
	node := -1
	for _, cell := range grid {
		if len(cell.Polygon) < 3 {
			continue
		}
		node++
		for j, a := range cell.Polygon {
			b := cell.Polygon[(j+1)%len(cell.Polygon)]
			if b.Sub(a).Len2() < 1e-18 {
				continue
			}
			key := geom.EdgeKey(a, b)
			other, shared := owners[key]
			if !shared {
				owners[key] = node
				continue
			}
			pair := [2]int{other, node}
			if seen[pair] || g.points[node].Sub(g.points[other]).Len2() < 1e-18 {
				continue
			}
			seen[pair] = true
			midpoint := len(g.points)
			g.points = append(g.points, geom.LerpVector(a, b, .5))
			g.links = append(g.links, nil)
			for _, center := range []int{other, node} {
				edge := len(g.edges)
				g.edges = append(g.edges, vineEdge{center, midpoint})
				g.links[center] = append(g.links[center], edge)
				g.links[midpoint] = append(g.links[midpoint], edge)
			}
		}
	}
	return g
}
