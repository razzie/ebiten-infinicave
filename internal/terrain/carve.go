package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func FiniteCarveValue(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func finiteCarvePoint(p geom.V) bool { return FiniteCarveValue(p.X) && FiniteCarveValue(p.Y) }

type RockCut struct {
	Poly                       []geom.V
	Min, Max                   geom.V
	anchorCenter               geom.V
	anchorInner2, anchorOuter2 float64
}

// ContainsAnchor uses the exact cut polygon, excluding boundary-only contact.
// Generated circles accept/reject almost every point with a distance test;
// only the narrow band between the incircle and circumcircle needs edge tests.
func (cut RockCut) ContainsAnchor(p geom.V) bool {
	if p.X <= cut.Min.X || p.X >= cut.Max.X || p.Y <= cut.Min.Y || p.Y >= cut.Max.Y {
		return false
	}
	if cut.anchorOuter2 > 0 {
		d := p.Sub(cut.anchorCenter).Len2()
		if d < cut.anchorInner2 {
			return true
		}
		if d >= cut.anchorOuter2 {
			return false
		}
	}
	return geom.InsidePolygon(p, cut.Poly)
}

// Check section coverage with intervals, avoiding a walk through potentially
// enormous unloaded ranges for large cuts. Out-of-world area is known empty.
func (cut RockCut) Covered(sections []int64) bool {
	if cut.Max.X <= 0 || cut.Min.X >= Width || cut.Min.Y >= 0 {
		return true
	}
	end := math.Min(0, cut.Max.Y)
	for i := len(sections) - 1; i >= 0; i-- {
		top := SectionTop(sections[i])
		if top+SectionHeight <= cut.Min.Y || top >= end {
			continue
		}
		if top > cut.Min.Y+geom.QueryEpsilon {
			return false
		}
		cut.Min.Y = top + SectionHeight
		if cut.Min.Y >= end-geom.QueryEpsilon {
			return true
		}
	}
	return false
}

func (cut RockCut) localPolygon(top float64) []geom.V {
	poly := make([]geom.V, len(cut.Poly))
	for i, p := range cut.Poly {
		poly[i] = p.Sub(geom.V{Y: top})
	}
	return poly
}

func cutIntersection(poly, hole []geom.V) []geom.V {
	for i, a := range hole {
		normal := hole[(i+1)%len(hole)].Sub(a).Perp().Mul(-1).Norm()
		poly = geom.ClipHalfPlane(poly, normal, a.Dot(normal))
		if len(poly) < 3 {
			return nil
		}
	}
	return poly
}

func convexCarveFace(poly []geom.V) bool {
	for i, a := range poly {
		b, c := poly[(i+1)%len(poly)], poly[(i+2)%len(poly)]
		if geom.Cross(b.Sub(a), c.Sub(b)) < -1e-16 {
			return false
		}
	}
	return true
}

func (cut RockCut) Intersects(poly []geom.V, top float64) bool {
	lo, hi := geom.PolygonBounds(poly)
	if hi.X <= cut.Min.X || lo.X >= cut.Max.X || hi.Y+top <= cut.Min.Y || lo.Y+top >= cut.Max.Y {
		return false
	}
	hole := cut.localPolygon(top)
	if convexCarveFace(poly) {
		return geom.PolygonArea(cutIntersection(poly, hole)) > 1e-15
	}
	for _, tri := range geom.Triangulate(poly) {
		if geom.PolygonArea(cutIntersection([]geom.V{poly[tri[0]], poly[tri[1]], poly[tri[2]]}, hole)) > 1e-15 {
			return true
		}
	}
	return false
}

// Subtract a convex hole by peeling off the outside of each half-plane.
// Each piece is disjoint; the final inside remainder is discarded. Triangulate
// concave source faces first so half-plane clipping cannot bridge concavities.
func subtractRockCut(poly, hole []geom.V) [][]geom.V {
	sources := [][]geom.V{poly}
	if !convexCarveFace(poly) {
		sources = nil
		for _, tri := range geom.Triangulate(poly) {
			sources = append(sources, []geom.V{poly[tri[0]], poly[tri[1]], poly[tri[2]]})
		}
	}
	var pieces [][]geom.V
	for _, source := range sources {
		inside := source
		for i, a := range hole {
			normal := hole[(i+1)%len(hole)].Sub(a).Perp().Mul(-1).Norm()
			outside := geom.ClipHalfPlane(inside, normal.Mul(-1), -a.Dot(normal))
			if len(outside) >= 3 && geom.PolygonArea(outside) > 1e-15 {
				pieces = append(pieces, outside)
			}
			inside = geom.ClipHalfPlane(inside, normal, a.Dot(normal))
			if len(inside) < 3 || geom.PolygonArea(inside) <= 1e-15 {
				break
			}
		}
	}
	// Remove partition edges within the original facet where possible. A ring
	// stays as multiple simple faces; joinFaces never fills an enclosed hole.
	for i := 0; i < len(pieces); i++ {
		for j := i + 1; j < len(pieces); j++ {
			if mergeableBorder(pieces[i], pieces[j], nil) <= mergeTolerance {
				continue
			}
			if merged := joinFaces(pieces[i], pieces[j]); len(merged) >= 3 {
				pieces[i] = merged
				pieces = append(pieces[:j], pieces[j+1:]...)
				j = i // retry the enlarged face against all remaining pieces
			}
		}
	}
	return pieces
}

func (cut RockCut) Geometry(id int64, old *Geometry, tolerance float64) (*Geometry, []int, bool) {
	if old == nil {
		return old, nil, false
	}
	if cut.Max.Y <= old.Top+GenerationMinY || cut.Min.Y >= old.Top+generationMaxY || cut.Max.X <= 0 || cut.Min.X >= Width {
		return old, nil, false
	}
	var shapes []RockShape
	var parents []int
	var hole []geom.V
	changed := false
	for i, s := range old.Shapes {
		parts := []RockShape{s}
		edited := false
		if s.Max.X > cut.Min.X && s.Min.X < cut.Max.X && s.Max.Y+old.Top > cut.Min.Y && s.Min.Y+old.Top < cut.Max.Y {
			if hole == nil {
				hole = cut.localPolygon(old.Top)
			}
			parts, edited = DifferenceShape(s, hole)
		}
		changed = changed || edited
		shapes = append(shapes, parts...)
		for range parts {
			parents = append(parents, i)
		}
	}
	if !changed {
		return old, nil, false
	}
	updated := *old
	updated.setShapes(shapes)
	// Preserve untouched face slices and keys for query cache reuse.
	for i, p := range parents {
		if len(shapes[i].Loops) > 0 && len(old.Shapes[p].Loops) > 0 && &shapes[i].Loops[0][0] == &old.Shapes[p].Loops[0][0] {
			updated.Faces[i] = old.Faces[p]
			updated.Faces[i].block = i
		}
	}
	updated.Cuts = append(append([]RockCut(nil), old.Cuts...), cut)
	topology := *old.Topology
	topology.Cuts = updated.Cuts
	topology.Top = old.Top
	// Base material remains immutable, including its original lighting.
	updated.Topology = &topology
	updated.Collision = updated.shapeCollision(id, tolerance)
	return &updated, parents, true
}

func (cut RockCut) grid(source RockGrid, top float64) (RockGrid, []int, bool) {
	var grid RockGrid
	var parents []int
	changed := false
	var hole []geom.V
	for i, cell := range source {
		if !cut.Intersects(cell.Polygon, top) {
			if changed {
				grid = append(grid, cell)
				parents = append(parents, i)
			}
			continue
		}
		if !changed {
			// Most cached sections miss a small cut entirely. Allocate only
			// when it removes area, and copy the untouched prefix once.
			grid = make(RockGrid, i, len(source)+8)
			copy(grid, source[:i])
			parents = make([]int, i, len(source)+8)
			for j := range parents {
				parents[j] = j
			}
			changed = true
		}
		if hole == nil {
			hole = cut.localPolygon(top)
		}
		for _, poly := range subtractRockCut(cell.Polygon, hole) {
			fragment := cell
			fragment.Polygon = poly
			if !geom.InsidePolygon(fragment.Center, poly) {
				fragment.Center = geom.PolygonCenter(poly)
				fragment.Z = RockDepthAt(cell, fragment.Center)
			}
			grid = append(grid, fragment)
			parents = append(parents, i)
		}
	}
	if !changed {
		return source, nil, false
	}
	return grid, parents, changed
}

func CarvedTopology(grid RockGrid, cuts []RockCut, top float64) *RockTopology {
	t := &RockTopology{Grid: grid, neighbors: RockNeighbors(grid), Cuts: cuts, Top: top}
	t.prepareBoundary()
	return t
}
