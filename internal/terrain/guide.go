package terrain

import (
	"math"
	"math/rand"
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

// NearestPoint projects onto the sampled guide without requiring arc lengths.
// Empty or single-point guides have no surface and return infinite distance.
func (g *Guide) NearestPoint(p geom.V) (geom.V, float64) {
	if len(g.Pts) < 2 {
		return geom.V{}, math.Inf(1)
	}
	_, _, q, distance2 := guideNearest(g, p)
	return q, math.Sqrt(distance2)
}

func (g *Guide) project(p geom.V) Projection {
	segment, u, q, best2 := guideNearest(g, p)
	return g.projectionAt(p, segment, u, q, best2)
}

func (g *Guide) projectWithin(p geom.V, radius float64) (Projection, bool) {
	segment, u, q, best2 := guideNearestScalarLimit(g, p, radius*radius)
	if segment < 0 {
		return Projection{}, false
	}
	return g.projectionAt(p, segment, u, q, best2), true
}

func (g *Guide) projectionAt(p geom.V, segment int, u float64, q geom.V, best2 float64) Projection {
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

// Choose one continuous rock side for the entire guide. The arc-length-weighted
// mean perpendicular is the endpoint displacement's perpendicular. Pointing it
// down makes the exposed side face up overall, without flipping at vertical
// tangents and introducing relief discontinuities away from the guide.
func (g *Guide) faceUp(orientation Orientation) {
	direction := g.Pts[len(g.Pts)-1].Sub(g.Pts[0])
	if direction.Len2() < 1e-24 {
		// A closed guide has no mean direction. Its middle tangent provides a
		// reference that reverses with point order and preserves the curl.
		_, direction, _ = g.frameAt(g.S[len(g.S)-1] / 2)
	}
	normal := WorldPoint(orientation, direction.Perp())
	g.BrightSign = 1
	// If the guide is vertical overall, choose the left-facing exposed side.
	if normal.Y < -1e-12 || math.Abs(normal.Y) <= 1e-12 && normal.X < 0 {
		g.BrightSign = -1
	}
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

// Broad, slightly uneven facets give the crest a chipped rock silhouette.
// These are border vertices, not Voronoi sites. Build them before translating
// a guide into a section window so overlapping windows get identical ridges.
func RidgedGuide(g Guide, seed int64) Guide {
	rng := rand.New(rand.NewSource(seed))
	length := g.S[len(g.S)-1]
	pts := []geom.V{g.Pts[0]}
	for s := 0.0; s < length; {
		step := math.Min(geom.Lerp(0.012, 0.023, rng.Float64()), length-s)
		// Shorten facets at tight bends so the original curl remains intact.
		for step > 0.005 {
			a, _, _ := g.frameAt(s)
			b, _, _ := g.frameAt(s + step)
			mid, _, _ := g.frameAt(s + step*.5)
			if mid.Sub(geom.LerpVector(a, b, .5)).Len() < .0008 {
				break
			}
			step *= .7
		}
		s += step
		p, _, n := g.frameAt(s)
		taper := geom.Smoothstep(0, 0.018, math.Min(s, length-s))
		pts = append(pts, p.Add(n.Mul(geom.Lerp(-0.0025, 0.0025, rng.Float64())*taper)))
	}
	g.projection = nil
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

const guideProjectionBlock = 8

type projectionBounds struct{ lo, hi geom.V }

func (b projectionBounds) distance2(p geom.V) float64 {
	dx := max(0, b.lo.X-p.X, p.X-b.hi.X)
	dy := max(0, b.lo.Y-p.Y, p.Y-b.hi.Y)
	return dx*dx + dy*dy
}

// Prepared only for the builder's owned, immutable guide window. Public guide
// inputs can still be edited and project without this cache.
type guideProjection struct {
	length2        []float64
	blocks         []projectionBounds
	ax, ay, dx, dy []float64
	index          *geom.BoundsIndex
}

func (g *Guide) prepareProjection() {
	n := len(g.Pts) - 1
	if n <= 0 {
		return
	}
	cache := &guideProjection{length2: make([]float64, n), ax: make([]float64, n), ay: make([]float64, n), dx: make([]float64, n), dy: make([]float64, n)}
	for first := 0; first < n; first += guideProjectionBlock {
		b := projectionBounds{g.Pts[first], g.Pts[first]}
		for i := first; i < min(first+guideProjectionBlock, n); i++ {
			d := g.Pts[i+1].Sub(g.Pts[i])
			cache.length2[i] = d.Len2()
			cache.ax[i], cache.ay[i], cache.dx[i], cache.dy[i] = g.Pts[i].X, g.Pts[i].Y, d.X, d.Y
			p := g.Pts[i+1]
			b.lo.X, b.lo.Y = min(b.lo.X, p.X), min(b.lo.Y, p.Y)
			b.hi.X, b.hi.Y = max(b.hi.X, p.X), max(b.hi.Y, p.Y)
		}
		// Keep pruning conservative around interpolated endpoints.
		b.lo = b.lo.Sub(geom.V{X: 1e-12, Y: 1e-12})
		b.hi = b.hi.Add(geom.V{X: 1e-12, Y: 1e-12})
		cache.blocks = append(cache.blocks, b)
	}
	g.projection = cache
	bounds := make([]geom.Bounds, len(cache.blocks))
	for i, b := range cache.blocks {
		bounds[i] = geom.Bounds{Min: b.lo, Max: b.hi}
	}
	cache.index = geom.NewBoundsIndex(bounds)
}

func guideNearestScalar(g *Guide, p geom.V) (segment int, u float64, q geom.V, best2 float64) {
	segment, u, q, best2 = guideNearestScalarLimit(g, p, math.Inf(1))
	if segment < 0 {
		segment = 0
	}
	return
}
func guideNearestScalarLimit(g *Guide, p geom.V, limit float64) (segment int, u float64, q geom.V, best2 float64) {
	segment = -1
	best2 = limit
	if g.projection != nil {
		c := g.projection
		best2 = c.index.Nearest(p, best2, func(block int, best float64) float64 {
			if c.blocks[block].distance2(p) > best {
				return best
			}
			for i := block * guideProjectionBlock; i < min((block+1)*guideProjectionBlock, len(c.length2)); i++ {
				if c.length2[i] == 0 {
					continue
				}
				d := geom.V{X: c.dx[i], Y: c.dy[i]}
				a := geom.V{X: c.ax[i], Y: c.ay[i]}
				t := geom.Clamp(p.Sub(a).Dot(d)/c.length2[i], 0, 1)
				point := a.Add(d.Mul(t))
				distance := p.Sub(point).Len2()
				if distance < best || (distance == best && (segment < 0 || i < segment)) {
					best, segment, u, q = distance, i, t, point
				}
			}
			return best
		})
		return
	}
	for i := 0; i < len(g.Pts)-1; i++ {
		a := g.Pts[i]
		d := g.Pts[i+1].Sub(a)
		l2 := d.Len2()
		if l2 == 0 {
			continue
		}
		t := geom.Clamp(p.Sub(a).Dot(d)/l2, 0, 1)
		point := a.Add(d.Mul(t))
		distance2 := p.Sub(point).Len2()
		if distance2 < best2 {
			best2, segment, u, q = distance2, i, t, point
		}
	}
	return
}

func guideNearest(g *Guide, p geom.V) (int, float64, geom.V, float64) {
	return guideNearestScalar(g, p)
}
