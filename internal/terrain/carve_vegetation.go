package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Retain CPU plants for support checks, branch remapping, and section reuploads.
// Slices are immutable once published; edits create replacements.
type Vegetation struct {
	Vines, ForegroundVines []Vine
	Mushrooms              []MushroomGroup
	Cuts                   []RockCut
}

func SectionVegetation(data SectionData) Vegetation {
	return Vegetation{data.Vines, data.ForegroundVines, data.Mushrooms, data.vegetationCuts}
}

func (cut RockCut) vegetation(source Vegetation, ground RockGrid, top float64) (Vegetation, bool) {
	for _, previous := range source.Cuts {
		if sameRockCut(cut, previous) {
			return source, false
		}
	}
	updated := source
	groundFaces := mushroomGroundFromCells(ground)
	var mushrooms []MushroomGroup
	removed := false
	for _, group := range source.Mushrooms {
		var kept []Mushroom
		for _, mushroom := range group.Mushrooms {
			if !cut.affectsMushroomSupport(mushroom, top) || mushroomSupported(mushroom, groundFaces) {
				kept = append(kept, mushroom)
			} else {
				removed = true
			}
		}
		if len(kept) > 0 {
			mushrooms = append(mushrooms, MushroomGroup{Mushrooms: kept})
		}
	}
	if removed {
		updated.Mushrooms = mushrooms
	}
	// Ribbons and contact shadows can overlap a hole even when their
	// centerline stays outside it. Keep the mask for the full rendered width.
	var backHit, frontHit bool
	updated.Vines, backHit = cut.Vines(source.Vines, top)
	updated.ForegroundVines, frontHit = cut.Vines(source.ForegroundVines, top)
	if backHit || frontHit {
		updated.Cuts = append(append([]RockCut(nil), source.Cuts...), cut)
	}
	return updated, removed || backHit || frontHit
}

// Only a cut near the buried root/anchor can remove existing support. Caps and
// distant mushrooms need no search through every remaining terrain face.
func (cut RockCut) affectsMushroomSupport(m Mushroom, top float64) bool {
	root := m.Anchor
	if len(m.Stem) > 0 {
		root = m.Stem[0]
	}
	const contact = .002 // same allowance as mushroomSupported
	return math.Max(root.X, m.Anchor.X)+contact >= cut.Min.X &&
		math.Min(root.X, m.Anchor.X)-contact <= cut.Max.X &&
		math.Max(root.Y, m.Anchor.Y)+top+contact >= cut.Min.Y &&
		math.Min(root.Y, m.Anchor.Y)+top-contact <= cut.Max.Y
}

func (cut RockCut) Plants(old *Geometry) (*Geometry, bool) {
	if old == nil {
		return old, false
	}
	vegetation, changed := cut.vegetation(old.Vegetation, old.Grid, old.Top)
	if !changed {
		return old, false
	}
	updated := *old
	updated.Vegetation = vegetation
	return &updated, true
}

func sameRockCut(a, b RockCut) bool {
	if len(a.Poly) != len(b.Poly) {
		return false
	}
	for i, p := range a.Poly {
		if p != b.Poly[i] {
			return false
		}
	}
	return true
}

func mushroomSupported(m Mushroom, ground []mushroomGround) bool {
	root := m.Anchor
	if len(m.Stem) > 0 {
		root = m.Stem[0]
	}
	// The generated root is sunk into rock. Include the same small contact
	// allowance used by initial placement for jagged guide/contour borders.
	const contact = .002
	for _, cell := range ground {
		if math.Max(root.X, m.Anchor.X)+contact < cell.lo.X || math.Min(root.X, m.Anchor.X)-contact > cell.hi.X ||
			math.Max(root.Y, m.Anchor.Y)+contact < cell.lo.Y || math.Min(root.Y, m.Anchor.Y)-contact > cell.hi.Y {
			continue
		}
		if geom.InsidePolygon(root, cell.poly) || geom.InsidePolygon(m.Anchor, cell.poly) {
			return true
		}
		for i, a := range cell.poly {
			if geom.SegmentDistanceSquared(root, m.Anchor, a, cell.poly[(i+1)%len(cell.poly)]) <= contact*contact {
				return true
			}
		}
	}
	return false
}

