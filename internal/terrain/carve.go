package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func FiniteCarveValue(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func finiteCarvePoint(p geom.V) bool { return FiniteCarveValue(p.X) && FiniteCarveValue(p.Y) }

type RockCut struct {
	Poly     []geom.V
	Min, Max geom.V
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
	grid, parents, changed := cut.grid(old.Grid, old.Top)
	if !changed {
		return old, nil, false
	}
	cuts := append(append([]RockCut(nil), old.Cuts...), cut)
	topology := carveTopology(old.Topology, grid, parents, cuts, old.Top)
	updated := PrepareTerrainGeometry(SectionData{ID: id, Guides: old.Guides, ForegroundTopology: topology}, tolerance)
	updated.source = old.source
	updated.Vegetation = old.Vegetation
	return updated, parents, true
}

func (cut RockCut) grid(source RockGrid, top float64) (RockGrid, []int, bool) {
	var grid RockGrid
	var parents []int
	changed := false
	hole := cut.localPolygon(top)
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
