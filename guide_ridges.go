package infinicave

import (
	"math"
	"math/rand"
)

// Broad, slightly uneven facets give the crest a chipped rock silhouette.
// These are border vertices, not Voronoi sites. Build them before translating
// a guide into a section window so overlapping windows get identical ridges.
func ridgedGuide(g Guide, seed int64) Guide {
	rng := rand.New(rand.NewSource(seed))
	length := g.S[len(g.S)-1]
	pts := []V{g.Pts[0]}
	for s := 0.0; s < length; {
		step := math.Min(lerp(0.012, 0.023, rng.Float64()), length-s)
		// Shorten facets at tight bends so the original curl remains intact.
		for step > 0.005 {
			a, _, _ := g.frameAt(s)
			b, _, _ := g.frameAt(s + step)
			mid, _, _ := g.frameAt(s + step*.5)
			if mid.Sub(lerpV(a, b, .5)).Len() < .0008 {
				break
			}
			step *= .7
		}
		s += step
		p, _, n := g.frameAt(s)
		taper := smoothstep(0, 0.018, math.Min(s, length-s))
		pts = append(pts, p.Add(n.Mul(lerp(-0.0025, 0.0025, rng.Float64())*taper)))
	}
	g.Pts, g.S = pts, make([]float64, len(pts))
	g.Min, g.Max = pts[0], pts[0]
	for i, p := range pts {
		if i > 0 {
			g.S[i] = g.S[i-1] + p.Sub(pts[i-1]).Len()
		}
		g.Min.X, g.Min.Y = math.Min(g.Min.X, p.X), math.Min(g.Min.Y, p.Y)
		g.Max.X, g.Max.Y = math.Max(g.Max.X, p.X), math.Max(g.Max.Y, p.Y)
	}
	return g
}
