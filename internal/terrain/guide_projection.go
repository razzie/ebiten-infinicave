package terrain

import (
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"math"
)

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
}

func guideNearestScalar(g *Guide, p geom.V) (segment int, u float64, q geom.V, best2 float64) {
	best2 = math.Inf(1)
	for i := 0; i < len(g.Pts)-1; i++ {
		a := g.Pts[i]
		var d geom.V
		var l2 float64
		if g.projection != nil {
			if i%guideProjectionBlock == 0 && g.projection.blocks[i/guideProjectionBlock].distance2(p) >= best2 {
				i = min(i+guideProjectionBlock, len(g.Pts)-1) - 1
				continue
			}
			d = geom.V{X: g.projection.dx[i], Y: g.projection.dy[i]}
			l2 = g.projection.length2[i]
		} else {
			d = g.Pts[i+1].Sub(a)
			l2 = d.Len2()
		}
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
