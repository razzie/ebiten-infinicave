package geom

import (
	"math"
)

func Cross(a, b V) float64 { return a.X*b.Y - a.Y*b.X }

// Boundary points are excluded so an existing seam cannot split a face again.
func InsidePolygon(p V, poly []V) bool {
	inside := false
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		d := b.Sub(a)
		if d.Len2() > 0 {
			q := a.Add(d.Mul(Clamp(p.Sub(a).Dot(d)/d.Len2(), 0, 1)))
			if p.Sub(q).Len2() < 1e-20 {
				return false
			}
		}
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < a.X+(b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y) {
			inside = !inside
		}
	}
	return inside
}

func CleanPolygon(poly []V) []V {
	out := make([]V, 0, len(poly))
	for _, p := range poly {
		if len(out) == 0 || p.Sub(out[len(out)-1]).Len2() > 1e-20 {
			out = append(out, p)
		}
	}
	if len(out) > 1 && out[0].Sub(out[len(out)-1]).Len2() < 1e-20 {
		out = out[:len(out)-1]
	}
	// Straight sampled guides should produce one edge, not dozens of facets.
	for changed := true; changed && len(out) > 3; {
		changed = false
		for i, p := range out {
			a, b := out[(i+len(out)-1)%len(out)], out[(i+1)%len(out)]
			if math.Abs(Cross(p.Sub(a), b.Sub(p))) < 1e-15 && p.Sub(a).Dot(b.Sub(p)) >= 0 {
				out = append(out[:i], out[i+1:]...)
				changed = true
				break
			}
		}
	}
	return OrderPolygon(out)
}

func PolygonArea(poly []V) float64 {
	area := 0.0
	for i := 1; i+1 < len(poly); i++ {
		area += Cross(poly[i].Sub(poly[0]), poly[i+1].Sub(poly[0])) * .5
	}
	return area
}

// Ear clipping supports concave faces along bends without filling across the
// guide. Indices refer to the perimeter, allowing rendering to omit inner rims.
func Triangulate(poly []V) [][3]int {
	remaining := make([]int, len(poly))
	for i := range remaining {
		remaining[i] = i
	}
	var triangles [][3]int
	for len(remaining) > 3 {
		found := false
		for j, b := range remaining {
			a, c := remaining[(j+len(remaining)-1)%len(remaining)], remaining[(j+1)%len(remaining)]
			if Cross(poly[b].Sub(poly[a]), poly[c].Sub(poly[b])) <= 1e-16 {
				continue
			}
			blocked := false
			for _, k := range remaining {
				if k == a || k == b || k == c {
					continue
				}
				p := poly[k]
				if Cross(poly[b].Sub(poly[a]), p.Sub(poly[a])) >= -1e-16 &&
					Cross(poly[c].Sub(poly[b]), p.Sub(poly[b])) >= -1e-16 &&
					Cross(poly[a].Sub(poly[c]), p.Sub(poly[c])) >= -1e-16 {
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

func PolygonCenter(poly []V) V {
	origin, weighted, area := poly[0], V{}, 0.0
	for i := 1; i+1 < len(poly); i++ {
		a, b := poly[i].Sub(origin), poly[i+1].Sub(origin)
		weight := Cross(a, b)
		weighted = weighted.Add(a.Add(b).Mul(weight / 3))
		area += weight
	}
	center := origin.Add(weighted.Mul(1 / area))
	if InsidePolygon(center, poly) {
		return center
	}
	// A deep curl can put the centroid outside the face. Sample the largest
	// interior triangle instead so light and material never come from a neighbor.
	largest := 0.0
	for _, tri := range Triangulate(poly) {
		a, b, c := poly[tri[0]], poly[tri[1]], poly[tri[2]]
		if size := Cross(b.Sub(a), c.Sub(a)); size > largest {
			largest, center = size, a.Add(b).Add(c).Mul(1.0/3)
		}
	}
	return center
}

func PolygonBounds(poly []V) (lo, hi V) {
	lo, hi = poly[0], poly[0]
	for _, p := range poly[1:] {
		lo = V{math.Min(lo.X, p.X), math.Min(lo.Y, p.Y)}
		hi = V{math.Max(hi.X, p.X), math.Max(hi.Y, p.Y)}
	}
	return
}

func ClipHalfPlane(poly []V, n V, c float64) []V {
	if len(poly) == 0 {
		return poly
	}
	// Most sites cannot cut an already small cell. Return the existing
	// polygon unchanged instead of allocating for every distant half-plane.
	inside := 0
	for _, p := range poly {
		if p.Dot(n) <= c+1e-15 {
			inside++
		}
	}
	if inside == len(poly) {
		return poly
	}
	if inside == 0 {
		return nil
	}
	out := make([]V, 0, len(poly)+1)
	prev := poly[len(poly)-1]
	prevIn := prev.Dot(n) <= c+1e-15
	for _, cur := range poly {
		curIn := cur.Dot(n) <= c+1e-15
		if curIn != prevIn {
			d := cur.Sub(prev)
			den := d.Dot(n)
			if math.Abs(den) > 1e-18 {
				t := (c - prev.Dot(n)) / den
				out = append(out, prev.Add(d.Mul(t)))
			}
		}
		if curIn {
			out = append(out, cur)
		}
		prev, prevIn = cur, curIn
	}
	return out
}

func OrderPolygon(poly []V) []V {
	if len(poly) > 0 {
		first := 0
		for j := 1; j < len(poly); j++ {
			ax, bx := math.Round(poly[j].X*1e9), math.Round(poly[first].X*1e9)
			if ax < bx || (ax == bx && poly[j].Y < poly[first].Y) {
				first = j
			}
		}
		ordered := append([]V{}, poly[first:]...)
		poly = append(ordered, poly[:first]...)
	}
	return poly
}

func EdgeKey(a, b V) [4]int64 {
	ax, ay := int64(math.Round(a.X*1e7)), int64(math.Round(a.Y*1e7))
	bx, by := int64(math.Round(b.X*1e7)), int64(math.Round(b.Y*1e7))
	if ax > bx || (ax == bx && ay > by) {
		ax, ay, bx, by = bx, by, ax, ay
	}
	return [4]int64{ax, ay, bx, by}
}
