package main

import (
	"image/color"
	"math"
	"sort"
)

// Guides divide the ordinary Voronoi faces along their sampled curves. Each
// resulting face gets its own interior color sample, including a lit fragment
// whose original site lay on the shadow side. No guide sites or rows are added.
func newGuideRockGrid(seeds []V, guides []Guide, colorAt func(V) color.NRGBA) RockGrid {
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
func guideRockFaces(seeds []V, guides []Guide) RockGrid {
	var fragments []guideFragment
	for i, site := range seeds {
		poly := voronoiCell(i, seeds)
		faces := [][]V{poly}
		lo, hi := V{W, H}, V{}
		for _, p := range poly {
			lo.X, lo.Y = math.Min(lo.X, p.X), math.Min(lo.Y, p.Y)
			hi.X, hi.Y = math.Max(hi.X, p.X), math.Max(hi.Y, p.Y)
		}
		for j := range guides {
			g := &guides[j]
			if g.Max.X < lo.X || g.Min.X > hi.X || g.Max.Y < lo.Y || g.Min.Y > hi.Y {
				continue
			}
			var next [][]V
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
				center = faceCenter(face)
			}
			fragments = append(fragments, makeGuideFragment(face, center, len(faces) > 1))
		}
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

func cross(a, b V) float64 { return a.X*b.Y - a.Y*b.X }

// Boundary points are excluded so an existing seam cannot split a face again.
func insideFace(p V, poly []V) bool {
	inside := false
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		d := b.Sub(a)
		if d.Len2() > 0 {
			q := a.Add(d.Mul(clamp(p.Sub(a).Dot(d)/d.Len2(), 0, 1)))
			if p.Sub(q).Len2() < 1e-14 {
				return false
			}
		}
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < a.X+(b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y) {
			inside = !inside
		}
	}
	return inside
}

type guideHit struct {
	path, edge float64 // segment index plus fractional position
	p          V
}

// Cut only paths entering and leaving the face. An open guide tip within a
// face does not divide it, and its tangent is never extended into distant cells.
// Repeating the cut handles curls, multiple crossings and intersecting guides.
func splitGuideFace(poly []V, g *Guide) [][]V {
	var hits []guideHit
	for j := 0; j+1 < len(g.Pts); j++ {
		a, d := g.Pts[j], g.Pts[j+1].Sub(g.Pts[j])
		for i, b := range poly {
			e := poly[(i+1)%len(poly)].Sub(b)
			den := cross(d, e)
			if math.Abs(den) < 1e-12 {
				continue
			}
			t, u := cross(b.Sub(a), e)/den, cross(b.Sub(a), d)/den
			if t >= -1e-9 && t <= 1+1e-9 && u >= -1e-9 && u <= 1+1e-9 {
				t, u = clamp(t, 0, 1), clamp(u, 0, 1)
				hits = append(hits, guideHit{float64(j) + t, math.Mod(float64(i)+u, float64(len(poly))), a.Add(d.Mul(t))})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].path < hits[j].path })
	unique := hits[:0]
	for _, hit := range hits {
		if len(unique) == 0 || hit.p.Sub(unique[len(unique)-1].p).Len2() > 1e-14 {
			unique = append(unique, hit)
		}
	}
	for i := 1; i < len(unique); i++ {
		a, b := unique[i-1], unique[i]
		mid := (a.path + b.path) * .5
		j := int(mid)
		if !insideFace(lerpV(g.Pts[j], g.Pts[j+1], mid-float64(j)), poly) {
			continue
		}
		path := []V{a.p}
		for k := int(math.Floor(a.path)) + 1; float64(k) < b.path; k++ {
			path = append(path, g.Pts[k])
		}
		path = append(path, b.p)
		left, right := boundaryArc(poly, a, b), boundaryArc(poly, b, a)
		for k := len(path) - 2; k > 0; k-- {
			left = append(left, path[k])
		}
		right = append(right, path[1:len(path)-1]...)
		left, right = cleanFace(left), cleanFace(right)
		if len(left) < 3 || len(right) < 3 || faceArea(left) < 1e-8 || faceArea(right) < 1e-8 {
			continue
		}
		return append(splitGuideFace(left, g), splitGuideFace(right, g)...)
	}
	return [][]V{poly}
}

func boundaryArc(poly []V, a, b guideHit) []V {
	end := b.edge
	if end <= a.edge {
		end += float64(len(poly))
	}
	arc := []V{a.p}
	for k := int(math.Floor(a.edge)) + 1; float64(k) < end; k++ {
		arc = append(arc, poly[k%len(poly)])
	}
	return append(arc, b.p)
}

func cleanFace(poly []V) []V {
	out := make([]V, 0, len(poly))
	for _, p := range poly {
		if len(out) == 0 || p.Sub(out[len(out)-1]).Len2() > 1e-14 {
			out = append(out, p)
		}
	}
	if len(out) > 1 && out[0].Sub(out[len(out)-1]).Len2() < 1e-14 {
		out = out[:len(out)-1]
	}
	// Straight sampled guides should produce one edge, not dozens of facets.
	for changed := true; changed && len(out) > 3; {
		changed = false
		for i, p := range out {
			a, b := out[(i+len(out)-1)%len(out)], out[(i+1)%len(out)]
			if math.Abs(cross(p.Sub(a), b.Sub(p))) < 1e-9 && p.Sub(a).Dot(b.Sub(p)) >= 0 {
				out = append(out[:i], out[i+1:]...)
				changed = true
				break
			}
		}
	}
	return orderPolygon(out)
}

func faceArea(poly []V) float64 {
	area := 0.0
	for i := 1; i+1 < len(poly); i++ {
		area += cross(poly[i].Sub(poly[0]), poly[i+1].Sub(poly[0])) * .5
	}
	return area
}

// Ear clipping supports concave faces along bends without filling across the
// guide. Indices refer to the perimeter, allowing rendering to omit inner rims.
func faceTriangles(poly []V) [][3]int {
	remaining := make([]int, len(poly))
	for i := range remaining {
		remaining[i] = i
	}
	var triangles [][3]int
	for len(remaining) > 3 {
		found := false
		for j, b := range remaining {
			a, c := remaining[(j+len(remaining)-1)%len(remaining)], remaining[(j+1)%len(remaining)]
			if cross(poly[b].Sub(poly[a]), poly[c].Sub(poly[b])) <= 1e-10 {
				continue
			}
			blocked := false
			for _, k := range remaining {
				if k == a || k == b || k == c {
					continue
				}
				p := poly[k]
				if cross(poly[b].Sub(poly[a]), p.Sub(poly[a])) >= -1e-10 &&
					cross(poly[c].Sub(poly[b]), p.Sub(poly[b])) >= -1e-10 &&
					cross(poly[a].Sub(poly[c]), p.Sub(poly[c])) >= -1e-10 {
					blocked = true
					break
				}
			}
			if !blocked {
				triangles = append(triangles, [3]int{a, b, c})
				remaining = append(remaining[:j], remaining[j+1:]...)
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	if len(remaining) == 3 {
		triangles = append(triangles, [3]int{remaining[0], remaining[1], remaining[2]})
	}
	return triangles
}

func faceCenter(poly []V) V {
	origin, weighted, area := poly[0], V{}, 0.0
	for i := 1; i+1 < len(poly); i++ {
		a, b := poly[i].Sub(origin), poly[i+1].Sub(origin)
		weight := cross(a, b)
		weighted = weighted.Add(a.Add(b).Mul(weight / 3))
		area += weight
	}
	center := origin.Add(weighted.Mul(1 / area))
	if insideFace(center, poly) {
		return center
	}
	// A deep curl can put the centroid outside the face. Sample the largest
	// interior triangle instead so light and material never come from a neighbor.
	largest := 0.0
	for _, tri := range faceTriangles(poly) {
		a, b, c := poly[tri[0]], poly[tri[1]], poly[tri[2]]
		if size := cross(b.Sub(a), c.Sub(a)); size > largest {
			largest, center = size, a.Add(b).Add(c).Mul(1.0/3)
		}
	}
	return center
}