// Clip a centerline segment against the convex cut. Boundary-only tangencies
// leave the line intact; the render mask still removes any overlapping ribbon.
func vineCutInterval(a, b geom.V, hole []geom.V) (float64, float64, bool) {
	lo, hi := 0.0, 1.0
	for i, p := range hole {
		n := hole[(i+1)%len(hole)].Sub(p).Perp().Mul(-1).Norm()
		start, end := a.Sub(p).Dot(n), b.Sub(p).Dot(n)
		if math.Abs(start) <= 1e-12 && math.Abs(end) <= 1e-12 {
			return 0, 0, false
		}
		speed := end - start
		if math.Abs(speed) < 1e-15 {
			if start > 0 {
				return 0, 0, false
			}
			continue
		}
		crossing := -start / speed
		if speed < 0 {
			lo = math.Max(lo, crossing)
		} else {
			hi = math.Min(hi, crossing)
		}
		if hi-lo <= 1e-12 {
			return 0, 0, false
		}
	}
	return lo, hi, true
}

type vineFragment struct {
	points []VinePoint
	first  bool // retains the original branch attachment
}

func splitVine(vine Vine, hole []geom.V) []vineFragment {
	holeMin, holeMax := geom.PolygonBounds(hole)
	var fragments []vineFragment
	var current []VinePoint
	first := true
	flush := func() {
		if len(current) >= 2 {
			fragments = append(fragments, vineFragment{current, first})
		}
		current = nil
		first = false
	}
	appendPoint := func(p VinePoint) {
		if len(current) == 0 || p.P.Sub(current[len(current)-1].P).Len2() > 1e-24 {
			current = append(current, p)
		}
	}
	for i := 1; i < len(vine.Points); i++ {
		a, b := vine.Points[i-1], vine.Points[i]
		if math.Max(a.P.X, b.P.X) < holeMin.X || math.Min(a.P.X, b.P.X) > holeMax.X ||
			math.Max(a.P.Y, b.P.Y) < holeMin.Y || math.Min(a.P.Y, b.P.Y) > holeMax.Y {
			appendPoint(a)
			appendPoint(b)
			continue
		}
		lo, hi, hit := vineCutInterval(a.P, b.P, hole)
		if !hit {
			appendPoint(a)
			appendPoint(b)
			continue
		}
		point := func(t float64) VinePoint {
			return VinePoint{P: geom.LerpVector(a.P, b.P, t), Radius: geom.Lerp(a.Radius, b.Radius, t)}
		}
		if lo > 1e-12 {
			appendPoint(a)
			appendPoint(point(lo))
		}
		flush()
		if hi < 1-1e-12 {
			appendPoint(point(hi))
			appendPoint(b)
		}
	}
	flush()
	return fragments
}

func (cut RockCut) Vines(source []Vine, top float64) ([]Vine, bool) {
	hit := false
	for _, vine := range source {
		for _, point := range vine.Points {
			if point.P.X+point.Radius+.002 >= cut.Min.X && point.P.X-point.Radius-.002 <= cut.Max.X &&
				point.P.Y+top+point.Radius+.002 >= cut.Min.Y && point.P.Y+top-point.Radius-.002 <= cut.Max.Y {
				hit = true
				break
			}
		}
		if hit {
			break
		}
	}
	if !hit {
		// Sparse, long segments may cross the cut between their samples.
		for _, vine := range source {
			for i := 1; i < len(vine.Points); i++ {
				a, b := vine.Points[i-1], vine.Points[i]
				radius := math.Max(a.Radius, b.Radius) + .002
				if math.Max(a.P.X, b.P.X)+radius >= cut.Min.X && math.Min(a.P.X, b.P.X)-radius <= cut.Max.X &&
					math.Max(a.P.Y, b.P.Y)+top+radius >= cut.Min.Y && math.Min(a.P.Y, b.P.Y)+top-radius <= cut.Max.Y {
					hit = true
					break
				}
			}
			if hit {
				break
			}
		}
	}
	if !hit {
		return source, false
	}
	hole := cut.localPolygon(top)
	var vines []Vine
	byOriginal := make([][]int, len(source))
	for i, original := range source {
		for _, fragment := range splitVine(original, hole) {
			vine := original
			vine.Foreground = source[VineFamily(source, i)].Foreground
			vine.Points = fragment.points
			vine.Parent, vine.Joint, vine.Depth = -1, 0, 0
			vine.styleFamily, vine.styleSet = VineStyleFamily(source, i), true
			if fragment.first && original.Parent >= 0 && original.Parent < i {
				for _, parent := range byOriginal[original.Parent] {
					for joint, point := range vines[parent].Points {
						if point.P.Sub(vine.Points[0].P).Len2() < 1e-20 {
							vine.Parent, vine.Joint, vine.Depth = parent, joint, vines[parent].Depth+1
							break
						}
					}
					if vine.Parent >= 0 {
						break
					}
				}
			}
			byOriginal[i] = append(byOriginal[i], len(vines))
			vines = append(vines, vine)
		}
	}
	return vines, true
}
