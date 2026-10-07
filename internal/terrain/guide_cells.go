package terrain

import (
	"image/color"
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Guides divide the ordinary Voronoi faces along their sampled curves. Each
// resulting face gets its own interior color sample, including a lit fragment
// whose original site lay on the shadow side. No guide sites or rows are added.
func newGuideRockGrid(seeds []geom.V, guides []Guide, colorAt func(geom.V) color.NRGBA) RockGrid {
	grid := guideRockFaces(seeds, guides)
	visible := grid[:0]
	for _, cell := range grid {
		if cell.Color = colorAt(cell.Center); cell.Color.A != 0 {
			visible = append(visible, cell)
		}
	}
	return visible
}

// Retain invisible neighbors until heights and normals have been computed.
func guideRockFaces(seeds []geom.V, guides []Guide) RockGrid {
	cells := voronoiCells(seeds)
	perSite := make([][]guideFragment, len(seeds))
	parallelFor(len(seeds), func(i int) {
		site, poly := seeds[i], cells[i]
		faces := [][]geom.V{poly}
		lo, hi := geom.V{X: generationWidth, Y: generationMaxY}, geom.V{Y: GenerationMinY}
		for _, p := range poly {
			lo.X, lo.Y = math.Min(lo.X, p.X), math.Min(lo.Y, p.Y)
			hi.X, hi.Y = math.Max(hi.X, p.X), math.Max(hi.Y, p.Y)
		}
		for j := range guides {
			g := &guides[j]
			if g.Max.X < lo.X || g.Min.X > hi.X || g.Max.Y < lo.Y || g.Min.Y > hi.Y {
				continue
			}
			var next [][]geom.V
			for _, face := range faces {
				next = append(next, splitGuideFace(face, g)...)
			}
			faces = next
		}
		for _, face := range faces {
			if len(face) < 3 {
				continue
			}
			center := site
			if len(faces) > 1 {
				center = geom.PolygonCenter(face)
			}
			perSite[i] = append(perSite[i], makeGuideFragment(face, center, len(faces) > 1))
		}
	})
	var fragments []guideFragment
	for _, f := range perSite {
		fragments = append(fragments, f...)
	}
	mergeGuideFragments(fragments, guides)
	var grid RockGrid
	for _, fragment := range fragments {
		if len(fragment.poly) < 3 {
			continue
		}
		grid = append(grid, RockCell{Center: fragment.center, Polygon: fragment.poly})
	}
	return grid
}

type guideHit struct {
	path, edge float64 // segment index plus fractional position
	p          geom.V
}

// Cut only paths entering and leaving the face. An open guide tip within a
// face does not divide it, and its tangent is never extended into distant cells.
// Repeating the cut handles curls, multiple crossings and intersecting guides.
func splitGuideFace(poly []geom.V, g *Guide) [][]geom.V {
	var hits []guideHit
	for j := 0; j+1 < len(g.Pts); j++ {
		a, d := g.Pts[j], g.Pts[j+1].Sub(g.Pts[j])
		for i, b := range poly {
			e := poly[(i+1)%len(poly)].Sub(b)
			den := geom.Cross(d, e)
			if math.Abs(den) < 1e-18 {
				continue
			}
			t, u := geom.Cross(b.Sub(a), e)/den, geom.Cross(b.Sub(a), d)/den
			if t >= -1e-9 && t <= 1+1e-9 && u >= -1e-9 && u <= 1+1e-9 {
				t, u = geom.Clamp(t, 0, 1), geom.Clamp(u, 0, 1)
				hits = append(hits, guideHit{float64(j) + t, math.Mod(float64(i)+u, float64(len(poly))), a.Add(d.Mul(t))})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].path < hits[j].path })
	unique := hits[:0]
	for _, hit := range hits {
		if len(unique) == 0 || hit.p.Sub(unique[len(unique)-1].p).Len2() > 1e-20 {
			unique = append(unique, hit)
		}
	}
	for i := 1; i < len(unique); i++ {
		a, b := unique[i-1], unique[i]
		mid := (a.path + b.path) * .5
		j := int(mid)
		if !geom.InsidePolygon(geom.LerpVector(g.Pts[j], g.Pts[j+1], mid-float64(j)), poly) {
			continue
		}
		path := []geom.V{a.p}
		for k := int(math.Floor(a.path)) + 1; float64(k) < b.path; k++ {
			path = append(path, g.Pts[k])
		}
		path = append(path, b.p)
		left, right := boundaryArc(poly, a, b), boundaryArc(poly, b, a)
		for k := len(path) - 2; k > 0; k-- {
			left = append(left, path[k])
		}
		right = append(right, path[1:len(path)-1]...)
		left, right = geom.CleanPolygon(left), geom.CleanPolygon(right)
		if len(left) < 3 || len(right) < 3 || geom.PolygonArea(left) < 1e-14 || geom.PolygonArea(right) < 1e-14 {
			continue
		}
		return append(splitGuideFace(left, g), splitGuideFace(right, g)...)
	}
	return [][]geom.V{poly}
}

func boundaryArc(poly []geom.V, a, b guideHit) []geom.V {
	end := b.edge
	if end <= a.edge {
		end += float64(len(poly))
	}
	arc := []geom.V{a.p}
	for k := int(math.Floor(a.edge)) + 1; float64(k) < end; k++ {
		arc = append(arc, poly[k%len(poly)])
	}
	return append(arc, b.p)
}
