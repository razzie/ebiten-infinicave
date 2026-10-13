package terrain

import (
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// RockShape is a connected structural polygon set. Loops carry signed winding
// (outer positive, holes negative); appearance facets are stored separately.
type RockShape struct {
	Loops    [][]geom.V
	Min, Max geom.V
	edges    []RockEdge
	index    *geom.BoundsIndex
}

func newRockShape(loops [][]geom.V) RockShape {
	s := RockShape{Loops: loops}
	if len(loops) == 0 {
		return s
	}
	s.Min, s.Max = geom.PolygonBounds(loops[0])
	for _, p := range loops {
		lo, hi := geom.PolygonBounds(p)
		s.Min = geom.V{X: min(s.Min.X, lo.X), Y: min(s.Min.Y, lo.Y)}
		s.Max = geom.V{X: max(s.Max.X, hi.X), Y: max(s.Max.Y, hi.Y)}
		for j, a := range p {
			s.edges = append(s.edges, RockEdge{A: a, B: p[(j+1)%len(p)]})
		}
	}
	s.index = geom.NewBoundsIndex(edgeBounds(s.edges, 0))
	return s
}

func (s RockShape) Contains(p geom.V) bool {
	if p.X < s.Min.X || p.X > s.Max.X || p.Y < s.Min.Y || p.Y > s.Max.Y {
		return false
	}
	winding := 0
	for _, poly := range s.Loops {
		if geom.InsidePolygon(p, poly) {
			if geom.PolygonArea(poly) > 0 {
				winding++
			} else {
				winding--
			}
		}
	}
	return winding > 0
}

// SplitLoops associates each negative loop with its smallest containing outer
// loop. Islands remain separate components even when nested inside a hole.
func SplitLoops(loops [][]geom.V) []RockShape {
	var shapes []RockShape
	for _, p := range loops {
		if geom.PolygonArea(p) > 1e-15 {
			shapes = append(shapes, newRockShape([][]geom.V{p}))
		}
	}
	for _, p := range loops {
		if geom.PolygonArea(p) >= -1e-15 {
			continue
		}
		owner, area := -1, math.Inf(1)
		for i, s := range shapes {
			if a := geom.PolygonArea(s.Loops[0]); a < area && geom.InsidePolygon(p[0], s.Loops[0]) {
				owner, area = i, a
			}
		}
		if owner >= 0 {
			shapes[owner].Loops = append(shapes[owner].Loops, p)
		}
	}
	for i := range shapes {
		shapes[i] = newRockShape(shapes[i].Loops)
	}
	return shapes
}

type shapeSplit struct {
	t float64
	p geom.V
}

func segmentSplits(a, b, c, d geom.V) (first, second []shapeSplit) {
	u, v := b.Sub(a), d.Sub(c)
	cross := geom.Cross(u, v)
	if math.Abs(cross) > 1e-18 {
		t, s := geom.Cross(c.Sub(a), v)/cross, geom.Cross(c.Sub(a), u)/cross
		if t >= -1e-12 && t <= 1+1e-12 && s >= -1e-12 && s <= 1+1e-12 {
			t, s = geom.Clamp(t, 0, 1), geom.Clamp(s, 0, 1)
			p := a.Add(u.Mul(t))
			// Prefer exact existing endpoints at contacts. Both edges use the
			// same intersection coordinate, avoiding independent rounding.
			if t < 1e-12 {
				p = a
			}
			if t > 1-1e-12 {
				p = b
			}
			if s < 1e-12 {
				p = c
			}
			if s > 1-1e-12 {
				p = d
			}
			return []shapeSplit{{t, p}}, []shapeSplit{{s, p}}
		}
		return
	}
	if u.Len2() < 1e-24 || v.Len2() < 1e-24 || math.Abs(geom.Cross(c.Sub(a), u)) > 1e-14 {
		return
	}
	for _, p := range []geom.V{a, b, c, d} {
		t, s := p.Sub(a).Dot(u)/u.Len2(), p.Sub(c).Dot(v)/v.Len2()
		if t >= -1e-12 && t <= 1+1e-12 && s >= -1e-12 && s <= 1+1e-12 {
			first = append(first, shapeSplit{geom.Clamp(t, 0, 1), p})
			second = append(second, shapeSplit{geom.Clamp(s, 0, 1), p})
		}
	}
	return
}

// DifferenceShape subtracts a convex cut directly from union boundaries. Work
// depends on boundary segments, not on the number of shaded Voronoi facets.
// It handles enclosed holes, concavity, collinear contacts, and disconnections.
func DifferenceShape(shape RockShape, hole []geom.V) ([]RockShape, bool) {
	if len(hole) < 3 || len(shape.Loops) == 0 {
		return []RockShape{shape}, false
	}
	lo, hi := geom.PolygonBounds(hole)
	if !((geom.Bounds{Min: shape.Min, Max: shape.Max}).Overlaps(geom.Bounds{Min: lo, Max: hi})) {
		return []RockShape{shape}, false
	}
	splits := make([][]shapeSplit, len(shape.edges))
	cutSplits := make([][]shapeSplit, len(hole))
	for j, c := range hole {
		d := hole[(j+1)%len(hole)]
		shape.index.Query(geom.Bounds{Min: geom.V{X: min(c.X, d.X) - 1e-12, Y: min(c.Y, d.Y) - 1e-12}, Max: geom.V{X: max(c.X, d.X) + 1e-12, Y: max(c.Y, d.Y) + 1e-12}}, func(i int) bool {
			e := shape.edges[i]
			a, b := segmentSplits(e.A, e.B, c, d)
			splits[i] = append(splits[i], a...)
			cutSplits[j] = append(cutSplits[j], b...)
			return true
		})
	}
	var edges []RockEdge
	emitSpan := func(p, q geom.V, source bool, tag int) {
		if q.Sub(p).Len2() < 1e-20 {
			return
		}
		mid := geom.LerpVector(p, q, .5)
		if source {
			if mid.X >= lo.X && mid.X <= hi.X && mid.Y >= lo.Y && mid.Y <= hi.Y {
				if geom.InsidePolygon(mid, hole) {
					return
				}
				if onPolygonBoundary(mid, hole) && insideConvex(mid.Add(q.Sub(p).Norm().Perp().Mul(min(1e-8, q.Sub(p).Len()*.001))), hole) {
					return
				}
			}
			edges = append(edges, RockEdge{A: p, B: q, Cell: tag})
		} else if shape.Contains(mid) || (onShapeBoundary(mid, shape) && shape.Contains(mid.Add(p.Sub(q).Norm().Perp().Mul(min(1e-8, q.Sub(p).Len()*.001))))) {
			edges = append(edges, RockEdge{A: q, B: p, Cell: -1})
		}
	}
	emit := func(a, b geom.V, parts []shapeSplit, source bool, tag int) {
		if len(parts) == 0 {
			emitSpan(a, b, source, tag)
			return
		}
		parts = append(parts, shapeSplit{0, a}, shapeSplit{1, b})
		sort.Slice(parts, func(i, j int) bool { return parts[i].t < parts[j].t })
		for j := 1; j < len(parts); j++ {
			emitSpan(parts[j-1].p, parts[j].p, source, tag)
		}
	}
	for i, e := range shape.edges {
		emit(e.A, e.B, splits[i], true, e.Cell)
	}
	for j, p := range hole {
		emit(p, hole[(j+1)%len(hole)], cutSplits[j], false, -1)
	}
	// Remove duplicate coincident boundary spans. Opposite directed copies
	// cancel at touching cuts; equal directed copies contribute just once.
	unique := make(map[[4]int64]int, len(edges))
	var clean []RockEdge
	for _, e := range edges {
		k := geom.EdgeKey(e.A, e.B)
		if i, ok := unique[k]; ok {
			if clean[i].A.Sub(e.B).Len2() < 1e-18 {
				clean[i].A = clean[i].B
			}
			continue
		}
		unique[k] = len(clean)
		clean = append(clean, e)
	}
	edges = clean[:0]
	for _, e := range clean {
		if e.A.Sub(e.B).Len2() > 1e-20 {
			edges = append(edges, e)
		}
	}
	loops, ok := TraceCollisionLoops(edges)
	if !ok {
		// Degenerate contours use the existing exact face subtraction. This
		// is deliberately isolated from the common boundary-only fast path.
		return differenceShapeFallback(shape, hole)
	}
	oldArea, newArea := 0.0, 0.0
	for _, p := range shape.Loops {
		oldArea += geom.PolygonArea(p)
	}
	for _, p := range loops {
		newArea += geom.PolygonArea(p)
	}
	if oldArea-newArea <= 1e-15 {
		return []RockShape{shape}, false
	}
	parts := SplitLoops(loops)
	tags := make(map[[4]int64]int, len(edges))
	for _, e := range edges {
		tags[geom.EdgeKey(e.A, e.B)] = e.Cell
	}
	for i := range parts {
		for j := range parts[i].edges {
			parts[i].edges[j].Cell = tags[geom.EdgeKey(parts[i].edges[j].A, parts[i].edges[j].B)]
		}
	}
	return parts, true
}

func insideConvex(p geom.V, poly []geom.V) bool {
	for i, a := range poly {
		if geom.Cross(poly[(i+1)%len(poly)].Sub(a), p.Sub(a)) < -1e-14 {
			return false
		}
	}
	return true
}
func onPolygonBoundary(p geom.V, poly []geom.V) bool {
	for i, a := range poly {
		if geom.SegmentDistanceSquared(p, p, a, poly[(i+1)%len(poly)]) < 1e-20 {
			return true
		}
	}
	return false
}
func onShapeBoundary(p geom.V, s RockShape) bool {
	for _, poly := range s.Loops {
		if onPolygonBoundary(p, poly) {
			return true
		}
	}
	return false
}

func differenceShapeFallback(s RockShape, hole []geom.V) ([]RockShape, bool) {
	grid := shapeTrapezoids(s)
	cut := RockCut{Poly: hole}
	cut.Min, cut.Max = geom.PolygonBounds(hole)
	carved, _, changed := cut.grid(grid, 0)
	if !changed {
		return []RockShape{s}, false
	}
	edges := RockBoundaryEdges(carved, RockNeighbors(carved), true)
	loops, ok := TraceCollisionLoops(edges)
	if ok {
		return SplitLoops(loops), true
	}
	// Keep an exact disjoint decomposition if a zero-width contact cannot be
	// traced. Shared edges still connect the pieces when queries assemble them.
	var shapes []RockShape
	for _, c := range carved {
		shapes = append(shapes, newRockShape([][]geom.V{c.Polygon}))
	}
	return shapes, true
}

// Scanline trapezoids provide an exact fallback decomposition with holes.
// Each slab ends at a source vertex, so crossing order is constant inside it.
func shapeTrapezoids(s RockShape) RockGrid {
	var ys []float64
	for _, e := range s.edges {
		ys = append(ys, e.A.Y)
	}
	sort.Float64s(ys)
	unique := ys[:0]
	for _, y := range ys {
		if len(unique) == 0 || y-unique[len(unique)-1] > 1e-12 {
			unique = append(unique, y)
		}
	}
	type crossing struct {
		x    float64
		edge RockEdge
	}
	var crossings []crossing
	var grid RockGrid
	xAt := func(e RockEdge, y float64) float64 { return e.A.X + (y-e.A.Y)*(e.B.X-e.A.X)/(e.B.Y-e.A.Y) }
	for k := 1; k < len(unique); k++ {
		y0, y1 := unique[k-1], unique[k]
		mid := (y0 + y1) * .5
		crossings = crossings[:0]
		for _, e := range s.edges {
			if (e.A.Y <= mid && e.B.Y > mid) || (e.B.Y <= mid && e.A.Y > mid) {
				crossings = append(crossings, crossing{xAt(e, mid), e})
			}
		}
		sort.Slice(crossings, func(i, j int) bool { return crossings[i].x < crossings[j].x })
		for j := 1; j < len(crossings); j++ {
			a, b := crossings[j-1], crossings[j]
			if b.x-a.x < 1e-12 || !s.Contains(geom.V{X: (a.x + b.x) * .5, Y: mid}) {
				continue
			}
			p := []geom.V{{X: xAt(a.edge, y0), Y: y0}, {X: xAt(b.edge, y0), Y: y0}, {X: xAt(b.edge, y1), Y: y1}, {X: xAt(a.edge, y1), Y: y1}}
			p = geom.CleanPolygon(p)
			if geom.PolygonArea(p) > 1e-15 {
				grid = append(grid, RockCell{Polygon: p, Raised: true})
			}
		}
	}
	return grid
}

// ClipShapeBand intersects structural loops with the owned horizontal band.
func ClipShapeBand(s RockShape, minY, maxY float64) []RockShape {
	if s.Max.Y <= minY || s.Min.Y >= maxY {
		return nil
	}
	if s.Min.Y >= minY && s.Max.Y <= maxY {
		return []RockShape{s}
	}
	parts := []RockShape{s}
	for _, h := range [][]geom.V{
		{{X: s.Min.X - 1, Y: s.Min.Y - 1}, {X: s.Max.X + 1, Y: s.Min.Y - 1}, {X: s.Max.X + 1, Y: minY}, {X: s.Min.X - 1, Y: minY}},
		{{X: s.Min.X - 1, Y: maxY}, {X: s.Max.X + 1, Y: maxY}, {X: s.Max.X + 1, Y: s.Max.Y + 1}, {X: s.Min.X - 1, Y: s.Max.Y + 1}},
	} {
		if geom.PolygonArea(h) <= 0 {
			continue
		}
		var next []RockShape
		for _, p := range parts {
			result, _ := DifferenceShape(p, h)
			next = append(next, result...)
		}
		parts = next
	}
	return parts
}

// ShapesConnect accepts shared area or an edge, but excludes corner contact.
func ShapesConnect(a, b RockShape, offset geom.V) bool {
	if a.Max.X < b.Min.X+offset.X || a.Min.X > b.Max.X+offset.X || a.Max.Y < b.Min.Y+offset.Y || a.Min.Y > b.Max.Y+offset.Y {
		return false
	}
	for _, e := range b.edges {
		p, q := e.A.Add(offset), e.B.Add(offset)
		if a.Contains(geom.LerpVector(p, q, .5)) {
			return true
		}
		connected := false
		a.index.Query(geom.Bounds{Min: geom.V{X: min(p.X, q.X) - 1e-9, Y: min(p.Y, q.Y) - 1e-9}, Max: geom.V{X: max(p.X, q.X) + 1e-9, Y: max(p.Y, q.Y) + 1e-9}}, func(i int) bool {
			f := a.edges[i]
			u, v := f.B.Sub(f.A), q.Sub(p)
			if math.Abs(geom.Cross(u, v)) < 1e-12 && geom.SegmentDistanceSquared(f.A, f.B, p, q) < 1e-18 {
				t0, t1 := p.Sub(f.A).Dot(u)/u.Len2(), q.Sub(f.A).Dot(u)/u.Len2()
				connected = (min(1, max(t0, t1))-max(0, min(t0, t1)))*u.Len() > 1e-9
			}
			return !connected
		})
		if connected {
			return true
		}
	}
	if len(a.edges) > 0 {
		return b.Contains(a.edges[0].A.Sub(offset))
	}
	return false
}
