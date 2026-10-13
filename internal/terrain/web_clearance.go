package terrain

import (
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"sort"
)

type webFaceBounds struct{ lo, hi geom.V }

type webClearance struct {
	grid      RockGrid
	bounds    []webFaceBounds
	index     *geom.BoundsIndex
	edgeIndex *geom.BoundsIndex
}

func newWebClearance(grid RockGrid) webClearance {
	c := webClearance{grid: grid, bounds: make([]webFaceBounds, len(grid))}
	for i, face := range grid {
		if len(face.Polygon) > 0 {
			c.bounds[i].lo, c.bounds[i].hi = geom.PolygonBounds(face.Polygon)
		}
	}
	bounds := make([]geom.Bounds, len(c.bounds))
	for i, b := range c.bounds {
		bounds[i] = geom.Bounds{Min: b.lo, Max: b.hi}
	}
	c.index = geom.NewBoundsIndex(bounds)
	return c
}

func (c webClearance) nearby(lo, hi geom.V, reuse []int) []int {
	reuse = reuse[:0]
	c.index.Query(geom.Bounds{Min: lo, Max: hi}, func(i int) bool {
		b := c.bounds[i]
		if c.grid[i].Raised && c.grid[i].Color.A != 0 &&
			b.hi.X >= lo.X && b.lo.X <= hi.X && b.hi.Y >= lo.Y && b.lo.Y <= hi.Y {
			reuse = append(reuse, i)
		}
		return true
	})
	return reuse
}

// Check complete segments, not just spaced samples. Trimming only a numerical
// epsilon permits intended rock contacts but still catches thin obstructions.
func (c webClearance) segmentClear(a, b geom.V, faces []int) bool {
	d := b.Sub(a)
	length := d.Len()
	if length < 1e-9 {
		return false
	}
	epsilon := min(1e-8/length, .001)
	a, b = a.Add(d.Mul(epsilon)), b.Sub(d.Mul(epsilon))
	lo := geom.V{X: min(a.X, b.X), Y: min(a.Y, b.Y)}
	hi := geom.V{X: max(a.X, b.X), Y: max(a.Y, b.Y)}
	for _, i := range faces {
		bounds := c.bounds[i]
		if bounds.hi.X < lo.X || bounds.lo.X > hi.X || bounds.hi.Y < lo.Y || bounds.lo.Y > hi.Y {
			continue
		}
		poly := c.grid[i].Polygon
		if geom.InsidePolygon(a, poly) || geom.InsidePolygon(b, poly) || geom.InsidePolygon(geom.LerpVector(a, b, .5), poly) {
			return false
		}
		for j, p := range poly {
			q := poly[(j+1)%len(poly)]
			if max(p.X, q.X) < lo.X || min(p.X, q.X) > hi.X || max(p.Y, q.Y) < lo.Y || min(p.Y, q.Y) > hi.Y {
				continue
			}
			if geom.SegmentDistanceSquared(a, b, p, q) < 1e-22 {
				return false
			}
		}
	}
	return true
}

func (c webClearance) networkClear(n *WebNetwork, faces []int) bool {
	for _, t := range n.Threads {
		if !c.segmentClear(n.Nodes[t.A], n.Nodes[t.B], faces) {
			return false
		}
	}
	return true
}

func extraWebAnchor(w SpiderWeb, reach float64, edges []RockEdge, clearance webClearance, faces []int) (geom.V, int, bool) {
	chord := w.B.Sub(w.A)
	probe := geom.LerpVector(w.A, w.B, .5).Sub(chord.Perp().Norm().Mul(reach * .45))
	center := w.Corner.Add(w.A.Sub(w.Corner).Add(w.B.Sub(w.Corner)).Mul(.3))
	best := reach * reach * .50
	var anchor geom.V
	cell := -1
	var candidates []int
	if clearance.edgeIndex != nil {
		clearance.edgeIndex.Query(geom.Bounds{Min: probe.Sub(geom.V{X: reach, Y: reach}), Max: probe.Add(geom.V{X: reach, Y: reach})}, func(i int) bool { candidates = append(candidates, i); return true })
		sort.Ints(candidates)
	} else {
		for i := range edges {
			candidates = append(candidates, i)
		}
	}
	for _, i := range candidates {
		edge := edges[i]
		d := edge.B.Sub(edge.A)
		if d.Len2() < 1e-20 {
			continue
		}
		// Reject distant edges before projection or polygon queries.
		if max(edge.A.X, edge.B.X) < probe.X-reach || min(edge.A.X, edge.B.X) > probe.X+reach ||
			max(edge.A.Y, edge.B.Y) < probe.Y-reach || min(edge.A.Y, edge.B.Y) > probe.Y+reach {
			continue
		}
		p := edge.A.Add(d.Mul(geom.Clamp(probe.Sub(edge.A).Dot(d)/d.Len2(), .08, .92)))
		if p.Sub(w.Corner).Len() > .25 || geom.Cross(chord, p.Sub(w.A)) >= -reach*reach*.08 {
			continue
		}
		distance := p.Sub(probe).Len2()
		if distance >= best || !clearance.segmentClear(center, p, faces) {
			continue
		}
		best, anchor, cell = distance, p, edge.Cell
	}
	return anchor, cell, cell >= 0
}

// Reject overlapping capture frames, while allowing neighboring attachment
// strands to share a recess without filling the whole opening with silk.
func webOverlaps(w SpiderWeb, others []SpiderWeb) bool {
	frame := webFramePoints(w.Network)
	lo, hi := geom.PolygonBounds(frame)
	for _, other := range others {
		otherFrame := webFramePoints(other.Network)
		olo, ohi := geom.PolygonBounds(otherFrame)
		if hi.X < olo.X || lo.X > ohi.X || hi.Y < olo.Y || lo.Y > ohi.Y {
			continue
		}
		for _, p := range frame {
			if geom.InsidePolygon(p, otherFrame) {
				return true
			}
		}
		for _, p := range otherFrame {
			if geom.InsidePolygon(p, frame) {
				return true
			}
		}
		for i, a := range frame {
			for j, b := range otherFrame {
				if geom.SegmentDistanceSquared(a, frame[(i+1)%len(frame)], b, otherFrame[(j+1)%len(otherFrame)]) < 1e-20 {
					return true
				}
			}
		}
	}
	return false
}

func webFramePoints(n *WebNetwork) []geom.V {
	frame := make([]geom.V, len(n.Frame))
	for i, node := range n.Frame {
		frame[i] = n.Nodes[node]
	}
	return frame
}
