package terrain

import (
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type Guide struct {
	Pts        []geom.V
	S          []float64 // cumulative arc length
	BrightSign float64   // selects a continuous lit side, including around curls
	Seed       int64     // stable random stream for streamed world guides
	Min, Max   geom.V
	projection *guideProjection
}

// Each spline segment is a cubic Bezier with matching tangents at its joins.
func cubicBezier(a, b, c, d geom.V, t float64) geom.V {
	u := 1 - t
	return a.Mul(u * u * u).
		Add(b.Mul(3 * u * u * t)).
		Add(c.Mul(3 * u * t * t)).
		Add(d.Mul(t * t * t))
}

// Sample Catmull-Rom curves as Beziers for both drawing and projection.
func SplineGuide(knots []geom.V, sign float64) Guide {
	const samplesPerSegment = 64
	pts := make([]geom.V, 1, 1+(len(knots)-1)*samplesPerSegment)
	pts[0] = knots[0]
	for i := 0; i < len(knots)-1; i++ {
		a, d := knots[i], knots[i+1]
		prev, next := a.Mul(2).Sub(d), d.Mul(2).Sub(a)
		if i > 0 {
			prev = knots[i-1]
		}
		if i+2 < len(knots) {
			next = knots[i+2]
		}
		b := a.Add(d.Sub(prev).Mul(1.0 / 6))
		c := d.Sub(next.Sub(a).Mul(1.0 / 6))
		for j := 1; j <= samplesPerSegment; j++ {
			pts = append(pts, cubicBezier(a, b, c, d, float64(j)/samplesPerSegment))
		}
	}
	ss := make([]float64, len(pts))
	for i := 1; i < len(pts); i++ {
		ss[i] = ss[i-1] + pts[i].Sub(pts[i-1]).Len()
	}
	lo, hi := pts[0], pts[0]
	for _, p := range pts {
		lo.X = math.Min(lo.X, p.X)
		lo.Y = math.Min(lo.Y, p.Y)
		hi.X = math.Max(hi.X, p.X)
		hi.Y = math.Max(hi.Y, p.Y)
	}
	return Guide{Pts: pts, S: ss, BrightSign: sign, Min: lo, Max: hi}
}

type Projection struct {
	Q      geom.V
	T, N   geom.V
	S      float64
	Signed float64
	Dist   float64
}

func (g *Guide) translateY(offset float64) {
	g.projection = nil
	for i := range g.Pts {
		g.Pts[i].Y += offset
	}
	g.Min.Y += offset
	g.Max.Y += offset
}

func (g *Guide) distanceBound2(p geom.V) float64 {
	dx := max(0, g.Min.X-p.X, p.X-g.Max.X)
	dy := max(0, g.Min.Y-p.Y, p.Y-g.Max.Y)
	return dx*dx + dy*dy
}

func (g *Guide) project(p geom.V) Projection {
	segment, u, q, best2 := guideNearest(g, p)
	tangent := g.segmentTangent(segment, u)
	normal := tangent.Perp().Mul(g.BrightSign)
	return Projection{Q: q, T: tangent, N: normal, S: geom.Lerp(g.S[segment], g.S[segment+1], u), Signed: p.Sub(q).Dot(normal), Dist: math.Sqrt(best2)}
}

// Interpolate vertex tangents so lighting and branches stay smooth at sample boundaries.
func (g *Guide) segmentTangent(i int, u float64) geom.V {
	t := g.Pts[i+1].Sub(g.Pts[i]).Norm()
	a, b := t, t
	if i > 0 {
		a = g.Pts[i].Sub(g.Pts[i-1]).Norm().Add(t).Norm()
	}
	if i+2 < len(g.Pts) {
		b = t.Add(g.Pts[i+2].Sub(g.Pts[i+1]).Norm()).Norm()
	}
	return geom.LerpVector(a, b, u).Norm()
}

func (g *Guide) frameAt(s float64) (q, t, n geom.V) {
	s = geom.Clamp(s, 0, g.S[len(g.S)-1])
	i := sort.Search(len(g.S)-2, func(i int) bool { return g.S[i+1] >= s })
	a, b := g.Pts[i], g.Pts[i+1]
	seg := g.S[i+1] - g.S[i]
	u := 0.0
	if seg > 0 {
		u = (s - g.S[i]) / seg
	}
	q = geom.LerpVector(a, b, u)
	t = g.segmentTangent(i, u)
	n = t.Perp().Mul(g.BrightSign)
	return
}

func nearestGuide(p geom.V, guides []Guide) (int, Projection) {
	bestI := -1
	best := Projection{Dist: math.Inf(1)}
	for i := range guides {
		if guides[i].distanceBound2(p) >= best.Dist*best.Dist {
			continue
		}
		pr := guides[i].project(p)
		if pr.Dist < best.Dist {
			bestI, best = i, pr
		}
	}
	return bestI, best
}
