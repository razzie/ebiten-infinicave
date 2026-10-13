package terrain

import "github.com/razzie/ebiten-infinicave/internal/geom"

// Finished web networks are immutable attachments: damage removes the whole
// network instead of fitting a replacement pocket after every rock edit.
func (cut RockCut) SpiderWebs(source []SpiderWeb, top float64) []SpiderWeb {
	var kept []SpiderWeb
	changed := false
	for i, w := range source {
		hit := false
		if w.Network != nil {
			for _, t := range w.Network.Threads {
				if cut.segmentTouches(w.Network.Nodes[t.A], w.Network.Nodes[t.B], top, .0006) {
					hit = true
					break
				}
			}
		} else {
			hit = cut.segmentTouches(w.Corner, w.A, top, .0006) || cut.segmentTouches(w.Corner, w.B, top, .0006) || cut.segmentTouches(w.A, w.B, top, .0006)
		}
		if hit {
			if !changed {
				kept = append([]SpiderWeb(nil), source[:i]...)
				changed = true
			}
		} else if changed {
			kept = append(kept, w)
		}
	}
	if !changed {
		return source
	}
	return kept
}

func (cut RockCut) segmentTouches(a, b geom.V, top, pad float64) bool {
	a.Y += top
	b.Y += top
	if max(a.X, b.X)+pad < cut.Min.X || min(a.X, b.X)-pad > cut.Max.X || max(a.Y, b.Y)+pad < cut.Min.Y || min(a.Y, b.Y)-pad > cut.Max.Y {
		return false
	}
	if geom.InsidePolygon(a, cut.Poly) || geom.InsidePolygon(b, cut.Poly) {
		return true
	}
	for i, p := range cut.Poly {
		if geom.SegmentDistanceSquared(a, b, p, cut.Poly[(i+1)%len(cut.Poly)]) < pad*pad {
			return true
		}
	}
	return false
}
