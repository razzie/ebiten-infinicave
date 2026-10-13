package terrain

import "github.com/razzie/ebiten-infinicave/internal/geom"

const supportInset = .0005

// InsetRockAnchor finds a point strictly inside the supporting polygon without
// changing visual placement. Shrinking the inset handles thin and jagged rock.
// Only nearby edges can support a placement outside the polygon.
func InsetRockAnchor(poly []geom.V, p, inward geom.V) (geom.V, bool) {
	if len(poly) < 3 {
		return p, false
	}
	inward = inward.Norm()
	if inward.Len2() > 0 {
		for depth := supportInset; depth >= 1e-8; depth *= .5 {
			q := p.Add(inward.Mul(depth))
			if geom.InsidePolygon(q, poly) {
				return q, true
			}
		}
	}
	if geom.InsidePolygon(p, poly) {
		return p, true
	}
	best, edge := .003*.003, -1
	var nearest geom.V
	for i, a := range poly {
		d := poly[(i+1)%len(poly)].Sub(a)
		if d.Len2() == 0 {
			continue
		}
		q := a.Add(d.Mul(geom.Clamp(p.Sub(a).Dot(d)/d.Len2(), 0, 1)))
		if distance := p.Sub(q).Len2(); distance < best {
			best, edge, nearest = distance, i, q
		}
	}
	if edge >= 0 {
		n := poly[(edge+1)%len(poly)].Sub(poly[edge]).Norm().Perp()
		if geom.PolygonArea(poly) < 0 {
			n = n.Mul(-1)
		}
		// At a convex corner, move slightly along the supporting edge as well.
		mid := geom.LerpVector(poly[edge], poly[(edge+1)%len(poly)], .5)
		for depth := supportInset; depth >= 1e-8; depth *= .5 {
			for _, q := range [2]geom.V{nearest.Add(n.Mul(depth)), nearest.Add(n.Add(mid.Sub(nearest).Norm()).Mul(depth))} {
				if geom.InsidePolygon(q, poly) {
					return q, true
				}
			}
		}
	}
	return p, false
}

// MushroomSupport is independent of the visual guide root. Generated plants
// retain a verified rock-interior point; hand-authored plants use their root.
func MushroomSupport(m Mushroom) geom.V {
	if m.supportSet {
		return m.support
	}
	return m.Anchor
}

// SpiderWebSupports excludes all free silk nodes. Generation retains inset
// support points separately from the fixed nodes used to render the network.
func SpiderWebSupports(w SpiderWeb) []geom.V {
	if len(w.supports) > 0 {
		return w.supports
	}
	if w.Network != nil {
		return w.Network.Nodes[:w.Network.Anchors]
	}
	return []geom.V{w.Corner, w.A, w.B}
}
